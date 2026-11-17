package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"testing"
	"time"

	"sparkdream/tools/contentscan"
	storagetypes "sparkdream/x/storage/types"
)

func profilePNG(t *testing.T, shade uint8, alpha uint8) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 6, 6))
	for i := 0; i < 36; i++ {
		img.SetNRGBA(i%6, i/6, color.NRGBA{shade, uint8(i * 5), 40, alpha})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// The worker scans x/storage requests as subjects named by the request,
// tries every source until one serves the requested bytes, signs nothing for
// a file outside the storage profile, flags a removed hash on chain, and
// signs afresh when a request is reopened.
func TestWorkerStorageRequests(t *testing.T) {
	good := profilePNG(t, 10, 255)
	bad := profilePNG(t, 20, 255)         // on the takedown list
	transparent := profilePNG(t, 30, 0)   // outside the profile
	unreachable := profilePNG(t, 40, 255) // no source serves it
	hash := contentscan.SHA256Hex

	request := func(id string, data []byte, openedAt string, urls ...string) string {
		srcs := ""
		for i, u := range urls {
			if i > 0 {
				srcs += ","
			}
			srcs += `{"url":"` + u + `"}`
		}
		return fmt.Sprintf(`{"id":%q,"sha256":%q,"content_type":"image/png","scanned":true,"opened_at":%q,"sources":[%s]}`, id, hash(data), openedAt, srcs)
	}
	chain := &fakeChain{routes: map[string]string{
		"/cosmos/base/tendermint/v1beta1/blocks/latest":    `{"block":{"header":{"height":"900"}}}`,
		"/sparkdream/blog/v1/list_post":                    `{"post":[]}`,
		"/sparkdream/forum/v1/post":                        `{"post":[]}`,
		"/sparkdream/federation/v1/list_federated_content": `{"content":[]}`,
		"/sparkdream/collect/v1/public_collections":        `{"collections":[]}`,
		"/sparkdream/storage/v1/backends":                  `{"backends":[{"name":"arweave"}]}`,
		"/sparkdream/storage/v1/requests_by_status/arweave/REQUEST_STATUS_PENDING": `{"requests":[` +
			// The first source serves other bytes; the second serves the file.
			request("1", good, "100", "https://wrong.example/1.png", "https://aurora.example/1.png") + "," +
			request("2", bad, "100", "https://aurora.example/2.png") + "," +
			request("3", transparent, "100", "https://aurora.example/3.png") + "," +
			request("4", unreachable, "100", "https://dead.example/4.png") + "," +
			// A type listed without scanning is none of the worker's business.
			`{"id":"5","sha256":"` + hash([]byte("notes")) + `","content_type":"text/plain","scanned":false,"opened_at":"100","sources":[{"url":"https://aurora.example/5.txt"}]}` +
			`]}`,
		"/sparkdream/storage/v1/requests_by_status/arweave/REQUEST_STATUS_CLAIMED": `{"requests":[]}`,
		"/sparkdream/storage/v1/request_by_hash/arweave/" + hash(bad):              `{"request":{"id":"2"}}`,
	}}
	fetched := map[string][]byte{
		"https://wrong.example/1.png":  []byte("something else"),
		"https://aurora.example/1.png": good,
		"https://aurora.example/2.png": bad,
		"https://aurora.example/3.png": transparent,
	}

	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{9}, 32))
	dir := t.TempDir()
	feed, err := contentscan.OpenFeed(filepath.Join(dir, "feed"), "sprkdrm1aurora", b64(key.Public().(ed25519.PublicKey)))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	w := &Worker{
		cfg: Config{ChainID: "sparkdream-1", URITTL: 720 * time.Hour, MaxFetchBytes: 1 << 20, Storage: true,
			StateFile: filepath.Join(dir, "state.json")},
		chain: chain, feed: feed, key: key, operator: "sprkdrm1aurora",
		now:  func() time.Time { return now },
		done: map[string]time.Time{},
		detector: contentscan.Detector{
			Takedown:   contentscan.NewHashList(map[string]string{hash(bad): contentscan.CategoryIllegal}),
			Classifier: classifierFunc(func(contentscan.Blob) map[string]float64 { return map[string]float64{"sexual": 0.1} }),
			Ruleset:    contentscan.Ruleset{Version: "test", Thresholds: map[string]float64{"sexual": 0.8, "minor": 0.6}},
		},
		fetch: func(_ context.Context, u string) ([]byte, error) {
			if b, ok := fetched[u]; ok {
				return b, nil
			}
			return nil, fmt.Errorf("unreachable %s", u)
		},
	}
	if err := w.poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	atts, err := contentscan.ReadSegments(filepath.Join(dir, "feed", "segments"))
	if err != nil {
		t.Fatal(err)
	}
	byRecord := map[string]contentscan.Attestation{}
	for _, a := range atts {
		byRecord[a.Subject.Record] = a
	}
	if len(atts) != 2 {
		t.Fatalf("want verdicts for requests 1 and 2 only, got %d: %+v", len(atts), atts)
	}
	a1 := byRecord["storage/request/1"]
	if a1.Verdict != contentscan.VerdictClean || a1.Subject.SHA256 != hash(good) || a1.Subject.ChainID != "sparkdream-1" ||
		a1.ScannedHeight != 900 || !a1.ExpiresAt.IsZero() || a1.Subject.URI != "" {
		t.Fatalf("clean storage verdict: %+v", a1)
	}
	a2 := byRecord["storage/request/2"]
	if a2.Verdict != contentscan.VerdictRemoved {
		t.Fatalf("takedown-listed file: %+v", a2)
	}

	// The removed hash is flagged on chain, naming the verdict behind it.
	if len(chain.broadcast) != 1 {
		t.Fatalf("want one MsgFlagRemoved, got %d: %+v", len(chain.broadcast), chain.broadcast)
	}
	flag, ok := chain.broadcast[0].(*storagetypes.MsgFlagRemoved)
	wantHash, err := contentscan.AttestationHash(a2)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || flag.Scanner != "sprkdrm1aurora" || flag.Sha256 != hash(bad) || flag.AttestationHash != wantHash {
		t.Fatalf("flag: %+v", chain.broadcast[0])
	}
	if storagetypes.ValidateHash(flag.AttestationHash) != nil {
		t.Fatalf("attestation hash %q is not what the chain accepts", flag.AttestationHash)
	}

	// A second poll issues nothing new and flags nothing again.
	if err := w.poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if again, _ := contentscan.ReadSegments(filepath.Join(dir, "feed", "segments")); len(again) != 2 {
		t.Fatalf("re-issued verdicts: %d", len(again))
	}
	if len(chain.broadcast) != 1 {
		t.Fatalf("flagged again: %d", len(chain.broadcast))
	}

	// Request 1 is voided and reopened: its opened_at moves, and a verdict
	// from before that no longer counts, so the worker signs a fresh one.
	chain.routes["/sparkdream/storage/v1/requests_by_status/arweave/REQUEST_STATUS_PENDING"] = `{"requests":[` +
		request("1", good, "950", "https://aurora.example/1.png") + `]}`
	chain.routes["/cosmos/base/tendermint/v1beta1/blocks/latest"] = `{"block":{"header":{"height":"960"}}}`
	if err := w.poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	final, _ := contentscan.ReadSegments(filepath.Join(dir, "feed", "segments"))
	if len(final) != 3 || final[2].Subject.Record != "storage/request/1" || final[2].ScannedHeight != 960 {
		t.Fatalf("want a fresh verdict for the reopened request: %+v", final)
	}

	// With CS_STORAGE off the worker neither scans requests nor flags.
	off := *w
	off.cfg.Storage = false
	off.done = map[string]time.Time{}
	subjects, err := off.collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range subjects {
		if s.kind == kindStorage {
			t.Fatal("storage subject collected with storage off")
		}
	}
}

