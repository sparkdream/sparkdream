package vision

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"

	"golang.org/x/image/bmp"
	"golang.org/x/image/webp"

	"sparkdream/tools/contentscan"
)

// ErrUnsupported means the bytes are not an image this package can look at
// in full: video, audio, archives, vector graphics, formats without a Go
// decoder (AVIF, HEIC), or an animation it cannot step through (APNG,
// animated WebP). The worker then issues no verdict and the subject stays
// unchecked: calling a file clean after seeing one frame of it, or none,
// would sign for media nobody looked at.
var ErrUnsupported = fmt.Errorf("%w: not an image the built-in model can decode", contentscan.ErrUnsupportedMedia)

// Limits bound the work one image may cause. Decoders run on attacker-chosen
// input (§5.3 "sandbox all decoding"): Go's are memory-safe, and these keep a
// small file from expanding into gigabytes or hours.
type Limits struct {
	// MaxPixels caps width x height (default 40 megapixels).
	MaxPixels int
	// MaxFrames caps the frames of an animation that are composited
	// (default 600); a longer one is unsupported.
	MaxFrames int
	// SampleFrames is how many frames of an animation are classified
	// (default 8, spread evenly, first and last included).
	SampleFrames int
	// MaxViews caps the model runs for one image (default 16).
	MaxViews int
}

func (l Limits) withDefaults() Limits {
	if l.MaxPixels <= 0 {
		l.MaxPixels = 40_000_000
	}
	if l.MaxFrames <= 0 {
		l.MaxFrames = 600
	}
	if l.SampleFrames <= 0 {
		l.SampleFrames = 8
	}
	if l.MaxViews <= 0 {
		l.MaxViews = 16
	}
	return l
}

// decodeFrames returns the rasters to classify for one file: the image, or a
// sample of an animation's frames, each as it would be shown. A picture with
// transparency yields two rasters, over black and over white, since what a
// viewer sees depends on the page behind it.
func decodeFrames(data []byte, lim Limits) ([]rgb, error) {
	lim = lim.withDefaults()
	format, err := sniff(data)
	if err != nil {
		return nil, err
	}
	var frames []*image.NRGBA
	switch format {
	case "png":
		if isAPNG(data) {
			return nil, fmt.Errorf("%w: animated PNG", ErrUnsupported)
		}
		frames, err = decodeStill(data, lim, png.DecodeConfig, png.Decode)
	case "jpeg":
		frames, err = decodeStill(data, lim, jpeg.DecodeConfig, jpeg.Decode)
		if err == nil {
			frames[0] = orient(frames[0], jpegOrientation(data))
		}
	case "bmp":
		frames, err = decodeStill(data, lim, bmp.DecodeConfig, bmp.Decode)
	case "webp":
		if isAnimatedWebP(data) {
			return nil, fmt.Errorf("%w: animated WebP", ErrUnsupported)
		}
		frames, err = decodeStill(data, lim, webp.DecodeConfig, webp.Decode)
	case "gif":
		frames, err = decodeGIF(data, lim)
	}
	if err != nil {
		return nil, err
	}
	var out []rgb
	for _, f := range frames {
		out = append(out, flatten(f)...)
	}
	return out, nil
}

// sniff names the format by its leading bytes. The declared media type is
// not trusted: a browser picks its decoder the same way.
func sniff(b []byte) (string, error) {
	has := func(off int, sig string) bool { return len(b) >= off+len(sig) && string(b[off:off+len(sig)]) == sig }
	switch {
	case has(0, "\x89PNG\r\n\x1a\n"):
		return "png", nil
	case has(0, "\xff\xd8\xff"):
		return "jpeg", nil
	case has(0, "GIF87a"), has(0, "GIF89a"):
		return "gif", nil
	case has(0, "RIFF") && has(8, "WEBP"):
		return "webp", nil
	case has(0, "BM"):
		return "bmp", nil
	}
	return "", ErrUnsupported
}

func decodeStill(data []byte, lim Limits,
	config func(io.Reader) (image.Config, error),
	decode func(io.Reader) (image.Image, error),
) ([]*image.NRGBA, error) {
	cfg, err := config(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("vision: image header: %w", err)
	}
	if err := checkSize(cfg.Width, cfg.Height, lim); err != nil {
		return nil, err
	}
	img, err := decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("vision: image data: %w", err)
	}
	return []*image.NRGBA{toNRGBA(img)}, nil
}

func checkSize(w, h int, lim Limits) error {
	if w <= 0 || h <= 0 || w > lim.MaxPixels/h {
		return fmt.Errorf("%w: %dx%d pixels is outside the limit", ErrUnsupported, w, h)
	}
	return nil
}

func toNRGBA(img image.Image) *image.NRGBA {
	if n, ok := img.(*image.NRGBA); ok && n.Rect.Min == (image.Point{}) {
		return n
	}
	b := img.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Rect, img, b.Min, draw.Src)
	return out
}

