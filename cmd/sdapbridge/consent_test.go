package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"sparkdream/tools/apcanon"
)

func boolPtr(v bool) *bool { return &v }

func TestOptedOutTags(t *testing.T) {
	cases := []struct {
		name string
		acct Account
		want bool
	}{
		// Mastodon renders a bio hashtag as #<span>nobridge</span>.
		{"rendered bio hashtag", Account{Note: `<p>hi <a href="x" class="mention hashtag">#<span>nobridge</span></a></p>`}, true},
		{"profile field", Account{Fields: []Field{{Name: "bots", Value: "#NoBot"}}}, true},
		{"field name", Account{Fields: []Field{{Name: "#nobridge", Value: "please"}}}, true},
		{"longer tag is not the tag", Account{Note: "#nobridges #nobotanics"}, false},
		{"plain words", Account{Note: "no bridge here, no bot"}, false},
		{"nothing", Account{}, false},
	}
	for _, c := range cases {
		if got := optedOut(&c.acct); got != c.want {
			t.Errorf("%s: optedOut = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestConsentIndexable(t *testing.T) {
	b, _ := testBridge(t, nil)
	b.cfg.Consent = ConsentIndexable
	cases := []struct {
		name string
		acct *Account
		want bool
	}{
		{"indexable", &Account{URI: "https://md.test/users/a", Indexable: boolPtr(true)}, true},
		{"not indexable", &Account{URI: "https://md.test/users/a", Indexable: boolPtr(false)}, false},
		{"server did not say", &Account{URI: "https://md.test/users/a"}, false},
		{"indexable but #nobridge", &Account{URI: "https://md.test/users/a", Indexable: boolPtr(true), Note: "#nobridge"}, false},
		{"no account: fail closed", nil, false},
	}
	for _, c := range cases {
		if got, reason, _ := b.consents(t.Context(), c.acct); got != c.want {
			t.Errorf("%s: consents = %v (%s), want %v", c.name, got, reason, c.want)
		}
	}
}

// Opt-in: only authors following the bridge account, read once per TTL.
func TestConsentOptIn(t *testing.T) {
	b, _ := testBridge(t, nil)
	b.cfg.Consent = ConsentOptIn
	var followerReads atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/accounts/verify_credentials":
			_, _ = w.Write([]byte(`{"id":"1"}`))
		case "/api/v1/accounts/1/followers":
			followerReads.Add(1)
			_, _ = w.Write([]byte(`[{"id":"2","uri":"https://md.test/users/alice"}]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	b.masto = NewMastodonClient(srv.URL, "token")

	alice := &Account{URI: "https://md.test/users/alice"}
	bob := &Account{URI: "https://md.test/users/bob"}
	if ok, reason, _ := b.consents(t.Context(), alice); !ok {
		t.Fatalf("a follower of the bridge must be allowed: %s", reason)
	}
	if ok, _, _ := b.consents(t.Context(), bob); ok {
		t.Fatal("an author who does not follow the bridge must be refused")
	}
	alice.Note = "changed my mind #nobridge"
	if ok, _, _ := b.consents(t.Context(), alice); ok {
		t.Fatal("#nobridge overrides a follow")
	}
	if n := followerReads.Load(); n != 1 {
		t.Fatalf("followers read %d times within the TTL, want 1", n)
	}
}

// A refused author's post is never fetched, and does not hold up the
// timeline cursor.
func TestRefusedAuthorIsNeverFetched(t *testing.T) {
	b, sub := testBridge(t, map[string]string{"/users/alice/statuses/10": publicNote})
	b.cfg.Consent = ConsentIndexable
	fetch := &countingFetcher{AS2Source: b.fetcher}
	b.fetcher = fetch
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"10","uri":"https://md.test/users/alice/statuses/10",
			"account":{"acct":"alice","uri":"https://md.test/users/alice","indexable":false}}]`))
	}))
	defer srv.Close()
	b.masto = NewMastodonClient(srv.URL, "token")

	if err := b.pollOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if fetch.n != 0 || len(sub.msgs) != 0 {
		t.Fatalf("refused author: %d fetches, %d submissions; want none", fetch.n, len(sub.msgs))
	}
	if got := b.state.LastSeenStatusID(); got != "10" {
		t.Fatalf("cursor = %q, want 10: a refusal must not pin it", got)
	}
}

// No outbox walk for an author who has not consented, and the pass is not
// marked done, so a later opt-in still gets it.
func TestBackfillSkippedWithoutConsent(t *testing.T) {
	b, _ := testBridge(t, nil)
	b.cfg.Consent = ConsentIndexable
	b.cfg.Backfill = 5
	var outboxHits atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		outboxHits.Add(1)
		_, _ = w.Write([]byte(`{"orderedItems":[]}`))
	}))
	defer remote.Close()
	b.masto = NewMastodonClient("https://local.instance", "token")

	acct := &Account{URI: remote.URL + "/users/alice", Indexable: boolPtr(false)}
	b.backfill(t.Context(), acct)
	if outboxHits.Load() != 0 || b.state.Backfilled(acct.URI) {
		t.Fatalf("outbox hits=%d backfilled=%v; want no walk and not marked", outboxHits.Load(), b.state.Backfilled(acct.URI))
	}
}

// mapFetcher serves AS2 objects by absolute URI, for multi-host tests.
type mapFetcher map[string]string

func (m mapFetcher) Fetch(_ context.Context, uri string) (map[string]any, error) {
	body, ok := m[uri]
	if !ok {
		return nil, context.DeadlineExceeded
	}
	return apcanon.Parse([]byte(body))
}

