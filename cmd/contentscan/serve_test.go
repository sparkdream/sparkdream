package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestFeedHandler(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(`{"v":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	h := feedHandler(dir)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/manifest.json", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != `{"v":1}` {
		t.Fatalf("GET manifest: %d %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("CORS header %q", got)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/manifest.json", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: %d, want 405", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/../serve.go", nil))
	if rec.Code == http.StatusOK {
		t.Fatal("served a file outside the feed directory")
	}
}

func TestCheckServable(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		ok   bool
	}{
		{"private files beside the feed", Config{FeedDir: "/data/feed", StateFile: "/data/state.json", VerdictKeyFile: "/data/verdict.key"}, true},
		{"sibling with a shared prefix", Config{FeedDir: "/data/feed", StateFile: "/data/feed-state.json"}, true},
		{"state inside the feed", Config{FeedDir: "/data/feed", StateFile: "/data/feed/state.json"}, false},
		{"key inside the feed", Config{FeedDir: "feed", VerdictKeyFile: "feed/sub/verdict.key"}, false},
		{"feed is the working directory", Config{FeedDir: ".", StateFile: "contentscan-state.json"}, false},
	}
	for _, c := range cases {
		err := checkServable(c.cfg)
		if (err == nil) != c.ok {
			t.Errorf("%s: err = %v, want ok=%v", c.name, err, c.ok)
		}
	}
}
