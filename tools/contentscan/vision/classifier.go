// Package vision is the content-scanner worker's built-in image classifier
// (docs/content-scanning.md §5.2): a pinned open-weight image-safety model
// run in-process, on the CPU, in plain Go. A worker with it needs no model
// server, and two workers with the same weights file give the same scores.
//
// The model scores one thing, sexual content ("sexual"). It does not
// estimate age and does not score violence, so the worker's CSAM heuristic
// (sexual and minor both over threshold) cannot fire from this classifier
// alone: sexual imagery is held under the "sexual" category.
package vision

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"os"

	"sparkdream/tools/contentscan"
)

// ModelName is the key the weights hash is published under in a ruleset's
// and an operator's `models`.
const ModelName = "image-safety"

// PinnedWeights is the sha256 of the weights the reference image ships:
// Marqo/nsfw-image-detection-384 (Apache 2.0), model.safetensors at
// revision 0c26ec22111b83f106d72a55f611ec35962bcb65. A ViT-tiny/16 at
// 384x384 with two classes, NSFW then SFW.
const PinnedWeights = "6bf2e0f64a1d20169736c2836e3a787b12379fdc08ba87f7d94a7a3d58eeefce"

// nsfwClass is the index of the NSFW class in the pinned model's output.
const nsfwClass = 0

// Classifier scores images with the built-in model. It implements
// contentscan.Classifier.
type Classifier struct {
	model  *ViT
	hash   string
	Limits Limits
}

// Load reads a weights file. The returned classifier reports the file's
// sha256 through ModelHash, for the ruleset and the operator metadata.
func Load(path string) (*Classifier, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("vision: weights: %w", err)
	}
	return New(raw)
}

// New builds a classifier from weights bytes.
func New(weights []byte) (*Classifier, error) {
	m, err := NewViT(weights)
	if err != nil {
		return nil, err
	}
	if m.Classes() != 2 {
		return nil, fmt.Errorf("vision: the image-safety model must have 2 classes (NSFW, SFW), these weights have %d", m.Classes())
	}
	sum := sha256.Sum256(weights)
	return &Classifier{model: m, hash: hex.EncodeToString(sum[:])}, nil
}

// ModelHash is "sha256:<hex>" of the weights file.
func (c *Classifier) ModelHash() string { return "sha256:" + c.hash }

// Classify implements contentscan.Classifier. The score is the highest over
// every view of the image (see views): a picture is as unsafe as its worst
// part. Bytes that are not a supported image return ErrUnsupported, so the
// worker issues no verdict.
func (c *Classifier) Classify(ctx context.Context, b contentscan.Blob) (map[string]float64, error) {
	frames, err := decodeFrames(b.Data, c.Limits)
	if err != nil {
		return nil, err
	}
	lim := c.Limits.withDefaults()
	var all []rgb
	for _, f := range frames {
		all = append(all, views(f, c.model.Size)...)
	}
	all = thin(all, lim.MaxViews)
	worst := 0.0
	for _, v := range all {
		logits, err := c.model.Logits(ctx, normalise(v))
		if err != nil {
			return nil, err
		}
		worst = math.Max(worst, softmax(logits)[nsfwClass])
	}
	return map[string]float64{contentscan.CategorySexual: worst}, nil
}

// maxCrops bounds the squares taken along a long image.
const maxCrops = 6

// views returns the model inputs for one raster. The model's own
// preparation resizes the short side to the input size and keeps the centre
// square, which would leave the ends of a wide or tall image unseen: an
// easy way to hide something. So a long image is covered with evenly spaced
// squares along its length, the centre one included, and, when even those
// leave gaps, a view of the whole image squeezed into the square.
func views(img rgb, side int) []rgb {
	short, long := img.w, img.h
	if short > long {
		short, long = long, short
	}
	// torchvision's Resize(int): the long side is truncated, not rounded.
	scaled := side * long / short
	if scaled < side {
		scaled = side
	}
	w, h := side, scaled
	if img.w > img.h {
		w, h = scaled, side
	}
	resized := resize(img, w, h)
	if scaled == side {
		return []rgb{resized}
	}
	extra := scaled - side
	n := 1
	// Within a twelfth of the side of square, the centre crop loses little.
	if extra > side/12 {
		n = (scaled+side-1)/side + 1
		if n%2 == 0 {
			n++ // odd, so the centre square is one of them
		}
		if n > maxCrops {
			n = maxCrops - 1
		}
	}
	var out []rgb
	for i := 0; i < n; i++ {
		// torchvision's CenterCrop offset for the middle one: round half to even.
		off := int(math.RoundToEven(float64(extra) / 2))
		if n > 1 {
			off = extra * i / (n - 1)
			if i == n/2 {
				off = int(math.RoundToEven(float64(extra) / 2))
			}
		}
		if img.w > img.h {
			out = append(out, crop(resized, off, 0, side))
		} else {
			out = append(out, crop(resized, 0, off, side))
		}
	}
	if n > 1 && n*side < scaled {
		out = append(out, resize(img, side, side))
	}
	return out
}

// thin keeps at most n views, spread evenly.
func thin(v []rgb, n int) []rgb {
	if len(v) <= n {
		return v
	}
	out := make([]rgb, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, v[i*(len(v)-1)/(n-1)])
	}
	return out
}