// One process, several peers: each post goes to the peer that owns its
// host (content_hosts included); any other instance's post is skipped.
func TestMultiPeerRouting(t *testing.T) {
	b, sub := testBridge(t, nil)
	b.peers = newPeerSet([]string{"md.test", "aurora.test"}, nil)
	b.peers.hosts["aurora.test"] = []string{"social.aurora.test"}
	note := func(id string) string {
		return `{"id":"` + id + `","type":"Note","attributedTo":"x","content":"` + id + ` #cc0",
			"to":["https://www.w3.org/ns/activitystreams#Public"]}`
	}
	uris := []string{
		"https://md.test/users/a/statuses/1",
		"https://aurora.test/users/b/statuses/2",
		"https://social.aurora.test/users/c/statuses/3",
		"https://zenith.test/users/d/statuses/4",
	}
	m := mapFetcher{}
	for _, u := range uris {
		m[u] = note(u)
	}
	b.fetcher = m

	for _, u := range uris {
		if err := b.IngestURI(t.Context(), u, &Account{URI: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	for _, msg := range sub.msgs {
		got = append(got, msg.PeerId)
	}
	if strings.Join(got, ",") != "md.test,aurora.test,aurora.test" {
		t.Fatalf("peers = %v, want md.test, aurora.test, aurora.test and zenith.test skipped", got)
	}
}

func TestSplitList(t *testing.T) {
	if got := splitList(" md.test, ,aurora.test,"); strings.Join(got, "|") != "md.test|aurora.test" {
		t.Fatalf("splitList = %q", got)
	}
}

func noteAt(id, published string) string {
	return `{"id":"` + id + `","type":"Note","attributedTo":"x","content":"` + id + ` #cc0","published":"` + published + `",
		"to":["https://www.w3.org/ns/activitystreams#Public"]}`
}

// Opting in covers what comes after it: an author's earlier posts (the
// reconcile lookback and outbox backfill would otherwise reach them) stay
// off the chain.
func TestPostsBeforeConsentAreNotAnchored(t *testing.T) {
	b, sub := testBridge(t, nil)
	b.cfg.Consent = ConsentIndexable
	before := "https://md.test/users/a/statuses/1"
	after := "https://md.test/users/a/statuses/2"
	b.fetcher = mapFetcher{
		before: noteAt(before, time.Now().Add(-48*time.Hour).UTC().Format(time.RFC3339)),
		after:  noteAt(after, time.Now().UTC().Format(time.RFC3339)),
	}
	acct := &Account{URI: "https://md.test/users/a", Indexable: boolPtr(true)}

	for _, u := range []string{before, after} {
		if err := b.IngestURI(t.Context(), u, acct); err != nil {
			t.Fatal(err)
		}
	}
	if len(sub.msgs) != 1 || sub.msgs[0].ContentUri != after {
		t.Fatalf("want only the post after consent anchored, got %d", len(sub.msgs))
	}
	st := &Status{ID: "1", URI: before, Visibility: "public", Account: *acct,
		CreatedAt: time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339)}
	if b.reconcileCandidate(t.Context(), st, time.Now()) {
		t.Fatal("a pre-consent post must not count as missed by the timeline")
	}
}

// An unreadable followers list is "undetermined", not a withdrawal: the
// recorded consent start survives. A real withdrawal clears it.
func TestConsentStartSurvivesTransientErrors(t *testing.T) {
	b, _ := testBridge(t, nil)
	b.cfg.Consent = ConsentOptIn
	fail := atomic.Bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case fail.Load():
			w.WriteHeader(http.StatusBadGateway)
		case r.URL.Path == "/api/v1/accounts/verify_credentials":
			_, _ = w.Write([]byte(`{"id":"1"}`))
		default:
			_, _ = w.Write([]byte(`[{"id":"2","uri":"https://md.test/users/alice"}]`))
		}
	}))
	defer srv.Close()
	b.masto = NewMastodonClient(srv.URL, "token")
	alice := &Account{URI: "https://md.test/users/alice"}

	if !b.allowed(t.Context(), alice) {
		t.Fatal("setup: alice follows the bridge")
	}
	start := b.state.ConsentSince[alice.URI]

	fail.Store(true)
	b.followers = nil // force a refresh against the failing instance
	if b.allowed(t.Context(), alice) {
		t.Fatal("undetermined consent must not anchor")
	}
	if b.state.ConsentSince[alice.URI] != start {
		t.Fatal("a transient error must not clear the consent start")
	}

	alice.Note = "#nobridge"
	if b.allowed(t.Context(), alice) {
		t.Fatal("#nobridge must refuse")
	}
	if _, ok := b.state.ConsentSince[alice.URI]; ok {
		t.Fatal("a real withdrawal must clear the consent start")
	}
}

// Re-consent after a withdrawal must not reach back over the opted-out
// gap: the grace backdating stops at the last observed refusal.
func TestReconsentDoesNotCoverTheOptedOutGap(t *testing.T) {
	b, sub := testBridge(t, nil)
	b.cfg.Consent = ConsentIndexable
	acct := &Account{URI: "https://md.test/users/a", Indexable: boolPtr(true), Note: "#nobridge"}
	if b.allowed(t.Context(), acct) {
		t.Fatal("setup: #nobridge refuses")
	}
	during := "https://md.test/users/a/statuses/1"
	b.fetcher = mapFetcher{during: noteAt(during, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339))}

	acct.Note = "" // opted back in, within the grace window of that post
	if err := b.IngestURI(t.Context(), during, acct); err != nil {
		t.Fatal(err)
	}
	if len(sub.msgs) != 0 {
		t.Fatal("a post made while opted out was anchored after re-consent")
	}
}
