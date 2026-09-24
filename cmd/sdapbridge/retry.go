package main

import (
	"context"
	"errors"
	"log"
	"sort"
	"time"

	"sparkdream/tools/apcanon"
)

// Discovery keeps one cursor for every peer the process serves. A post
// that fails for a retryable reason (the peer's inbound rate limit, its
// instance unreachable, a transient chain error) used to hold that cursor
// until it succeeded, stalling every other peer behind one. Instead it is
// deferred here, per post, and retried with backoff while discovery moves
// on; a peer whose post keeps failing is held, so its new posts queue up
// behind it without spending a transaction fee on each certain rejection.
const (
	// maxDeferred bounds the queue. When it is full, discovery falls back
	// to holding the cursor: slower, but nothing is dropped.
	maxDeferred = 2000
	// deferredMaxAge is how long a post is retried before the bridge gives
	// up on it, loudly.
	deferredMaxAge = 24 * time.Hour
	retryBaseDelay = time.Minute
	retryMaxDelay  = 30 * time.Minute
)

// DeferredStatus is one post waiting for a retry. The whole Status is kept
// so the retry runs the same consent check and field mapping as discovery.
type DeferredStatus struct {
	Status   Status `json:"status"`
	PeerID   string `json:"peer_id"`
	Attempts int    `json:"attempts"`
	NextAt   int64  `json:"next_at"`
	Since    int64  `json:"since"` // first failure
	LastErr  string `json:"last_err"`
}

func retryDelay(attempts int) time.Duration {
	d := retryBaseDelay
	for i := 1; i < attempts && d < retryMaxDelay; i++ {
		d *= 2
	}
	return min(d, retryMaxDelay)
}

// Defer queues st for peerID after a failure. It returns false when the
// queue is full; the caller must then hold its cursor.
func (s *State) Defer(st Status, peerID string, cause error, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.Deferred[st.URI]
	if !ok {
		if len(s.Deferred) >= maxDeferred {
			return false
		}
		d = &DeferredStatus{Status: st, PeerID: peerID, Since: now.Unix()}
		s.Deferred[st.URI] = d
	}
	d.Attempts++
	d.NextAt = now.Add(retryDelay(d.Attempts)).Unix()
	if cause != nil {
		d.LastErr = cause.Error()
	}
	return true
}

// Undefer removes uri from the queue (anchored, or settled for good).
func (s *State) Undefer(uri string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Deferred, uri)
}

// DueDeferred returns the queued posts whose retry is due, oldest status
// first, and drops (logging) those past deferredMaxAge.
func (s *State) DueDeferred(now time.Time) []DeferredStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	var due []DeferredStatus
	for uri, d := range s.Deferred {
		if now.Sub(time.Unix(d.Since, 0)) > deferredMaxAge {
			log.Printf("sdapbridge: GAVE UP on %s (peer %s) after %d attempts over %s: %s",
				uri, d.PeerID, d.Attempts, deferredMaxAge, d.LastErr)
			delete(s.Deferred, uri)
			continue
		}
		if d.NextAt <= now.Unix() {
			due = append(due, *d)
		}
	}
	sort.Slice(due, func(i, j int) bool { return statusIDLess(due[i].Status.ID, due[j].Status.ID) })
	return due
}

// deferOrHold queues a failed post. It reports false when the post must
// instead hold the discovery cursor: the bridge's own instance is
// throttling (a global condition the Run loop backs off from), or the
// queue is full.
func (b *Bridge) deferOrHold(st *Status, cause error) bool {
	var rl *RateLimitError
	if errors.As(cause, &rl) {
		return false
	}
	peerID, err := b.peers.route(st.URI)
	if err != nil {
		return true // no peer owns it: nothing to retry
	}
	now := time.Now()
	if !b.state.Defer(*st, peerID, cause, now) {
		log.Printf("sdapbridge: retry queue full (%d); holding the cursor at %s", maxDeferred, st.URI)
		return false
	}
	b.holdPeer(peerID, now.Add(retryDelay(1)))
	if errors.Is(cause, apcanon.ErrMediaUnavailable) {
		log.Printf("sdapbridge: deferred %s (peer %s): an attachment's file could not be downloaded (%v); "+
			"retrying with backoff. For a large file on a slow link, raise SDA_MEDIA_TIMEOUT or lower SDA_MEDIA_MIN_RATE.",
			st.URI, peerID, cause)
		return true
	}
	log.Printf("sdapbridge: deferred %s (peer %s): %v", st.URI, peerID, cause)
	return true
}

// holdPeer stops new attempts for peerID until the given time.
func (b *Bridge) holdPeer(peerID string, until time.Time) {
	if b.heldUntil == nil {
		b.heldUntil = map[string]time.Time{}
	}
	if until.After(b.heldUntil[peerID]) {
		b.heldUntil[peerID] = until
	}
}

// peerHeld reports whether uri's peer is currently held, and which peer.
func (b *Bridge) peerHeld(uri string) (string, bool) {
	peerID, err := b.peers.route(uri)
	if err != nil {
		return "", false
	}
	return peerID, time.Now().Before(b.heldUntil[peerID])
}

// retryDeferred re-runs due posts, oldest first. The first failure for a
// peer holds that peer until its next due time, so a throttled peer gets
// one probe per backoff step rather than one per queued post. Posts that
// now succeed, or are settled for good (consent withdrawn, refused by
// policy — IngestStatus returns nil for those), leave the queue.
func (b *Bridge) retryDeferred(ctx context.Context) error {
	now := time.Now()
	for _, d := range b.state.DueDeferred(now) {
		if now.Before(b.heldUntil[d.PeerID]) {
			continue
		}
		st := d.Status
		err := b.IngestStatus(ctx, &st)
		if err == nil {
			b.state.Undefer(st.URI)
			log.Printf("sdapbridge: retry of %s succeeded after %d attempt(s)", st.URI, d.Attempts)
			continue
		}
		var rl *RateLimitError
		if errors.As(err, &rl) {
			_ = b.state.Save()
			return err // the bridge's own instance: back off globally
		}
		b.state.Defer(st, d.PeerID, err, now)
		b.holdPeer(d.PeerID, now.Add(retryDelay(d.Attempts+1)))
	}
	return b.state.Save()
}
