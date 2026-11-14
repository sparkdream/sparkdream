package vision

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/color/palette"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"testing"

	"sparkdream/tools/contentscan"
)

// The fixtures in testdata come from the reference implementations (timm's
// VisionTransformer and Pillow's bicubic resize): a tiny random model, a
// small picture, that picture resized by Pillow, and the model's logits for
// it after timm's preparation (resize the short side, centre crop,
// normalise).

func readFile(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func decodeOne(t *testing.T, data []byte) rgb {
	t.Helper()
	frames, err := decodeFrames(data, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(frames))
	}
	return frames[0]
}

func TestResizeMatchesPillow(t *testing.T) {
	src := decodeOne(t, readFile(t, "picture.png"))
	for _, c := range []struct {
		name string
		w, h int
	}{{"picture-resized.png", 43, 32}, {"picture-upscaled.png", 123, 91}} {
		want := decodeOne(t, readFile(t, c.name))
		got := resize(src, c.w, c.h)
		if got.w != want.w || got.h != want.h {
			t.Fatalf("%s: size %dx%d, want %dx%d", c.name, got.w, got.h, want.w, want.h)
		}
		if !bytes.Equal(got.pix, want.pix) {
			t.Errorf("%s: pixels differ from Pillow's", c.name)
		}
	}
}

