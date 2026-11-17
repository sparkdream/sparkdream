// Package fileprofile decides whether a file is one x/storage keeps a
// permanent copy of (docs/x-storage-spec.md, "The file profile"). It uses
// only the standard library, so the chain binary's CLI, the scanner worker
// and a storage operator can all link the same rule.
package fileprofile

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
)

// ErrOutsideProfile is returned (wrapped) when a file is not in the storage
// profile of its declared type.
var ErrOutsideProfile = errors.New("file is outside the storage profile")

// maxProfilePixelBytes bounds what a PNG may inflate to, so a small file
// cannot make a checker allocate without limit.
const maxProfilePixelBytes = 256 << 20

func profileErr(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrOutsideProfile, fmt.Sprintf(format, args...))
}

// Check reports whether data is a file x/storage will keep a permanent
// copy of under contentType (docs/x-storage-spec.md, "The file profile").
// The profile is deliberately narrow: one still image, no metadata, no
// colour management and no transparency, so the pixels a scanner's model
// sees are the pixels a viewer sees, and no camera metadata is published for
// good. A client re-encodes an image into the profile before it computes the
// hash.
//
// Clients, scanners and storage operators all call this one function. A
// storage operator's mismatch decline is judged against it, so its answers
// are pinned by the vectors in fileprofile_test.go and must not change for a
// released version without a new version of the rule.
//
// A content type with no profile is refused: only listed types are judged.
func Check(contentType string, data []byte) error {
	switch contentType {
	case "image/png":
		return pngProfile(data)
	case "image/jpeg":
		return jpegProfile(data)
	default:
		return profileErr("no profile for content type %q", contentType)
	}
}

var pngSignature = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

// pngProfile accepts: the signature; IHDR with bit depth 8, colour type 0, 2
// or 3 and no interlace; an optional sRGB; PLTE for colour type 3 only;
// consecutive IDAT chunks whose zlib stream ends exactly at the end of the
// last one and inflates to exactly the bytes the header needs; IEND. Every
// chunk has a valid CRC and nothing follows IEND.
func pngProfile(data []byte) error {
	if !bytes.HasPrefix(data, pngSignature) {
		return profileErr("not a PNG")
	}
	rest := data[len(pngSignature):]
	var (
		width, height      uint32
		colourType         byte
		seenIHDR, seenPLTE bool
		seenSRGB, seenIDAT bool
		idatDone           bool
		idat               bytes.Buffer
	)
	for {
		if len(rest) < 12 {
			return profileErr("truncated chunk")
		}
		n := binary.BigEndian.Uint32(rest[:4])
		if uint64(n) > uint64(len(rest))-12 {
			return profileErr("chunk runs past the end of the file")
		}
		typ := string(rest[4:8])
		body := rest[8 : 8+n]
		if crc32.ChecksumIEEE(rest[4:8+n]) != binary.BigEndian.Uint32(rest[8+n:12+n]) {
			return profileErr("%s chunk has a bad CRC", typ)
		}
		rest = rest[12+n:]
		if !seenIHDR && typ != "IHDR" {
			return profileErr("first chunk is %s, not IHDR", typ)
		}
		if seenIDAT && typ != "IDAT" {
			idatDone = true
		}
		switch typ {
		case "IHDR":
			if seenIHDR || n != 13 {
				return profileErr("bad IHDR")
			}
			seenIHDR = true
			width = binary.BigEndian.Uint32(body[0:4])
			height = binary.BigEndian.Uint32(body[4:8])
			depth, compression, filter, interlace := body[8], body[10], body[11], body[12]
			colourType = body[9]
			if width == 0 || height == 0 {
				return profileErr("zero width or height")
			}
			if depth != 8 {
				return profileErr("bit depth %d (only 8 is kept)", depth)
			}
			if colourType != 0 && colourType != 2 && colourType != 3 {
				return profileErr("colour type %d (transparency is not kept)", colourType)
			}
			if compression != 0 || filter != 0 {
				return profileErr("unknown compression or filter method")
			}
			if interlace != 0 {
				return profileErr("interlaced")
			}
		case "sRGB":
			if seenSRGB || seenPLTE || seenIDAT || n != 1 {
				return profileErr("misplaced or repeated sRGB")
			}
			seenSRGB = true
		case "PLTE":
			if seenPLTE || seenIDAT || colourType != 3 {
				return profileErr("PLTE where the image does not use one")
			}
			if n == 0 || n%3 != 0 || n > 768 {
				return profileErr("bad PLTE length")
			}
			seenPLTE = true
		case "IDAT":
			if idatDone {
				return profileErr("IDAT chunks are not consecutive")
			}
			if colourType == 3 && !seenPLTE {
				return profileErr("palette image without a PLTE")
			}
			seenIDAT = true
			idat.Write(body)
		case "IEND":
			if n != 0 || !seenIDAT {
				return profileErr("bad IEND")
			}
			if len(rest) != 0 {
				return profileErr("%d bytes after IEND", len(rest))
			}
			return pngPixels(idat.Bytes(), width, height, colourType)
		default:
			return profileErr("%s chunk is not kept", typ)
		}
	}
}

