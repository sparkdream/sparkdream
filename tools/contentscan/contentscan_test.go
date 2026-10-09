package contentscan

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

func testKey(seed byte) ed25519.PrivateKey {
	return ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, ed25519.SeedSize))
}

func hashOf(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func att(worker, sha, verdict, category string) Attestation {
	return Attestation{
		V: AttestationVersion, Worker: worker,
		Subject: Subject{ChainID: "sparkdream-1", Record: "blog/post/1", SHA256: sha},
		Verdict: verdict, Category: category,
		Ruleset:  Ruleset{Version: "2026.10.1", Thresholds: map[string]float64{"sexual": 0.8, "minor": 0.6}},
		IssuedAt: t0, ScannedHeight: 100,
	}
}

func mustSign(t *testing.T, a Attestation, k ed25519.PrivateKey) Attestation {
	t.Helper()
	s, err := Sign(a, k)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// The canonical signing bytes are the cross-implementation contract:
// frontends must reproduce exactly these bytes to verify a verdict.
func TestSigningBytesAreCanonical(t *testing.T) {
	a := att("sprkdrm1aurora", hashOf("x"), VerdictClean, CategoryNone)
	got, err := a.SigningBytes()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"category":"none","issued_at":"2026-10-09T12:00:00Z","ruleset":{"thresholds":{"minor":0.6,"sexual":0.8},"version":"2026.10.1"},"scanned_height":100,"subject":{"chain_id":"sparkdream-1","record":"blog/post/1","sha256":"` + hashOf("x") + `"},"v":1,"verdict":"clean","worker":"sprkdrm1aurora"}`
	if string(got) != want {
		t.Fatalf("signing bytes\n got %s\nwant %s", got, want)
	}
	// The signature is not part of what is signed.
	a.Sig = "anything"
	again, _ := a.SigningBytes()
	if !bytes.Equal(got, again) {
		t.Fatal("sig leaked into signing bytes")
	}
}

func TestSignVerify(t *testing.T) {
	k := testKey(1)
	a := mustSign(t, att("sprkdrm1aurora", hashOf("x"), VerdictClean, CategoryNone), k)
	if err := Verify(a, k.Public().(ed25519.PublicKey)); err != nil {
		t.Fatal(err)
	}
	if err := Verify(a, testKey(2).Public().(ed25519.PublicKey)); err == nil {
		t.Fatal("verified under the wrong key")
	}
	tampered := a
	tampered.Verdict = VerdictRemoved
	tampered.Category = CategorySpam
	if err := Verify(tampered, k.Public().(ed25519.PublicKey)); err == nil {
		t.Fatal("tampered verdict verified")
	}
}

func TestValidateRejectsBadShapes(t *testing.T) {
	good := att("w", hashOf("x"), VerdictClean, CategoryNone)
	cases := map[string]func(a *Attestation){
		"version":  func(a *Attestation) { a.V = 2 },
		"worker":   func(a *Attestation) { a.Worker = "" },
		"hash":     func(a *Attestation) { a.Subject.SHA256 = "abc" },
		"verdict":  func(a *Attestation) { a.Verdict = "maybe" },
		"category": func(a *Attestation) { a.Category = "rude" },
		"uri needs expiry": func(a *Attestation) {
			a.Subject.Record = ""
			a.Subject.URI = "https://aurora.example/a.png"
		},
	}
	for name, mutate := range cases {
		a := good
		mutate(&a)
		if a.Validate() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestCombine(t *testing.T) {
	kA, kB, kC := testKey(1), testKey(2), testKey(3)
	workers := []TrustedWorker{
		{Address: "a", VerdictKey: kA.Public().(ed25519.PublicKey)},
		{Address: "b", VerdictKey: kB.Public().(ed25519.PublicKey)},
		{Address: "c", VerdictKey: kC.Public().(ed25519.PublicKey)},
	}
	h := hashOf("body")
	cleanA := mustSign(t, att("a", h, VerdictClean, CategoryNone), kA)
	cleanB := mustSign(t, att("b", h, VerdictClean, CategoryNone), kB)
	removedC := mustSign(t, att("c", h, VerdictRemoved, CategoryIllegal), kC)
	heldC := mustSign(t, att("c", h, VerdictHeld, CategorySexual), kC)
	cleanOtherHash := mustSign(t, att("b", hashOf("other"), VerdictClean, CategoryNone), kB)
	untrusted := mustSign(t, att("z", h, VerdictClean, CategoryNone), testKey(9))
	forged := cleanB
	forged.Worker = "c" // b's signature claimed by c

	tests := []struct {
		name   string
		atts   []Attestation
		quorum int
		want   string
		confl  bool
	}{
		{"one clean meets quorum 1", []Attestation{cleanA}, 1, VerdictClean, false},
		{"one clean misses quorum 2", []Attestation{cleanA}, 2, VerdictUnchecked, false},
		{"two clean meet quorum 2", []Attestation{cleanA, cleanB}, 2, VerdictClean, false},
		{"clean for another hash does not count", []Attestation{cleanA, cleanOtherHash}, 2, VerdictUnchecked, false},
		{"untrusted worker ignored", []Attestation{cleanA, untrusted}, 2, VerdictUnchecked, false},
		{"forged signature ignored", []Attestation{cleanA, forged}, 2, VerdictUnchecked, false},
		{"duplicate worker counts once", []Attestation{cleanA, cleanA}, 2, VerdictUnchecked, false},
		{"removed beats clean", []Attestation{cleanA, cleanB, removedC}, 2, VerdictRemoved, true},
		{"held beats clean", []Attestation{cleanA, cleanB, heldC}, 2, VerdictHeld, true},
		{"nothing is unchecked", nil, 1, VerdictUnchecked, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Combine(tt.atts, workers, h, tt.quorum, t0)
			if got.Verdict != tt.want || got.Conflict != tt.confl {
				t.Fatalf("got %+v, want %s conflict=%v", got, tt.want, tt.confl)
			}
		})
	}

	// A worker's latest verdict replaces its earlier one.
	later := att("c", h, VerdictClean, CategoryNone)
	later.IssuedAt = t0.Add(time.Hour)
	laterC := mustSign(t, later, kC)
	if got := Combine([]Attestation{heldC, laterC}, workers, h, 1, t0.Add(2*time.Hour)); got.Verdict != VerdictClean {
		t.Fatalf("latest verdict not used: %+v", got)
	}

	// An expired URI verdict no longer counts.
	uri := att("a", h, VerdictClean, CategoryNone)
	uri.Subject.Record, uri.Subject.URI = "", "https://aurora.example/a.png"
	uri.ExpiresAt = t0.Add(time.Hour)
	uriA := mustSign(t, uri, kA)
	if got := Combine([]Attestation{uriA}, workers, h, 1, t0.Add(30*time.Minute)); got.Verdict != VerdictClean {
		t.Fatalf("live uri verdict: %+v", got)
	}
	if got := Combine([]Attestation{uriA}, workers, h, 1, t0.Add(2*time.Hour)); got.Verdict != VerdictUnchecked {
		t.Fatalf("expired uri verdict counted: %+v", got)
	}
}

func TestEffectiveScannedHeightAndQuorum(t *testing.T) {
	if got := EffectiveScannedHeight([]int64{90, 120, 100}, 2); got != 100 {
		t.Fatalf("got %d", got)
	}
	if got := EffectiveScannedHeight([]int64{90}, 2); got != 0 {
		t.Fatalf("too few workers: got %d", got)
	}
	if QuorumFor(2, 3, true) != 3 || QuorumFor(2, 3, false) != 2 || QuorumFor(2, 0, true) != 2 {
		t.Fatal("QuorumFor")
	}
}

func TestMerkleRoot(t *testing.T) {
	leaf := func(s string) [32]byte { return sha256.Sum256([]byte(s)) }
	if MerkleRoot(nil) != sha256.Sum256(nil) {
		t.Fatal("empty root")
	}
	a, b, c := leaf("a"), leaf("b"), leaf("c")
	if MerkleRoot([][32]byte{a}) != a {
		t.Fatal("single leaf")
	}
	ab := sha256.Sum256(append(append([]byte{1}, a[:]...), b[:]...))
	if MerkleRoot([][32]byte{a, b}) != ab {
		t.Fatal("two leaves")
	}
	abc := sha256.Sum256(append(append([]byte{1}, ab[:]...), c[:]...))
	if MerkleRoot([][32]byte{a, b, c}) != abc {
		t.Fatal("three leaves (RFC 6962 split)")
	}
}

func TestFeedWriterRoundTrip(t *testing.T) {
	dir := t.TempDir()
	k := testKey(1)
	fw, err := OpenFeed(dir, "w", "key")
	if err != nil {
		t.Fatal(err)
	}
	a1 := mustSign(t, att("w", hashOf("1"), VerdictClean, CategoryNone), k)
	a2 := mustSign(t, att("w", hashOf("2"), VerdictRemoved, CategoryIllegal), k)
	a3 := mustSign(t, att("w", hashOf("3"), VerdictRemoved, CategoryTest), k)
	if err := fw.Append(a1, a2, a3); err != nil {
		t.Fatal(err)
	}
	if err := fw.SetScannedHeight(250); err != nil {
		t.Fatal(err)
	}
	if err := fw.Append(Attestation{}); err == nil {
		t.Fatal("appended an unsigned attestation")
	}

	// Reopening reproduces the root over the same entries.
	again, err := OpenFeed(dir, "w", "key")
	if err != nil {
		t.Fatal(err)
	}
	if again.Root() != fw.Root() || again.Manifest().Count != 3 || again.Manifest().ScannedHeight != 250 {
		t.Fatalf("reopened feed differs: %+v", again.Manifest())
	}
	if _, err := OpenFeed(dir, "someone-else", "key"); err == nil {
		t.Fatal("opened another worker's feed")
	}

	// Takedown list holds removed subjects but never canaries.
	tl, err := LoadHashList(filepath.Join(dir, "takedown-hashes.json"), "")
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := tl.Lookup(hashOf("2")); !ok || c != CategoryIllegal {
		t.Fatal("removed hash missing from takedown list")
	}
	if _, ok := tl.Lookup(hashOf("3")); ok {
		t.Fatal("canary leaked into takedown list")
	}
}

func TestDecodeAndExtract(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nzenith")
	html := `<p>hi</p><img src="data:image/png;base64,` + base64.StdEncoding.EncodeToString(png) + `"><img src="https://aurora.example/a.png"> ![x](ipfs://bafyphoenix)`

	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, _ = zw.Write([]byte(html))
	_ = zw.Close()
	body := base64.StdEncoding.EncodeToString(gz.Bytes())

	text, err := DecodeBody("CONTENT_TYPE_GZIP", body, 1<<20)
	if err != nil || string(text) != html {
		t.Fatalf("gzip decode: %v %q", err, text)
	}
	if _, err := DecodeBody("CONTENT_TYPE_GZIP", body, 10); err != ErrTooLarge {
		t.Fatalf("decompression cap not enforced: %v", err)
	}

	blobs := ExtractDataURIs(text, 1<<20)
	if len(blobs) != 1 || !bytes.Equal(blobs[0].Data, png) || blobs[0].MediaType != "image/png" {
		t.Fatalf("data uri blobs: %+v", blobs)
	}
	urls := ExtractURLs(text)
	if strings.Join(urls, " ") != "https://aurora.example/a.png ipfs://bafyphoenix" {
		t.Fatalf("urls: %v", urls)
	}
}

type fakeClassifier map[string]float64

func (f fakeClassifier) Classify(context.Context, Blob) (map[string]float64, error) { return f, nil }

func TestDetectorJudge(t *testing.T) {
	img := Blob{Data: []byte("img"), MediaType: "image/png"}
	text := Blob{Data: []byte("hello"), MediaType: "text/plain"}
	th := map[string]float64{"sexual": 0.8, "minor": 0.6, "violence": 0.9}
	ctx := context.Background()

	judge := func(d Detector, blobs ...Blob) (string, string, error) {
		d.Ruleset.Thresholds = th
		return d.Judge(ctx, blobs)
	}
	if v, c, err := judge(Detector{}); err != nil || v != VerdictClean || c != CategoryNone {
		t.Fatal("no media should be clean")
	}
	if v, _, err := judge(Detector{}, text); err != nil || v != VerdictClean {
		t.Fatal("text-only should be clean without a classifier")
	}
	if _, _, err := judge(Detector{}, img); err != ErrNoClassifier {
		t.Fatal("media without a classifier must stay unchecked")
	}
	canary := NewHashList(map[string]string{SHA256Hex(img.Data): CategoryTest})
	if v, c, _ := judge(Detector{Canary: canary}, img); v != VerdictRemoved || c != CategoryTest {
		t.Fatal("canary")
	}
	takedown := NewHashList(map[string]string{SHA256Hex(img.Data): CategoryIllegal})
	if v, c, _ := judge(Detector{Takedown: takedown}, img); v != VerdictRemoved || c != CategoryIllegal {
		t.Fatal("takedown")
	}
	if v, c, _ := judge(Detector{Classifier: fakeClassifier{"sexual": 0.9, "minor": 0.7}}, img); v != VerdictHeld || c != CategoryCSAM {
		t.Fatal("csam heuristic must hold, never remove")
	}
	if v, c, _ := judge(Detector{Classifier: fakeClassifier{"sexual": 0.9, "minor": 0.1}}, img); v != VerdictHeld || c != CategorySexual {
		t.Fatal("advisory sexual")
	}
	if v, _, _ := judge(Detector{Classifier: fakeClassifier{"sexual": 0.1}}, img); v != VerdictClean {
		t.Fatal("low scores are clean")
	}
}

func TestHTTPClassifier(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "image/png" {
			http.Error(w, "type", 400)
			return
		}
		_, _ = w.Write([]byte(`{"scores":{"sexual":0.25}}`))
	}))
	defer srv.Close()
	scores, err := HTTPClassifier{URL: srv.URL}.Classify(context.Background(), Blob{Data: []byte("x"), MediaType: "image/png"})
	if err != nil || scores["sexual"] != 0.25 {
		t.Fatalf("%v %v", scores, err)
	}
}