// A removed verdict on a post's media is flagged in x/storage when a storage
// request names the same bytes, and only then. The flag is owed until it is
// read back from the chain: an accepted broadcast is not a recorded flag.
func TestFlagRemovedForAStoredHash(t *testing.T) {
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{3}, 32))
	stored := contentscan.SHA256Hex([]byte("stored"))
	other := contentscan.SHA256Hex([]byte("other"))
	byHash := "/sparkdream/storage/v1/request_by_hash/arweave/" + stored
	removed := "/sparkdream/storage/v1/removed_hash/" + stored
	chain := &fakeChain{routes: map[string]string{byHash: `{"request":{"id":"7"}}`}}
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	state := filepath.Join(t.TempDir(), "state.json")
	w := &Worker{cfg: Config{Storage: true, StateFile: state}, chain: chain, operator: "sprkdrm1aurora",
		storageBackends: []string{"arweave"}, now: func() time.Time { return now }, done: map[string]time.Time{}}
	sign := func(sha, verdict string) contentscan.Attestation {
		a := contentscan.Attestation{
			V: contentscan.AttestationVersion, Worker: "sprkdrm1aurora", Verdict: verdict, Category: contentscan.CategoryIllegal,
			Ruleset: contentscan.Ruleset{Version: "test"}, ScannedHeight: 5, IssuedAt: time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC),
		}
		if verdict == contentscan.VerdictClean {
			a.Category = contentscan.CategoryNone
		}
		a.Subject.ChainID = "sparkdream-1"
		a.Subject.Record = "blog/post/1"
		a.Subject.SHA256 = sha
		signed, err := contentscan.Sign(a, key)
		if err != nil {
			t.Fatal(err)
		}
		return signed
	}
	ctx := context.Background()
	post := subject{kind: kindBody, record: "blog/post/1"}
	sent := func(want int, what string) {
		t.Helper()
		if len(chain.broadcast) != want {
			t.Fatalf("%s: %d flags sent, want %d", what, len(chain.broadcast), want)
		}
	}

	w.flagRemoved(ctx, post, sign(other, contentscan.VerdictRemoved)) // nothing stored under it
	w.flagRemoved(ctx, post, sign(stored, contentscan.VerdictClean))  // not removed
	w.sendFlags(ctx)
	sent(0, "without cause")

	w.flagRemoved(ctx, post, sign(stored, contentscan.VerdictRemoved))
	w.sendFlags(ctx)
	sent(1, "a stored hash")
	if chain.broadcast[0].(*storagetypes.MsgFlagRemoved).Sha256 != stored {
		t.Fatalf("flagged another hash: %+v", chain.broadcast[0])
	}

	// The broadcast was accepted, but the flag is not on chain (the
	// transaction failed in its block, or the per-window cap refused it).
	// It is left alone for a while, then sent again.
	w.sendFlags(ctx)
	sent(1, "before the retry time")
	if w.flags[stored] == nil {
		t.Fatal("the note was dropped before the flag was on chain")
	}
	// A restart keeps the note.
	if err := w.saveState(); err != nil {
		t.Fatal(err)
	}
	w.flags = nil
	w.loadDone(nil)
	if w.flags[stored] == nil {
		t.Fatal("a restart lost the flag still owed")
	}
	now = now.Add(flagRetry + time.Minute)
	// A node that cannot answer proves nothing either way.
	chain.down = true
	w.sendFlags(ctx)
	chain.down = false
	sent(1, "while the node is down")
	if w.flags[stored] == nil {
		t.Fatal("a failed query dropped the note")
	}
	w.sendFlags(ctx)
	sent(2, "after the retry time")

	// Another scanner's flag is not this scanner's.
	chain.routes[removed] = `{"removed_hash":{"flags":[{"scanner":"sprkdrm1zenith"}]}}`
	now = now.Add(flagRetry + time.Minute)
	w.sendFlags(ctx)
	sent(3, "only another scanner's flag is on chain")

	// Read back from the chain: done.
	chain.routes[removed] = `{"removed_hash":{"flags":[{"scanner":"sprkdrm1zenith"},{"scanner":"sprkdrm1aurora"}]}}`
	now = now.Add(flagRetry + time.Minute)
	w.sendFlags(ctx)
	sent(3, "once the flag is on chain")
	if len(w.flags) != 0 {
		t.Fatal("the note was kept after the flag was read back")
	}

	// A note is dropped when no request names the hash any more.
	delete(chain.routes, removed)
	w.flagRemoved(ctx, post, sign(stored, contentscan.VerdictRemoved))
	delete(chain.routes, byHash)
	w.sendFlags(ctx)
	sent(3, "no request left")
	if len(w.flags) != 0 {
		t.Fatal("the note outlived the request")
	}

	// Nor is one dropped while the backend list is unknown (the first query
	// after a start failed): that is "cannot tell", not "no request".
	chain.routes[byHash] = `{"request":{"id":"7"}}`
	w.flagRemoved(ctx, post, sign(stored, contentscan.VerdictRemoved))
	w.storageBackends = nil
	w.sendFlags(ctx)
	sent(3, "backends unknown")
	if w.flags[stored] == nil {
		t.Fatal("an unknown backend list dropped the note")
	}

	if _, err := contentscan.AttestationHash(contentscan.Attestation{}); err == nil {
		t.Fatal("hashed an unsigned attestation")
	}
}
