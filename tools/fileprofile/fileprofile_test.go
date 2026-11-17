package fileprofile

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"testing"
)

// exportVector appends one test vector to the file named by
// FILEPROFILE_VECTORS_OUT, as a JSON line. Ports of the profile in other
// languages (sparkdream-ui's src/lib/fileProfile.ts) are tested against the
// vectors this package is tested against:
//
//	FILEPROFILE_VECTORS_OUT=/path/vectors.jsonl go test ./tools/fileprofile -count=1
func exportVector(t *testing.T, contentType, name string, data []byte, ok bool) {
	t.Helper()
	path := os.Getenv("FILEPROFILE_VECTORS_OUT")
	if path == "" {
		return
	}
	line, err := json.Marshal(map[string]any{"content_type": contentType, "name": name, "hex": hex.EncodeToString(data), "ok": ok})
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		t.Fatal(err)
	}
}

func pngChunk(typ string, body []byte) []byte {
	out := make([]byte, 0, 12+len(body))
	out = binary.BigEndian.AppendUint32(out, uint32(len(body)))
	out = append(out, typ...)
	out = append(out, body...)
	return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(out[4:]))
}

// pngInsert puts a chunk before the first chunk named before.
func pngInsert(t *testing.T, data []byte, before string, chunk []byte) []byte {
	t.Helper()
	pos := 8
	for pos < len(data) {
		n := int(binary.BigEndian.Uint32(data[pos : pos+4]))
		if string(data[pos+4:pos+8]) == before {
			out := append([]byte{}, data[:pos]...)
			out = append(out, chunk...)
			return append(out, data[pos:]...)
		}
		pos += 12 + n
	}
	t.Fatalf("no %s chunk", before)
	return nil
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func rgbImage() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 7, 5))
	for y := 0; y < 5; y++ {
		for x := 0; x < 7; x++ {
			img.SetNRGBA(x, y, color.NRGBA{uint8(x * 30), uint8(y * 40), 90, 255})
		}
	}
	return img
}

