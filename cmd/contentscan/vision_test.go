package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"sparkdream/tools/contentscan"
	"sparkdream/tools/contentscan/vision"
)

// TestWorkerBuiltInImageModel runs a poll with the in-process image
// classifier (a tiny random model from the vision fixtures: the scores mean
// nothing, the plumbing is what is tested). A picture linked from a post
// gets a signed verdict for its exact bytes; a video and an SVG get none, and
// the video is left alone until the retry interval has passed.
func TestWorkerBuiltInImageModel(t *testing.T) {
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join("..", "..", "tools", "contentscan", "vision", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	classifier, err := vision.New(read("tiny-vit.safetensors"))
	if err != nil {
		t.Fatal(err)
	}
	picture := read("picture.png")
	video := []byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom")
	drawing := []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><rect width="8" height="8"/></svg>`)
	body := "A dream.\n\n![a picture](https://aurora.example/picture.png)\n\n![a clip](https://aurora.example/clip.mp4)\n\n![a drawing](https://aurora.example/drawing.svg)"

	chain := &fakeChain{routes: map[string]string{
		"/cosmos/base/tendermint/v1beta1/blocks/latest":    `{"block":{"header":{"height":"700"}}}`,
		"/sparkdream/blog/v1/list_post":                    `{"post":[{"id":"1","body":` + jsonString(body) + `,"content_type":"CONTENT_TYPE_MARKDOWN","reply_count":"0"}]}`,
		"/sparkdream/forum/v1/post":                        `{"post":[]}`,
		"/sparkdream/federation/v1/list_federated_content": `{"content":[]}`,
		"/sparkdream/collect/v1/public_collections":        `{"collections":[]}`,
	}}
	fetches := map[string]int{}
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{9}, 32))
	dir := t.TempDir()
	feed, err := contentscan.OpenFeed(filepath.Join(dir, "feed"), "sprkdrm1zenith", b64(key.Public().(ed25519.PublicKey)))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	w := &Worker{
		cfg: Config{ChainID: "sparkdream-1", URITTL: 720 * time.Hour, MaxFetchBytes: 1 << 20,
			StateFile: filepath.Join(dir, "state.json")},
		chain: chain, feed: feed, key: key, operator: "sprkdrm1zenith",
		now:  func() time.Time { return now },
		done: map[string]time.Time{},
		detector: contentscan.Detector{
			Classifier: classifier,
			Ruleset:    contentscan.Ruleset{Version: "test", Models: map[string]string{vision.ModelName: classifier.ModelHash()}},
		},
		fetch: func(_ context.Context, u string) ([]byte, error) {
			fetches[u]++
			switch u {
			case "https://aurora.example/picture.png":
				return picture, nil
			case "https://aurora.example/clip.mp4":
				return video, nil
			case "https://aurora.example/drawing.svg":
				return drawing, nil
			}
			return nil, fmt.Errorf("unexpected fetch %s", u)
		},
	}

	poll := func() {
		t.Helper()
		if err := w.poll(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	poll()
	atts, err := contentscan.ReadSegments(filepath.Join(dir, "feed", "segments"))
	if err != nil {
		t.Fatal(err)
	}
	var pictureVerdict *contentscan.Attestation
	for i, a := range atts {
		switch a.Subject.URI {
		case "https://aurora.example/picture.png":
			pictureVerdict = &atts[i]
		case "https://aurora.example/clip.mp4":
			t.Errorf("a video got a verdict (%s) from an image model", a.Verdict)
		case "https://aurora.example/drawing.svg":
			t.Errorf("an SVG got a verdict (%s)", a.Verdict)
		}
	}
	if pictureVerdict == nil {
		t.Fatal("the picture got no verdict")
	}
	if pictureVerdict.Verdict != contentscan.VerdictClean || pictureVerdict.Subject.SHA256 != contentscan.SHA256Hex(picture) {
		t.Errorf("picture verdict = %s for %s", pictureVerdict.Verdict, pictureVerdict.Subject.SHA256)
	}
	if got := pictureVerdict.Ruleset.Models[vision.ModelName]; got != classifier.ModelHash() {
		t.Errorf("verdict names model %q, want %q", got, classifier.ModelHash())
	}

	// The next polls leave both alone: one has its verdict, the other waits.
	poll()
	poll()
	if fetches["https://aurora.example/picture.png"] != 1 || fetches["https://aurora.example/clip.mp4"] != 1 {
		t.Errorf("fetch counts after three polls: %v", fetches)
	}
	// After the retry interval the video is tried again.
	now = now.Add(unsupportedRetry + time.Minute)
	poll()
	if fetches["https://aurora.example/clip.mp4"] != 2 {
		t.Errorf("the video was fetched %d times, want 2 after the retry interval", fetches["https://aurora.example/clip.mp4"])
	}
}
