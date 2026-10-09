package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/internal/sdaptx"
	"sparkdream/tools/contentscan"
	servicetypes "sparkdream/x/service/types"
)

// fakeChain serves LCD paths (pagination query stripped) from a map and
// records broadcast messages.
type fakeChain struct {
	routes    map[string]string
	broadcast []sdk.Msg
}

func (f *fakeChain) GetJSON(_ context.Context, path string, out any) error {
	base := path
	if i := strings.Index(path, "?"); i >= 0 {
		base = path[:i]
		if strings.Contains(path, "pagination.offset=") && !strings.Contains(path, "pagination.offset=0") {
			return json.Unmarshal([]byte(`{}`), out) // single page
		}
	}
	body, ok := f.routes[base]
	if !ok {
		return fmt.Errorf("no route %s", base)
	}
	return json.Unmarshal([]byte(body), out)
}

func (f *fakeChain) SignAndBroadcast(_ context.Context, msgs ...sdk.Msg) (sdaptx.BroadcastResult, error) {
	f.broadcast = append(f.broadcast, msgs...)
	return sdaptx.BroadcastResult{TxHash: "ABC"}, nil
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func TestWorkerPoll(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nphoenix")
	inline := `<p>look</p><img src="data:image/png;base64,` + b64(png) + `">`
	inlineHash := sha256.Sum256([]byte(inline))
	plainWithURL := `<p>see <img src="https://aurora.example/cat.png"></p>`
	canaryBytes := []byte("\x89PNG\r\n\x1a\ncanary")
	canaryHex := contentscan.SHA256Hex(canaryBytes)

	chain := &fakeChain{routes: map[string]string{
		"/cosmos/base/tendermint/v1beta1/blocks/latest": `{"block":{"header":{"height":"700"}}}`,
		"/sparkdream/blog/v1/list_post": `{"post":[
			{"id":"1","body":"","content_type":"CONTENT_TYPE_HTML","media_flags":1,"body_hash":"` + b64(inlineHash[:]) + `","reply_count":"0"},
			{"id":"2","body":` + jsonString(plainWithURL) + `,"content_type":"CONTENT_TYPE_HTML"}]}`,
		"/sparkdream/blog/v1/post_body/1":                  `{"body":` + jsonString(inline) + `,"media_flags":1,"body_hash":"` + b64(inlineHash[:]) + `"}`,
		"/sparkdream/forum/v1/post":                        `{"post":[]}`,
		"/sparkdream/federation/v1/list_federated_content": `{"content":[]}`,
		"/sparkdream/collect/v1/public_collections":        `{"collections":[{"id":"4","cover_uri":"https://aurora.example/canary.png"}]}`,
		"/sparkdream/collect/v1/items/4":                   `{"items":[]}`,
	}}
	fetched := map[string][]byte{
		"https://aurora.example/cat.png":    []byte("\x89PNG\r\n\x1a\ncat"),
		"https://aurora.example/canary.png": canaryBytes,
	}

	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, 32))
	dir := t.TempDir()
	feed, err := contentscan.OpenFeed(filepath.Join(dir, "feed"), "sprkdrm1zenith", b64(key.Public().(ed25519.PublicKey)))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	newW := func() *Worker {
		return &Worker{
			cfg: Config{ChainID: "sparkdream-1", URITTL: 720 * time.Hour, CheckpointEvery: 600, MaxFetchBytes: 1 << 20,
				StateFile: filepath.Join(dir, "state.json")},
			chain: chain, feed: feed, key: key, operator: "sprkdrm1zenith",
			now:  func() time.Time { return now },
			done: map[string]time.Time{},
			detector: contentscan.Detector{
				Canary:     contentscan.NewHashList(map[string]string{canaryHex: contentscan.CategoryTest}),
				Classifier: classifierFunc(func(b contentscan.Blob) map[string]float64 { return map[string]float64{"sexual": 0.1} }),
				Ruleset:    contentscan.Ruleset{Version: "test", Thresholds: map[string]float64{"sexual": 0.8, "minor": 0.6}},
			},
			fetch: func(_ context.Context, u string) ([]byte, error) {
				if b, ok := fetched[u]; ok {
					return b, nil
				}
				return nil, fmt.Errorf("unexpected fetch %s", u)
			},
		}
	}

	w := newW()
	if err := w.poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	atts, err := contentscan.ReadSegments(filepath.Join(dir, "feed", "segments"))
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]contentscan.Attestation{}
	for _, a := range atts {
		if err := contentscan.Verify(a, key.Public().(ed25519.PublicKey)); err != nil {
			t.Fatalf("feed holds an unverifiable attestation: %v", err)
		}
		byKey[firstNonEmpty(a.Subject.URI, a.Subject.Record)] = a
	}
	if len(atts) != 3 {
		t.Fatalf("want 3 verdicts (inline body, url in plain post, canary cover), got %d: %+v", len(atts), atts)
	}
	if a := byKey["blog/post/1"]; a.Verdict != contentscan.VerdictClean || a.Subject.SHA256 != fmt.Sprintf("%x", inlineHash) {
		t.Fatalf("inline body verdict: %+v", a)
	}
	if a := byKey["https://aurora.example/cat.png"]; a.Verdict != contentscan.VerdictClean || a.ExpiresAt.IsZero() {
		t.Fatalf("url verdict must be clean and expiring: %+v", a)
	}
	if a := byKey["https://aurora.example/canary.png"]; a.Verdict != contentscan.VerdictRemoved || a.Category != contentscan.CategoryTest {
		t.Fatalf("canary verdict: %+v", a)
	}

	// Checkpoint: one MsgSubmitCheckpoint over the feed root.
	if len(chain.broadcast) != 1 {
		t.Fatalf("want one checkpoint, got %d", len(chain.broadcast))
	}
	cp := chain.broadcast[0].(*servicetypes.MsgSubmitCheckpoint)
	root := feed.Root()
	if cp.Height != 700 || !bytes.Equal(cp.Root, root[:]) || cp.ServiceType != servicetypes.ContentScannerServiceType {
		t.Fatalf("checkpoint %+v", cp)
	}
	if feed.Manifest().ScannedHeight != 700 {
		t.Fatal("manifest scanned height not advanced")
	}

	// A second poll (same process, then a restarted one) issues nothing new
	// and does not re-checkpoint within the window.
	if err := w.poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	restarted := newW()
	restarted.loadDone(atts)
	if err := restarted.poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if again, _ := contentscan.ReadSegments(filepath.Join(dir, "feed", "segments")); len(again) != 3 {
		t.Fatalf("re-issued verdicts: %d", len(again))
	}
	if len(chain.broadcast) != 1 {
		t.Fatalf("re-checkpointed within the window: %d", len(chain.broadcast))
	}

	// A body whose bytes do not match the chain's body_hash is not judged.
	chain.routes["/sparkdream/blog/v1/post_body/1"] = `{"body":"<p>swapped</p>","body_hash":"` + b64(inlineHash[:]) + `"}`
	fresh := newW()
	fresh.feed, _ = contentscan.OpenFeed(filepath.Join(dir, "feed2"), "sprkdrm1zenith", "k")
	if _, _, err := fresh.judge(context.Background(), fresh.bodySubject("blog/post/1", "CONTENT_TYPE_HTML", b64(inlineHash[:]), "/sparkdream/blog/v1/post_body/1", "body"), 700); err == nil {
		t.Fatal("judged a body that does not match the chain hash")
	}
}

type classifierFunc func(contentscan.Blob) map[string]float64

func (f classifierFunc) Classify(_ context.Context, b contentscan.Blob) (map[string]float64, error) {
	return f(b), nil
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestResolveURI(t *testing.T) {
	cfg := Config{Gateways: map[string]string{"CONTENT_TYPE_IPFS": "https://gw.example/ipfs/{id}", "CONTENT_TYPE_ARWEAVE": "https://ar.example/{id}"}}
	cases := []struct {
		in, want string
		ca       bool
	}{
		{"ipfs://bafyx", "https://gw.example/ipfs/bafyx", true},
		{"ar://tx1", "https://ar.example/tx1", true},
		{"https://a.example/x.png", "https://a.example/x.png", false},
		{"ftp://a.example/x", "", false},
	}
	for _, c := range cases {
		got, ca := cfg.resolveURI(c.in)
		if got != c.want || ca != c.ca {
			t.Errorf("%s: got %s %v", c.in, got, ca)
		}
	}
}