func TestFileProfilePNG(t *testing.T) {
	rgb := encodePNG(t, rgbImage())
	grey := encodePNG(t, image.NewGray(image.Rect(0, 0, 4, 4)))
	// Seventeen colours, so the encoder writes 8-bit indices.
	var colours color.Palette
	for i := 0; i < 17; i++ {
		colours = append(colours, color.NRGBA{uint8(i * 15), uint8(255 - i*15), 7, 255})
	}
	paletted := encodePNG(t, image.NewPaletted(image.Rect(0, 0, 4, 4), colours))
	twoColour := image.NewPaletted(image.Rect(0, 0, 4, 4), color.Palette{color.NRGBA{0, 0, 0, 255}, color.NRGBA{255, 255, 255, 255}})

	transparent := rgbImage()
	transparent.SetNRGBA(1, 1, color.NRGBA{1, 2, 3, 0})
	palAlpha := image.NewPaletted(image.Rect(0, 0, 4, 4), append(color.Palette{color.NRGBA{0, 0, 0, 0}}, colours...))

	interlaced := append([]byte{}, rgb...)
	interlaced[8+8+12] = 1 // IHDR interlace byte
	binary.BigEndian.PutUint32(interlaced[8+8+13:], crc32.ChecksumIEEE(interlaced[8+4:8+8+13]))

	badCRC := append([]byte{}, rgb...)
	badCRC[len(badCRC)-1] ^= 0xFF

	cases := []struct {
		name string
		data []byte
		ok   bool
	}{
		{"truecolour", rgb, true},
		{"greyscale", grey, true},
		{"paletted", paletted, true},
		{"with sRGB", pngInsert(t, rgb, "IDAT", pngChunk("sRGB", []byte{0})), true},
		{"1-bit palette", encodePNG(t, twoColour), false},
		{"alpha channel", encodePNG(t, transparent), false},
		{"palette with transparency", encodePNG(t, palAlpha), false},
		{"16-bit", encodePNG(t, image.NewGray16(image.Rect(0, 0, 4, 4))), false},
		{"interlaced", interlaced, false},
		{"bytes after IEND", append(append([]byte{}, rgb...), "hidden"...), false},
		{"text chunk", pngInsert(t, rgb, "IDAT", pngChunk("tEXt", []byte("Comment\x00hidden"))), false},
		{"EXIF chunk", pngInsert(t, rgb, "IDAT", pngChunk("eXIf", []byte("MM\x00*"))), false},
		{"gamma chunk", pngInsert(t, rgb, "IDAT", pngChunk("gAMA", []byte{0, 0, 0xB1, 0x8F})), false},
		{"colour profile", pngInsert(t, rgb, "IDAT", pngChunk("iCCP", []byte("p\x00\x00x"))), false},
		{"animation control", pngInsert(t, rgb, "IDAT", pngChunk("acTL", make([]byte, 8))), false},
		{"private chunk", pngInsert(t, rgb, "IEND", pngChunk("prVt", []byte("hidden"))), false},
		{"PLTE in a truecolour image", pngInsert(t, rgb, "IDAT", pngChunk("PLTE", make([]byte, 6))), false},
		{"bytes after the zlib stream", pngInsert(t, rgb, "IEND", pngChunk("IDAT", []byte("hidden"))), false},
		{"sRGB after IDAT", pngInsert(t, rgb, "IEND", pngChunk("sRGB", []byte{0})), false},
		{"bad CRC", badCRC, false},
		{"truncated", rgb[:len(rgb)-20], false},
		{"not a PNG", []byte("GIF89a..........................."), false},
		{"empty", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exportVector(t, "image/png", tc.name, tc.data, tc.ok)
			err := Check("image/png", tc.data)
			if tc.ok && err != nil {
				t.Fatalf("refused: %v", err)
			}
			if !tc.ok {
				if err == nil {
					t.Fatal("accepted")
				}
				if !errors.Is(err, ErrOutsideProfile) {
					t.Fatalf("error is not ErrOutsideProfile: %v", err)
				}
			}
		})
	}
}

func jpegSegment(marker byte, body []byte) []byte {
	out := []byte{0xFF, marker}
	out = binary.BigEndian.AppendUint16(out, uint16(len(body)+2))
	return append(out, body...)
}

// jpegInsert puts raw bytes directly after SOI.
func jpegInsert(data, seg []byte) []byte {
	out := append([]byte{}, data[:2]...)
	out = append(out, seg...)
	return append(out, data[2:]...)
}

