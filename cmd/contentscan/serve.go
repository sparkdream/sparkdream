package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"
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

// serveFeed runs the feed server until ctx ends. A server that cannot start
// stops the worker: a feed nobody can read earns no clean verdicts.
func serveFeed(ctx context.Context, addr, dir string) {
	srv := &http.Server{Addr: addr, Handler: feedHandler(dir), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	log.Printf("contentscan: serving feed %s on %s", dir, addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("contentscan: feed server: %v", err)
	}
}
