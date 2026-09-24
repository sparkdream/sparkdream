package apcanon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func mediaNote(url string) map[string]any {
	obj, err := Parse([]byte(`{"id":"https://md.test/users/a/statuses/1","type":"Note","content":"<p>pic</p>",
		"attachment":[{"type":"Document","mediaType":"image/png","name":"alt","url":"` + url + `"}]}`))
	if err != nil {
		panic(err)
	}
	return obj
}

func fixedDigest(d string) MediaDigester {
	return func(context.Context, string) (string, error) { return d, nil }
}

// The point of v2: a file swapped behind the same URL moves the hash,
// where v1 cannot see it at all.
func TestV2SeesAMediaSwapThatV1CannotSee(t *testing.T) {
	obj := mediaNote("https://md.test/media/1.png")
	v1, _ := Hash(obj)
	before, err := HashV2(t.Context(), obj, fixedDigest("sha256:aa"))
	if err != nil {
		t.Fatal(err)
	}
	after, err := HashV2(t.Context(), obj, fixedDigest("sha256:bb"))
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("v2 must change when the file's bytes change")
	}
	if v1 == before {
		t.Fatal("v1 and v2 must not collide")
	}
	if v1again, _ := Hash(obj); v1again != v1 {
		t.Fatal("v1 is unchanged by v2's existence")
	}
}

func TestV2DigestsOnlyHTTPURLs(t *testing.T) {
	called := 0
	count := func(context.Context, string) (string, error) { called++; return "sha256:aa", nil }
	for _, u := range []string{"data:image/png;base64,AAAA", "javascript:x", ""} {
		c, err := CanonicalizeV2(t.Context(), mediaNote(u), count)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(c), `"digest":null`) {
			t.Fatalf("url %q: want a null digest, got %s", u, c)
		}
	}
	if called != 0 {
		t.Fatalf("digester called %d times for non-http urls", called)
	}
	// No attachment at all stays null, exactly like v1.
	obj, _ := Parse([]byte(`{"id":"x","type":"Note"}`))
	c, _ := CanonicalizeV2(t.Context(), obj, nil)
	if !strings.Contains(string(c), `"attachment":null`) {
		t.Fatalf("got %s", c)
	}
}

// A file that cannot be read fails the hash; nobody hashes a guess.
func TestV2PropagatesMediaErrors(t *testing.T) {
	boom := func(context.Context, string) (string, error) { return "", errors.New("404") }
	if _, err := HashV2(t.Context(), mediaNote("https://md.test/1.png"), boom); err == nil {
		t.Fatal("want an error when a file cannot be read")
	}
	if _, err := HashV2(t.Context(), mediaNote("https://md.test/1.png"), nil); err == nil {
		t.Fatal("want an error when v2 has no digester")
	}
}

func TestHashForDispatchesOnRule(t *testing.T) {
	obj := mediaNote("https://md.test/1.png")
	v1, _ := Hash(obj)
	for _, rule := range []string{"", HashRuleName} {
		if h, err := HashFor(t.Context(), rule, obj, nil); err != nil || h != v1 {
			t.Fatalf("rule %q: want v1", rule)
		}
	}
	v2, _ := HashV2(t.Context(), obj, fixedDigest("sha256:aa"))
	if h, err := HashFor(t.Context(), HashRuleV2, obj, fixedDigest("sha256:aa")); err != nil || h != v2 {
		t.Fatal("want v2")
	}
	if _, err := HashFor(t.Context(), "ap-canonical-v9", obj, nil); err == nil {
		t.Fatal("an unknown rule must be an error, never a silent fallback")
	}
}