func TestFetchGuard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("a"), 100))
	}))
	defer srv.Close()
	ctx := context.Background()

	strict := FetchLimits{MaxBytes: 1 << 10, Timeout: 5 * time.Second}
	if _, err := Fetch(ctx, NewFetchClient(strict), srv.URL, strict); err == nil {
		t.Fatal("fetched plain http / loopback under the strict guard")
	}
	https := httptest.NewTLSServer(srv.Config.Handler)
	defer https.Close()
	if _, err := Fetch(ctx, NewFetchClient(strict), https.URL, strict); err == nil {
		t.Fatal("dialed a loopback address under the strict guard")
	}

	local := FetchLimits{MaxBytes: 1 << 10, Timeout: 5 * time.Second, AllowPrivate: true}
	if b, err := Fetch(ctx, NewFetchClient(local), srv.URL, local); err != nil || len(b) != 100 {
		t.Fatalf("local fetch: %v", err)
	}
	tiny := local
	tiny.MaxBytes = 10
	if _, err := Fetch(ctx, NewFetchClient(tiny), srv.URL, tiny); err != ErrTooLarge {
		t.Fatalf("size cap: %v", err)
	}
}

func TestOperatorMetadata(t *testing.T) {
	pub := testKey(1).Public().(ed25519.PublicKey)
	raw, err := OperatorMetadata{VerdictKey: base64.StdEncoding.EncodeToString(pub), FeedURL: "https://feeds.example/aurora", RulesetVersion: "2026.10.1"}.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	m, err := ParseOperatorMetadata(raw)
	if err != nil || m.V != 1 {
		t.Fatalf("%+v %v", m, err)
	}
	if k, _ := m.PublicKey(); !bytes.Equal(k, pub) {
		t.Fatal("key round trip")
	}
	if _, err := ParseOperatorMetadata([]byte(`{"verdict_key":"short","feed_url":"x"}`)); err == nil {
		t.Fatal("bad key accepted")
	}
}

func TestLoadHashListPlain(t *testing.T) {
	p := filepath.Join(t.TempDir(), "canary.txt")
	content := "# canaries\n" + strings.ToUpper(hashOf("c1")) + "\n" + hashOf("c2") + " spam\n"
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := LoadHashList(p, CategoryTest)
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := l.Lookup(hashOf("c1")); !ok || c != CategoryTest {
		t.Fatal("default category / case folding")
	}
	if c, _ := l.Lookup(hashOf("c2")); c != CategorySpam {
		t.Fatal("explicit category")
	}
}
