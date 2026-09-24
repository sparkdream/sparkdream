package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sparkdream/x/federation/types"
)

// twoPeerBridge serves a home timeline of three posts, oldest first:
// aurora.test/1, md.test/2, aurora.test/3. The submitter throttles
// aurora.test while throttle is set, and counts its attempts.
func twoPeerBridge(t *testing.T) (*Bridge, *fakeSubmitter, *int, *bool) {
	t.Helper()
	b, sub := testBridge(t, nil)
	b.peers = newPeerSet([]string{"md.test", "aurora.test"}, nil)
	uris := map[string]string{
		"1": "https://aurora.test/users/a/statuses/1",
		"2": "https://md.test/users/b/statuses/2",
		"3": "https://aurora.test/users/a/statuses/3",
	}
	m := mapFetcher{}
	for _, u := range uris {
		m[u] = noteAt(u, time.Now().UTC().Format(time.RFC3339))
	}
	b.fetcher = m

	auroraAttempts, throttle := 0, true
	sub.hook = func(msg *types.MsgSubmitFederatedContent) error {
		if msg.PeerId != "aurora.test" {
			return nil
		}
		auroraAttempts++
		if throttle {
			return fmt.Errorf("%w: inbound window full", ErrPeerThrottled)
		}
		return nil
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("since_id") != "" {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		var items []string
		for _, id := range []string{"3", "2", "1"} { // newest first
			// Authors live on their post's instance: "a" locally on
			// md.test, "a@aurora.test" for aurora.test's posts.
			acct := "a"
			if strings.Contains(uris[id], "aurora.test") {
				acct = "a@aurora.test"
			}
			items = append(items, `{"id":"`+id+`","uri":"`+uris[id]+`","visibility":"public","account":{"acct":"`+acct+`","uri":"x"}}`)
		}
		_, _ = w.Write([]byte("[" + strings.Join(items, ",") + "]"))
	}))
	t.Cleanup(srv.Close)
	b.masto = NewMastodonClient(srv.URL, "token")
	return b, sub, &auroraAttempts, &throttle
}

// One throttled peer must not stall another: its post is deferred, the
// cursor moves past everything, and its next post queues without spending
// a transaction on a certain rejection.
func TestThrottledPeerDoesNotStallOthers(t *testing.T) {
	b, sub, auroraAttempts, _ := twoPeerBridge(t)

	if err := b.pollOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(sub.msgs) != 1 || sub.msgs[0].PeerId != "md.test" {
		t.Fatalf("md.test's post must be anchored despite aurora.test throttling: %d submissions", len(sub.msgs))
	}
	if got := b.state.LastSeenStatusID(); got != "3" {
		t.Fatalf("cursor = %q, want 3: the throttled peer must not hold it", got)
	}
	if *auroraAttempts != 1 {
		t.Fatalf("aurora.test got %d submit attempts, want 1: its second post must queue while it is held", *auroraAttempts)
	}
	if len(b.state.Deferred) != 2 {
		t.Fatalf("want both aurora.test posts deferred, got %d", len(b.state.Deferred))
	}
}

// Once due and unthrottled, deferred posts are anchored and leave the queue.
func TestDeferredPostsAreRetried(t *testing.T) {
	b, sub, auroraAttempts, throttle := twoPeerBridge(t)
	if err := b.pollOnce(t.Context()); err != nil {
		t.Fatal(err)
	}

	// Still throttled when due: one probe for the peer, not one per post.
	makeDue(b)
	before := *auroraAttempts
	if err := b.retryDeferred(t.Context()); err != nil {
		t.Fatal(err)
	}
	if *auroraAttempts-before != 1 {
		t.Fatalf("a throttled peer got %d probes in one retry pass, want 1", *auroraAttempts-before)
	}

	*throttle = false
	makeDue(b)
	if err := b.retryDeferred(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(b.state.Deferred) != 0 {
		t.Fatalf("queue should be empty, has %d", len(b.state.Deferred))
	}
	aurora := 0
	for _, m := range sub.msgs {
		if m.PeerId == "aurora.test" {
			aurora++
		}
	}
	if aurora != 2 {
		t.Fatalf("want both aurora.test posts anchored on retry, got %d", aurora)
	}
}

func makeDue(b *Bridge) {
	for _, d := range b.state.Deferred {
		d.NextAt = 0
	}
	b.heldUntil = nil
}

// A full queue must not drop posts: discovery falls back to holding the
// cursor.
func TestFullRetryQueueHoldsTheCursor(t *testing.T) {
	b, _, _, _ := twoPeerBridge(t)
	for i := 0; i < maxDeferred; i++ {
		u := fmt.Sprintf("https://aurora.test/filler/%d", i)
		b.state.Deferred[u] = &DeferredStatus{Status: Status{URI: u}, PeerID: "aurora.test", NextAt: time.Now().Add(time.Hour).Unix(), Since: time.Now().Unix()}
	}
	if err := b.pollOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := b.state.LastSeenStatusID(); got != "" {
		t.Fatalf("cursor = %q, want it held before the first post that could not be queued", got)
	}
}

// The bridge's own instance throttling is global: nothing to gain by
// queueing per peer, so the cursor holds and the Run loop backs off.
func TestOwnInstanceRateLimitHoldsTheCursor(t *testing.T) {
	b, _, _, _ := twoPeerBridge(t)
	b.fetcher = throttledFetcher{}
	if err := b.pollOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := b.state.LastSeenStatusID(); got != "" || len(b.state.Deferred) != 0 {
		t.Fatalf("cursor=%q deferred=%d; want the cursor held and nothing queued", got, len(b.state.Deferred))
	}
}

type throttledFetcher struct{}

func (throttledFetcher) Fetch(context.Context, string) (map[string]any, error) {
	return nil, &RateLimitError{RetryAfter: time.Minute}
}

// A post that keeps failing is eventually given up on, not retried forever.
func TestDeferredGivesUpAfterMaxAge(t *testing.T) {
	b, _ := testBridge(t, nil)
	u := "https://md.test/users/a/statuses/9"
	b.state.Deferred[u] = &DeferredStatus{Status: Status{URI: u}, PeerID: "md.test",
		Since: time.Now().Add(-deferredMaxAge - time.Minute).Unix()}
	if due := b.state.DueDeferred(time.Now()); len(due) != 0 {
		t.Fatalf("an expired post was returned as due: %v", due)
	}
	if _, ok := b.state.Deferred[u]; ok {
		t.Fatal("an expired post must leave the queue")
	}
}

func TestRetryDelayBacksOffAndCaps(t *testing.T) {
	want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 30 * time.Minute, 30 * time.Minute}
	for i, w := range want {
		if got := retryDelay(i + 1); got != w {
			t.Errorf("retryDelay(%d) = %s, want %s", i+1, got, w)
		}
	}
}