func TestAttachmentURLShapes(t *testing.T) {
	cases := map[string]any{
		"https://a/1": "https://a/1",
		"https://a/2": map[string]any{"type": "Link", "href": "https://a/2"},
		"https://a/3": []any{map[string]any{"mediaType": "x"}, map[string]any{"href": "https://a/3"}},
		"":            42,
	}
	for want, in := range cases {
		if got := AttachmentURL(in); got != want {
			t.Errorf("AttachmentURL(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestFetchMediaDigest(t *testing.T) {
	var gotEncoding string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEncoding = r.Header.Get("Accept-Encoding")
		switch r.URL.Path {
		case "/ok.png":
			_, _ = w.Write([]byte("PNGDATA"))
		case "/huge.mp4":
			w.Header().Set("Content-Length", strconv.Itoa(MaxMediaBytes+1))
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	opts := FetchOptions{AllowPrivateHosts: true}

	d, err := FetchMediaDigest(t.Context(), srv.URL+"/ok.png", opts)
	if err != nil {
		t.Fatal(err)
	}
	if want := "sha256:" + sha256hex("PNGDATA"); d != want {
		t.Fatalf("digest = %q, want %q", d, want)
	}
	if gotEncoding != "identity" {
		t.Fatalf("Accept-Encoding = %q, want identity: transparent decompression would change the bytes hashed", gotEncoding)
	}
	again, _ := FetchMediaDigest(t.Context(), srv.URL+"/ok.png", opts)
	if again != d {
		t.Fatal("the same bytes must digest the same")
	}
	if d, err := FetchMediaDigest(t.Context(), srv.URL+"/huge.mp4", opts); err != nil || d != DigestOversize {
		t.Fatalf("oversize file: got %q, %v", d, err)
	}
	if _, err := FetchMediaDigest(t.Context(), srv.URL+"/missing.png", opts); err == nil {
		t.Fatal("a missing file must be an error")
	}
	// The destination guard applies to media exactly as to AS2 objects.
	if _, err := FetchMediaDigest(t.Context(), srv.URL+"/ok.png", FetchOptions{}); err == nil {
		t.Fatal("a loopback media URL must be refused without AllowPrivateHosts")
	}
}

func sha256hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func TestMediaBudgetScalesWithDeclaredSize(t *testing.T) {
	base := 2 * time.Minute
	if got := mediaBudget(base, DefaultMediaMinRate, 99_000_000); got < 5*time.Minute || got > 6*time.Minute {
		t.Fatalf("99 MB at 512 KiB/s: budget %s, want about five minutes", got)
	}
	if got := mediaBudget(base, DefaultMediaMinRate, 0); got != base {
		t.Fatalf("empty file: budget %s, want the base %s", got, base)
	}
	if unknown, cap := mediaBudget(base, DefaultMediaMinRate, -1), mediaBudget(base, DefaultMediaMinRate, MaxMediaBytes); unknown != cap {
		t.Fatalf("an undeclared size must get the full-cap budget: %s vs %s", unknown, cap)
	}
}

// A large file on a slow link finishes because its deadline grows with the
// declared size; a server that sends nothing times out, and says so.
func TestMediaDeadlineExtendsForLargeFiles(t *testing.T) {
	const size = 256 << 10
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/slow-but-steady.mp4":
			w.Header().Set("Content-Length", strconv.Itoa(size))
			chunk := make([]byte, size/8)
			for i := 0; i < 8; i++ { // ~400ms in total, well past the 100ms base
				_, _ = w.Write(chunk)
				w.(http.Flusher).Flush()
				time.Sleep(50 * time.Millisecond)
			}
		case "/stalled.mp4":
			time.Sleep(time.Second)
		}
	}))
	defer srv.Close()
	// 100ms base; at 128 KiB/s the declared 256 KiB adds 2s.
	opts := FetchOptions{AllowPrivateHosts: true, Timeout: 100 * time.Millisecond, MediaMinRate: 128 << 10}

	if d, err := FetchMediaDigest(t.Context(), srv.URL+"/slow-but-steady.mp4", opts); err != nil || !strings.HasPrefix(d, "sha256:") {
		t.Fatalf("a large file within its size-scaled budget must finish: %q %v", d, err)
	}
	_, err := FetchMediaDigest(t.Context(), srv.URL+"/stalled.mp4", opts)
	if !errors.Is(err, ErrMediaUnavailable) || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("a stalled server must fail as ErrMediaUnavailable with a timeout, got %v", err)
	}
}