// pngPixels checks that the IDAT stream is exactly one zlib stream holding
// exactly the scanlines the header calls for.
func pngPixels(stream []byte, width, height uint32, colourType byte) error {
	channels := uint64(1)
	if colourType == 2 {
		channels = 3
	}
	// Each dimension is bounded first: the header allows 2^32-1 for both,
	// and their product would wrap a uint64 and pass the size check below.
	if uint64(width) > maxProfilePixelBytes || uint64(height) > maxProfilePixelBytes {
		return profileErr("image is too large to check")
	}
	rowBytes := uint64(width)*channels + 1 // one filter byte per scanline
	want := rowBytes * uint64(height)
	if want > maxProfilePixelBytes {
		return profileErr("image is too large to check")
	}
	src := bytes.NewReader(stream)
	zr, err := zlib.NewReader(src)
	if err != nil {
		return profileErr("IDAT is not a zlib stream")
	}
	defer zr.Close()
	pixels := make([]byte, want)
	if _, err := io.ReadFull(zr, pixels); err != nil {
		return profileErr("IDAT holds fewer bytes than the image needs")
	}
	// The stream must end here, with a valid checksum, and use every byte.
	var one [1]byte
	if n, err := zr.Read(one[:]); n != 0 || err != io.EOF {
		return profileErr("IDAT holds more bytes than the image needs")
	}
	if src.Len() != 0 {
		return profileErr("%d bytes after the zlib stream ends", src.Len())
	}
	for y := uint64(0); y < uint64(height); y++ {
		if pixels[y*rowBytes] > 4 {
			return profileErr("unknown scanline filter")
		}
	}
	return nil
}

// JPEG markers.
const (
	mSOF0 = 0xC0
	mSOF2 = 0xC2
	mDHT  = 0xC4
	mRST0 = 0xD0
	mRST7 = 0xD7
	mSOI  = 0xD8
	mEOI  = 0xD9
	mSOS  = 0xDA
	mDQT  = 0xDB
	mDRI  = 0xDD
	mAPP0 = 0xE0
)

// maxJPEGScans bounds a progressive image's scans.
const maxJPEGScans = 64

// jpegProfile accepts: SOI; one optional 16-byte JFIF APP0 with no thumbnail,
// directly after SOI; DQT, DHT and DRI; exactly one SOF0 (baseline) or SOF2
// (progressive) frame with 8-bit samples and 1 or 3 components; its scans;
// EOI. Each segment starts where the last ended and nothing follows EOI.
func jpegProfile(data []byte) error {
	if len(data) < 4 || data[0] != 0xFF || data[1] != mSOI {
		return profileErr("not a JPEG")
	}
	pos := 2
	first := true
	seenFrame := false
	scans := 0
	for {
		if pos+2 > len(data) {
			return profileErr("truncated before EOI")
		}
		if data[pos] != 0xFF {
			return profileErr("bytes between segments at offset %d", pos)
		}
		marker := data[pos+1]
		pos += 2
		if marker == mEOI {
			if scans == 0 {
				return profileErr("no image data")
			}
			if pos != len(data) {
				return profileErr("%d bytes after EOI", len(data)-pos)
			}
			return nil
		}
		if pos+2 > len(data) {
			return profileErr("truncated segment")
		}
		segLen := int(binary.BigEndian.Uint16(data[pos : pos+2]))
		if segLen < 2 || pos+segLen > len(data) {
			return profileErr("segment runs past the end of the file")
		}
		body := data[pos+2 : pos+segLen]
		switch marker {
		case mAPP0:
			if !first {
				return profileErr("APP0 is not directly after SOI")
			}
			if len(body) != 14 || !bytes.Equal(body[:5], []byte("JFIF\x00")) {
				return profileErr("APP0 is not a plain JFIF header")
			}
			if body[12] != 0 || body[13] != 0 {
				return profileErr("JFIF thumbnail")
			}
		case mDQT, mDHT:
			if len(body) == 0 {
				return profileErr("empty table segment")
			}
		case mDRI:
			if len(body) != 2 {
				return profileErr("bad DRI")
			}
		case mSOF0, mSOF2:
			if seenFrame {
				return profileErr("a second frame")
			}
			if len(body) < 6 {
				return profileErr("bad frame header")
			}
			precision, components := body[0], int(body[5])
			h := binary.BigEndian.Uint16(body[1:3])
			w := binary.BigEndian.Uint16(body[3:5])
			if precision != 8 {
				return profileErr("%d-bit samples (only 8 is kept)", precision)
			}
			if w == 0 || h == 0 {
				return profileErr("zero width or height")
			}
			if components != 1 && components != 3 {
				return profileErr("%d components (only grey and three-colour are kept)", components)
			}
			if len(body) != 6+3*components {
				return profileErr("bad frame header")
			}
			seenFrame = true
		case mSOS:
			if !seenFrame {
				return profileErr("scan before the frame header")
			}
			scans++
			if scans > maxJPEGScans {
				return profileErr("too many scans")
			}
			pos += segLen
			// Entropy-coded data runs to the next marker that is not a
			// stuffed byte (FF00) or a restart marker.
			for {
				i := bytes.IndexByte(data[pos:], 0xFF)
				if i < 0 || pos+i+1 >= len(data) {
					return profileErr("truncated scan")
				}
				pos += i
				next := data[pos+1]
				if next == 0x00 || (next >= mRST0 && next <= mRST7) {
					pos += 2
					continue
				}
				break
			}
			first = false
			continue
		default:
			return profileErr("segment FF%02X is not kept", marker)
		}
		first = false
		pos += segLen
	}
}
