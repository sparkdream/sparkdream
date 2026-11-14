package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"sparkdream/tools/contentscan"
)

// feedHandler publishes the feed directory read-only (CS_LISTEN), for a host
// with no separate static server: on Akash a volume belongs to one service,
// so the worker serves its own feed. Feeds are public by design (§5.4) and
// clients verify every signature, so anyone may read them, from any origin.
func feedHandler(dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// the manifest and the newest segment change every poll
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	})
}

// checkServable refuses to publish a feed directory that holds the worker's
// private files: the state file and the verdict signing key.
func checkServable(cfg Config) error {
	feed, err := filepath.Abs(cfg.FeedDir)
	if err != nil {
		return err
	}
	for name, p := range map[string]string{"CS_STATE_FILE": cfg.StateFile, "CS_VERDICT_KEY_FILE": cfg.VerdictKeyFile} {
		if p == "" {
			continue
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			return err
		}
		if rel, err := filepath.Rel(feed, abs); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("CS_LISTEN would publish %s (%s): keep it outside CS_FEED_DIR", name, p)
		}
	}
	return nil
}

// precheckPath is where an upload service asks about bytes it has not stored
// yet (CS_PRECHECK_TOKEN).
const precheckPath = "/precheck"

// precheckResult is the answer to a pre-check. It is advice to the caller,
// not an attestation: nothing is signed and nothing enters the feed. The
// image gets its verdict the usual way, once a record on chain links to it.
type precheckResult struct {
	SHA256   string            `json:"sha256,omitempty"`
	Verdict  string            `json:"verdict,omitempty"`
	Category string            `json:"category,omitempty"`
	Ruleset  string            `json:"ruleset,omitempty"`
	Models   map[string]string `json:"models,omitempty"`
	Error    string            `json:"error,omitempty"`
}

// precheckHandler judges posted bytes with the worker's own detector (hash
// lists, then the classifier) so that a host can refuse an image before
// storing it. It is for the operator's own upload service, not the public:
// every call must carry the shared token, bodies are capped, and one image is
// judged at a time (classifying costs seconds of CPU; callers past the short
// queue are told to come back).
func precheckHandler(det contentscan.Detector, token string, maxBytes int64) http.Handler {
	running := make(chan struct{}, 1)
	waiting := make(chan struct{}, 4)
	reply := func(w http.ResponseWriter, status int, res precheckResult) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(res)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			reply(w, http.StatusMethodNotAllowed, precheckResult{Error: "method not allowed"})
			return
		}
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			reply(w, http.StatusUnauthorized, precheckResult{Error: "unauthorized"})
			return
		}
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBytes))
		if err != nil {
			reply(w, http.StatusRequestEntityTooLarge, precheckResult{Error: "body too large or unreadable"})
			return
		}
		if len(data) == 0 {
			reply(w, http.StatusBadRequest, precheckResult{Error: "empty body"})
			return
		}
		select {
		case waiting <- struct{}{}:
			defer func() { <-waiting }()
		default:
			w.Header().Set("Retry-After", "10")
			reply(w, http.StatusServiceUnavailable, precheckResult{Error: "busy"})
			return
		}
		select {
		case running <- struct{}{}:
			defer func() { <-running }()
		case <-r.Context().Done():
			return
		}

		res := precheckResult{SHA256: contentscan.SHA256Hex(data), Ruleset: det.Ruleset.Version, Models: det.Ruleset.Models}
		blobs, err := blobsOf(data, maxBytes)
		if err != nil {
			res.Error = "could not be read in full"
			reply(w, http.StatusUnprocessableEntity, res)
			return
		}
		// Text is "clean" to Judge because nothing in it is media. A
		// pre-check is asked about media, so say when there was none.
		if !contentscan.IsMedia(blobs[0].MediaType) {
			res.Error = "not media"
			reply(w, http.StatusUnprocessableEntity, res)
			return
		}
		res.Verdict, res.Category, err = det.Judge(r.Context(), blobs)
		switch {
		case errors.Is(err, contentscan.ErrNoClassifier):
			res.Error = "no classifier"
			reply(w, http.StatusServiceUnavailable, res)
		case errors.Is(err, contentscan.ErrUnsupportedMedia):
			res.Error = "unsupported media"
			reply(w, http.StatusUnprocessableEntity, res)
		case err != nil:
			log.Printf("contentscan: precheck %s: %v", res.SHA256, err)
			res.Error = "could not be checked"
			reply(w, http.StatusUnprocessableEntity, res)
		default:
			log.Printf("contentscan: precheck %s: %s %s", res.SHA256, res.Verdict, res.Category)
			reply(w, http.StatusOK, res)
		}
	})
}

// serveFeed runs the feed server until ctx ends, with the pre-check endpoint
// beside it when precheck is not nil. A server that cannot start stops the
// worker: a feed nobody can read earns no clean verdicts.
func serveFeed(ctx context.Context, addr, dir string, precheck http.Handler) {
	handler := feedHandler(dir)
	if precheck != nil {
		feed := handler
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == precheckPath {
				precheck.ServeHTTP(w, r)
				return
			}
			feed.ServeHTTP(w, r)
		})
	}
	// No overall write timeout: a pre-check answers only after classifying.
	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 60 * time.Second}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	log.Printf("contentscan: serving feed %s on %s", dir, addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("contentscan: feed server: %v", err)
	}
}