func TestViTMatchesTimm(t *testing.T) {
	var ref struct {
		Logits []float64 `json:"logits"`
	}
	if err := json.Unmarshal(readFile(t, "tiny-vit.json"), &ref); err != nil {
		t.Fatal(err)
	}
	m, err := NewViT(readFile(t, "tiny-vit.safetensors"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Size != 32 || m.Patch != 16 || m.Classes() != 2 {
		t.Fatalf("model read as size %d patch %d classes %d", m.Size, m.Patch, m.Classes())
	}
	v := views(decodeOne(t, readFile(t, "picture.png")), m.Size)
	if len(v) != 3 {
		t.Fatalf("got %d views of a 50x37 picture, want 3 (two ends and the centre)", len(v))
	}
	got, err := m.Logits(context.Background(), normalise(v[1]))
	if err != nil {
		t.Fatal(err)
	}
	for i := range ref.Logits {
		if math.Abs(float64(got[i])-ref.Logits[i]) > 1e-4 {
			t.Errorf("logit %d = %v, timm gives %v", i, got[i], ref.Logits[i])
		}
	}
	// The same input always gives the same bits, whatever the CPU count.
	again, _ := m.Logits(context.Background(), normalise(v[1]))
	for i := range got {
		if math.Float32bits(got[i]) != math.Float32bits(again[i]) {
			t.Errorf("logit %d is not reproducible", i)
		}
	}
}

// TestPinnedModel runs the real weights when CS_IMAGE_MODEL points at them
// (the reference image sets it): the file must be the pinned one.
func TestPinnedModel(t *testing.T) {
	path := os.Getenv("CS_IMAGE_MODEL")
	if path == "" {
		t.Skip("CS_IMAGE_MODEL is not set")
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.ModelHash() != "sha256:"+PinnedWeights {
		t.Fatalf("weights are %s, not the pinned model", c.ModelHash())
	}
	scores, err := c.Classify(context.Background(), contentscan.Blob{Data: readFile(t, "picture.png")})
	if err != nil {
		t.Fatal(err)
	}
	if s := scores[contentscan.CategorySexual]; s > 0.5 {
		t.Errorf("a test pattern scored %v for sexual content", s)
	}
}

func solid(w, h int, c color.Color) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		img.Set(i%w, i/w, c)
	}
	return img
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestViewsCoverLongImages(t *testing.T) {
	for _, c := range []struct {
		w, h, want int
	}{
		{100, 100, 1}, // square
		{104, 100, 1}, // nearly square: the centre crop loses little
		{150, 100, 3}, // both ends and the centre
		{100, 300, 5},
		{1000, 100, 6}, // five squares cannot cover it: plus the whole, squeezed
	} {
		v := views(rgb{w: c.w, h: c.h, pix: make([]byte, c.w*c.h*3)}, 32)
		if len(v) != c.want {
			t.Errorf("%dx%d: %d views, want %d", c.w, c.h, len(v), c.want)
		}
		for _, one := range v {
			if one.w != 32 || one.h != 32 {
				t.Errorf("%dx%d: a view is %dx%d", c.w, c.h, one.w, one.h)
			}
		}
	}
}

func TestViewsReachBothEnds(t *testing.T) {
	// A wide picture, black except for a white band at its right edge.
	img := rgb{w: 300, h: 100, pix: make([]byte, 300*100*3)}
	for y := 0; y < 100; y++ {
		for x := 280; x < 300; x++ {
			copy(img.pix[(y*300+x)*3:], []byte{255, 255, 255})
		}
	}
	v := views(img, 32)
	last := v[len(v)-1]
	if last.pix[(16*32+31)*3] < 200 {
		t.Error("the last view does not include the right edge")
	}
	if v[0].pix[(16*32+31)*3] != 0 {
		t.Error("the first view should be the left end")
	}
}

func TestTransparencyYieldsTwoRasters(t *testing.T) {
	frames, err := decodeFrames(encodePNG(t, solid(8, 8, color.NRGBA{R: 200, A: 128})), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 {
		t.Fatalf("got %d rasters, want one over black and one over white", len(frames))
	}
	if frames[0].pix[0] >= frames[1].pix[0] || frames[0].pix[1] != 0 || frames[1].pix[1] < 120 {
		t.Errorf("composites look wrong: %v over black, %v over white", frames[0].pix[:3], frames[1].pix[:3])
	}
	frames, err = decodeFrames(encodePNG(t, solid(8, 8, color.NRGBA{R: 200, A: 255})), Limits{})
	if err != nil || len(frames) != 1 {
		t.Fatalf("opaque picture: %d rasters, err %v", len(frames), err)
	}
}

func TestAnimatedGIFSamplesFramesAsShown(t *testing.T) {
	g := &gif.GIF{Config: image.Config{Width: 4, Height: 4, ColorModel: color.Palette(palette.Plan9)}}
	for i := 0; i < 20; i++ {
		f := image.NewPaletted(image.Rect(0, 0, 4, 4), palette.Plan9)
		shade := uint8(i * 12)
		for p := range f.Pix {
			f.Pix[p] = uint8(f.Palette.Index(color.RGBA{shade, shade, shade, 255}))
		}
		g.Image = append(g.Image, f)
		g.Delay = append(g.Delay, 1)
		g.Disposal = append(g.Disposal, gif.DisposalNone)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatal(err)
	}
	frames, err := decodeFrames(buf.Bytes(), Limits{SampleFrames: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 4 {
		t.Fatalf("got %d frames, want 4", len(frames))
	}
	if frames[0].pix[0] > 20 || frames[3].pix[0] < 200 {
		t.Errorf("sample should run from the first frame to the last: %d .. %d", frames[0].pix[0], frames[3].pix[0])
	}
	if _, err := decodeFrames(buf.Bytes(), Limits{MaxFrames: 10}); !errors.Is(err, ErrUnsupported) {
		t.Errorf("an animation over the frame limit: err = %v, want ErrUnsupported", err)
	}
}

func TestUnsupportedMediaGetsNoScore(t *testing.T) {
	apng := encodePNG(t, solid(4, 4, color.Gray{128}))
	// Splice an acTL chunk in after IHDR (8-byte signature + 25-byte IHDR).
	actl := make([]byte, 12+8)
	binary.BigEndian.PutUint32(actl, 8)
	copy(actl[4:], "acTL")
	apng = append(append(append([]byte{}, apng[:33]...), actl...), apng[33:]...)

	webpAnim := []byte("RIFF\x00\x00\x00\x00WEBPVP8X\x0a\x00\x00\x00\x02\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	for name, data := range map[string][]byte{
		"empty":         nil,
		"text":          []byte("hello, this is not an image"),
		"svg":           []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`),
		"mp4":           []byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom"),
		"avif":          []byte("\x00\x00\x00\x1cftypavif\x00\x00\x00\x00avifmif1"),
		"animated png":  apng,
		"animated webp": webpAnim,
	} {
		if _, err := decodeFrames(data, Limits{}); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%s: err = %v, want ErrUnsupported", name, err)
		}
	}
	// A picture over the pixel limit is refused from its header alone.
	if _, err := decodeFrames(encodePNG(t, solid(64, 64, color.Gray{1})), Limits{MaxPixels: 1000}); !errors.Is(err, ErrUnsupported) {
		t.Errorf("oversized picture: err = %v, want ErrUnsupported", err)
	}
	// A truncated picture is an error, never a score.
	whole := encodePNG(t, solid(64, 64, color.Gray{1}))
	if _, err := decodeFrames(whole[:len(whole)/2], Limits{}); err == nil {
		t.Error("a truncated picture decoded")
	}
}

func TestJPEGOrientationIsApplied(t *testing.T) {
	// 4 wide, 2 tall, left half white: orientation 6 (rotate 90° clockwise)
	// must give 2 wide, 4 tall with the top half white.
	img := image.NewRGBA(image.Rect(0, 0, 16, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 16; x++ {
			if x < 8 {
				img.Set(x, y, color.White)
			} else {
				img.Set(x, y, color.Black)
			}
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	plain := buf.Bytes()
	tiff := []byte("II*\x00\x08\x00\x00\x00\x01\x00\x12\x01\x03\x00\x01\x00\x00\x00\x06\x00\x00\x00\x00\x00\x00\x00")
	exif := append([]byte("Exif\x00\x00"), tiff...)
	seg := []byte{0xff, 0xe1, byte((len(exif) + 2) >> 8), byte(len(exif) + 2)}
	rotated := append(append(append([]byte{}, plain[:2]...), append(seg, exif...)...), plain[2:]...)

	if o := jpegOrientation(plain); o != 1 {
		t.Fatalf("plain JPEG orientation = %d", o)
	}
	got := decodeOne(t, rotated)
	if got.w != 8 || got.h != 16 {
		t.Fatalf("rotated size %dx%d, want 8x16", got.w, got.h)
	}
	if top, bottom := got.pix[(2*8+4)*3], got.pix[(13*8+4)*3]; top < 200 || bottom > 50 {
		t.Errorf("after rotation top=%d bottom=%d, want white over black", top, bottom)
	}
}

func TestBadWeightsAreRejected(t *testing.T) {
	good := readFile(t, "tiny-vit.safetensors")
	for name, data := range map[string][]byte{
		"empty":     nil,
		"truncated": good[:len(good)/2],
		"garbage":   bytes.Repeat([]byte{0xff}, 64),
	} {
		if _, err := New(data); err == nil {
			t.Errorf("%s weights loaded", name)
		}
	}
	if _, err := New(good); err != nil {
		t.Errorf("good weights: %v", err)
	}
}