func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// A header whose width times height wraps a uint64 to a small number must be
// refused as too large, not checked against the wrapped size: the scanline
// loop would index past the buffer and panic, and one such request would
// stop every worker that reads it.
func TestFileProfilePNGDimensionOverflow(t *testing.T) {
	var width, height uint32 = 3187393831, 1929135531
	wrapped := (uint64(width)*3 + 1) * uint64(height) // 236511698 after wrapping
	if wrapped >= maxProfilePixelBytes {
		t.Fatalf("the vector no longer wraps under the cap: %d", wrapped)
	}
	// A zlib stream that inflates to exactly the wrapped size.
	var idat bytes.Buffer
	zw := zlib.NewWriter(&idat)
	zeros := make([]byte, 1<<20)
	for left := wrapped; left > 0; {
		n := min(left, uint64(len(zeros)))
		if _, err := zw.Write(zeros[:n]); err != nil {
			t.Fatal(err)
		}
		left -= n
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	ihdr := binary.BigEndian.AppendUint32(nil, width)
	ihdr = binary.BigEndian.AppendUint32(ihdr, height)
	ihdr = append(ihdr, 8, 2, 0, 0, 0)
	file := []byte("\x89PNG\r\n\x1a\n")
	file = append(file, pngChunk("IHDR", ihdr)...)
	file = append(file, pngChunk("IDAT", idat.Bytes())...)
	file = append(file, pngChunk("IEND", nil)...)
	if err := Check("image/png", file); !errors.Is(err, ErrOutsideProfile) {
		t.Fatalf("want the file refused, got %v", err)
	}
}

func TestFileProfileJPEG(t *testing.T) {
	colour := encodeJPEG(t, rgbImage())
	grey := encodeJPEG(t, image.NewGray(image.Rect(0, 0, 9, 9)))

	jfif := []byte("JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00")
	thumb := append([]byte("JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x01\x01"), 1, 2, 3)
	jfxx := []byte("JFXX\x00\x10hidden")

	// A second copy of the frame header.
	sof := bytes.Index(colour, []byte{0xFF, 0xC0})
	sofLen := int(binary.BigEndian.Uint16(colour[sof+2:])) + 2
	twoFrames := append([]byte{}, colour[:sof+sofLen]...)
	twoFrames = append(twoFrames, colour[sof:sof+sofLen]...)
	twoFrames = append(twoFrames, colour[sof+sofLen:]...)

	fourComponents := append([]byte{}, colour...)
	fourComponents[sof+9] = 4

	twelveBit := append([]byte{}, colour...)
	twelveBit[sof+4] = 12

	afterDQT := bytes.Index(colour, []byte{0xFF, 0xC0})
	app0Late := append([]byte{}, colour[:afterDQT]...)
	app0Late = append(app0Late, jpegSegment(0xE0, jfif)...)
	app0Late = append(app0Late, colour[afterDQT:]...)

	cases := []struct {
		name string
		data []byte
		ok   bool
	}{
		{"colour baseline", colour, true},
		{"greyscale", grey, true},
		{"with a plain JFIF header", jpegInsert(colour, jpegSegment(0xE0, jfif)), true},
		{"JFIF thumbnail", jpegInsert(colour, jpegSegment(0xE0, thumb)), false},
		{"JFXX extension", jpegInsert(colour, jpegSegment(0xE0, jfxx)), false},
		{"APP0 not first", app0Late, false},
		{"EXIF", jpegInsert(colour, jpegSegment(0xE1, []byte("Exif\x00\x00MM\x00*"))), false},
		{"ICC profile", jpegInsert(colour, jpegSegment(0xE2, []byte("ICC_PROFILE\x00\x01\x01data"))), false},
		{"Adobe APP14", jpegInsert(colour, jpegSegment(0xEE, []byte("Adobe\x00d\x00\x00\x00\x00\x01"))), false},
		{"comment", jpegInsert(colour, jpegSegment(0xFE, []byte("hidden"))), false},
		{"bytes between segments", jpegInsert(colour, []byte("hidden")), false},
		{"fill byte between segments", jpegInsert(colour, []byte{0xFF}), false},
		{"second frame", twoFrames, false},
		{"four components", fourComponents, false},
		{"12-bit samples", twelveBit, false},
		{"bytes after EOI", append(append([]byte{}, colour...), "hidden"...), false},
		{"truncated", colour[:len(colour)-2], false},
		{"no scans", []byte{0xFF, 0xD8, 0xFF, 0xD9}, false},
		{"not a JPEG", []byte("\x89PNG\r\n\x1a\n"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exportVector(t, "image/jpeg", tc.name, tc.data, tc.ok)
			err := Check("image/jpeg", tc.data)
			if tc.ok && err != nil {
				t.Fatalf("refused: %v", err)
			}
			if !tc.ok {
				if err == nil {
					t.Fatal("accepted")
				}
				if !errors.Is(err, ErrOutsideProfile) {
					t.Fatalf("error is not ErrOutsideProfile: %v", err)
				}
			}
		})
	}
}

// A type with no profile is refused, never waved through.
func TestFileProfileUnknownType(t *testing.T) {
	for _, ct := range []string{"image/gif", "image/webp", "text/plain", ""} {
		if err := Check(ct, []byte("anything")); !errors.Is(err, ErrOutsideProfile) {
			t.Fatalf("%q: %v", ct, err)
		}
	}
}