// decodeGIF composites an animation frame by frame (each GIF frame is a
// patch over the canvas, with a disposal rule) and returns an even sample of
// the frames as shown.
func decodeGIF(data []byte, lim Limits) ([]*image.NRGBA, error) {
	cfg, err := gif.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("vision: image header: %w", err)
	}
	if err := checkSize(cfg.Width, cfg.Height, lim); err != nil {
		return nil, err
	}
	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("vision: image data: %w", err)
	}
	n := len(g.Image)
	if n == 0 {
		return nil, fmt.Errorf("%w: empty GIF", ErrUnsupported)
	}
	if n > lim.MaxFrames {
		return nil, fmt.Errorf("%w: %d frames is over the limit", ErrUnsupported, n)
	}
	if err := checkSize(g.Config.Width, g.Config.Height, lim); err != nil {
		return nil, err
	}
	keep := map[int]bool{}
	samples := lim.SampleFrames
	if samples > n {
		samples = n
	}
	for i := 0; i < samples; i++ {
		if samples == 1 {
			keep[0] = true
		} else {
			keep[i*(n-1)/(samples-1)] = true
		}
	}
	canvas := image.NewNRGBA(image.Rect(0, 0, g.Config.Width, g.Config.Height))
	var out []*image.NRGBA
	for i, frame := range g.Image {
		var saved *image.NRGBA
		disposal := byte(0)
		if i < len(g.Disposal) {
			disposal = g.Disposal[i]
		}
		if disposal == gif.DisposalPrevious {
			saved = cloneNRGBA(canvas)
		}
		draw.Draw(canvas, frame.Bounds(), frame, frame.Bounds().Min, draw.Over)
		if keep[i] {
			out = append(out, cloneNRGBA(canvas))
		}
		switch disposal {
		case gif.DisposalBackground:
			draw.Draw(canvas, frame.Bounds(), image.Transparent, image.Point{}, draw.Src)
		case gif.DisposalPrevious:
			canvas = saved
		}
	}
	return out, nil
}

func cloneNRGBA(src *image.NRGBA) *image.NRGBA {
	out := image.NewNRGBA(src.Rect)
	copy(out.Pix, src.Pix)
	return out
}

// flatten turns a frame into opaque rasters: one if it has no transparency,
// else one over black and one over white.
func flatten(img *image.NRGBA) []rgb {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	opaque := true
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 0xff {
			opaque = false
			break
		}
	}
	over := func(bg uint32) rgb {
		out := rgb{w: w, h: h, pix: make([]byte, w*h*3)}
		for y := 0; y < h; y++ {
			row := img.Pix[y*img.Stride:]
			for x := 0; x < w; x++ {
				a := uint32(row[x*4+3])
				for c := 0; c < 3; c++ {
					out.pix[(y*w+x)*3+c] = byte((uint32(row[x*4+c])*a + bg*(255-a) + 127) / 255)
				}
			}
		}
		return out
	}
	if opaque {
		return []rgb{over(0)}
	}
	return []rgb{over(0), over(255)}
}

// isAPNG reports whether a PNG carries an animation control chunk before its
// image data. Go decodes only the default image of an APNG, so the other
// frames would go unseen.
func isAPNG(b []byte) bool {
	for off := 8; off+8 <= len(b); {
		n := int(binary.BigEndian.Uint32(b[off:]))
		kind := string(b[off+4 : off+8])
		if kind == "acTL" {
			return true
		}
		if kind == "IDAT" || kind == "IEND" || n < 0 {
			return false
		}
		off += 12 + n
	}
	return false
}

// isAnimatedWebP reports whether a WebP has the animation flag set in its
// extended header, or carries animation chunks.
func isAnimatedWebP(b []byte) bool {
	for off := 12; off+8 <= len(b); {
		kind := string(b[off : off+4])
		n := int(binary.LittleEndian.Uint32(b[off+4:]))
		if kind == "ANIM" || kind == "ANMF" {
			return true
		}
		if kind == "VP8X" && off+9 <= len(b) && b[off+8]&0x02 != 0 {
			return true
		}
		if n < 0 {
			return false
		}
		off += 8 + n + n&1
	}
	return false
}

// jpegOrientation reads the EXIF orientation (1-8) of a JPEG, 1 if it has
// none. Browsers rotate by it, so the classifier must see the same picture.
func jpegOrientation(b []byte) int {
	for off := 2; off+4 <= len(b); {
		if b[off] != 0xff {
			return 1
		}
		marker := b[off+1]
		if marker == 0xda || marker == 0xd9 {
			return 1
		}
		n := int(binary.BigEndian.Uint16(b[off+2:]))
		if n < 2 || off+2+n > len(b) {
			return 1
		}
		seg := b[off+4 : off+2+n]
		if marker == 0xe1 && len(seg) >= 14 && string(seg[:6]) == "Exif\x00\x00" {
			return tiffOrientation(seg[6:])
		}
		off += 2 + n
	}
	return 1
}

func tiffOrientation(t []byte) int {
	var bo binary.ByteOrder
	switch {
	case len(t) >= 8 && string(t[:2]) == "II":
		bo = binary.LittleEndian
	case len(t) >= 8 && string(t[:2]) == "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	ifd := int(bo.Uint32(t[4:]))
	if ifd < 0 || ifd+2 > len(t) {
		return 1
	}
	count := int(bo.Uint16(t[ifd:]))
	for i := 0; i < count; i++ {
		e := ifd + 2 + i*12
		if e+12 > len(t) {
			return 1
		}
		if bo.Uint16(t[e:]) == 0x0112 {
			if v := int(bo.Uint16(t[e+8:])); v >= 1 && v <= 8 {
				return v
			}
			return 1
		}
	}
	return 1
}

// orient applies an EXIF orientation.
func orient(img *image.NRGBA, o int) *image.NRGBA {
	if o <= 1 || o > 8 {
		return img
	}
	w, h := img.Rect.Dx(), img.Rect.Dy()
	ow, oh := w, h
	if o >= 5 {
		ow, oh = h, w
	}
	out := image.NewNRGBA(image.Rect(0, 0, ow, oh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch o {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			out.SetNRGBA(dx, dy, color.NRGBA{
				R: img.Pix[y*img.Stride+x*4], G: img.Pix[y*img.Stride+x*4+1],
				B: img.Pix[y*img.Stride+x*4+2], A: img.Pix[y*img.Stride+x*4+3],
			})
		}
	}
	return out
}
