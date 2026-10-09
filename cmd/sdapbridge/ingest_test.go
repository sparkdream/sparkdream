package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sparkdream/internal/sdaptx"
	"sparkdream/tools/apcanon"
	"sparkdream/x/federation/types"
)

// fakeSubmitter records every MsgSubmitFederatedContent the pipeline
// produces — the assertion surface for the P5 acceptance criteria.
type fakeSubmitter struct {
	msgs []*types.MsgSubmitFederatedContent
	err  error
	// hook, when set, can refuse an individual submission (the chain's
	// per-message verdicts); a nil return lets it through.
	hook func(msg *types.MsgSubmitFederatedContent) error
}

func (f *fakeSubmitter) Submit(_ context.Context, msg *types.MsgSubmitFederatedContent) (uint64, string, error) {
	if f.err != nil {
		return 0, "", f.err
	}
	if f.hook != nil {
		if err := f.hook(msg); err != nil {
			return 0, "", err
		}
	}
	f.msgs = append(f.msgs, msg)
	return uint64(len(f.msgs)), "TXHASH", nil
}

// as2Server serves AS2 objects by path, so the same object can be
// edited between ingest calls (determinism-harness behavior in
// miniature).
func as2Server(t *testing.T, objects map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); !strings.Contains(got, "application/activity+json") {
			t.Errorf("AS2 fetch Accept = %q", got)
		}
		body, ok := objects[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/activity+json")
		_, _ = w.Write([]byte(body))
	}))
}

const publicNote = `{
  "@context":"https://www.w3.org/ns/activitystreams",
  "id":"https://md.test/users/alice/statuses/10",
  "type":"Note",
  "attributedTo":"https://md.test/users/alice",
  "content":"<p>hello federation #cc0</p>",
  "published":"2026-09-10T10:00:00Z",
  "to":["https://www.w3.org/ns/activitystreams#Public"]
}`

const editedNote = `{
  "@context":"https://www.w3.org/ns/activitystreams",
  "id":"https://md.test/users/alice/statuses/10",
  "type":"Note",
  "attributedTo":"https://md.test/users/alice",
  "content":"<p>hello federation (edited) #cc0</p>",
  "published":"2026-09-10T10:00:00Z",
  "updated":"2026-09-10T11:00:00Z",
  "to":["https://www.w3.org/ns/activitystreams#Public"]
}`

const followersOnly = `{
  "id":"https://md.test/users/alice/statuses/11",
  "type":"Note",
  "attributedTo":"https://md.test/users/alice",
  "content":"<p>followers only #cc0</p>",
  "published":"2026-09-10T10:05:00Z",
  "to":["https://md.test/users/alice/followers"]
}`

const replyNote = `{
  "id":"https://md.test/users/bob/statuses/12",
  "type":"Note",
  "attributedTo":"https://md.test/users/bob",
  "content":"<p>a reply #cc0</p>",
  "published":"2026-09-10T10:10:00Z",
  "inReplyTo":"https://md.test/users/alice/statuses/10",
  "to":["https://www.w3.org/ns/activitystreams#Public"]
}`

// rewritingFetcher maps the instance's absolute URIs onto the test
// server, the way a network path would in production. The OBJECTS keep
// their md.test ids — the hash must be over what the instance published.
type rewritingFetcher struct {
	serverURL string
}

func (f *rewritingFetcher) Fetch(ctx context.Context, uri string) (map[string]any, error) {
	mapped := strings.Replace(uri, "https://md.test", f.serverURL, 1)
	// httptest binds loopback, which apcanon's destination guard refuses
	// by design; the guard itself is covered in tools/apcanon.
	obj, _, err := apcanon.Fetch(ctx, mapped, apcanon.FetchOptions{AllowPrivateHosts: true})
	return obj, err
}

func testBridge(t *testing.T, objects map[string]string) (*Bridge, *fakeSubmitter) {
	t.Helper()
	srv := as2Server(t, objects)
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	state, err := LoadState(dir + "/state.json")
	if err != nil {
		t.Fatal(err)
	}
	sub := &fakeSubmitter{}
	return &Bridge{
		cfg: Config{
			PeerIDs:     []string{"md.test"},
			BodyLimit:   4096,
			Backfill:    0,
			MastodonURL: "https://md.test",
			// Consent has its own tests (consent_test.go); the pipeline
			// tests here are about everything after it.
			Consent: ConsentNone,
		},
		peers:   newPeerSet([]string{"md.test"}, nil),
		fetcher: &rewritingFetcher{serverURL: srv.URL},
		chain:   sub,
		state:   state,
	}, sub
}

// P5 acceptance: a new post is anchored with the §6.1 field mapping.
func TestIngestAnchorsNewPost(t *testing.T) {
	b, sub := testBridge(t, map[string]string{"/users/alice/statuses/10": publicNote})

	acct := &Account{Acct: "alice", DisplayName: "Alice", URI: "https://md.test/users/alice"}
	if err := b.IngestURI(t.Context(), "https://md.test/users/alice/statuses/10", acct); err != nil {
		t.Fatal(err)
	}
	if len(sub.msgs) != 1 {
		t.Fatalf("want 1 submission, got %d", len(sub.msgs))
	}
	m := sub.msgs[0]
	if m.PeerId != "md.test" || m.RemoteContentId != "10" || m.ContentType != "blog_post" {
		t.Fatalf("mapping wrong: peer=%q id=%q type=%q", m.PeerId, m.RemoteContentId, m.ContentType)
	}
	if m.CreatorIdentity != "@alice@md.test" || m.CreatorName != "Alice" {
		t.Fatalf("creator mapping wrong: %q / %q", m.CreatorIdentity, m.CreatorName)
	}
	if m.Body != "<p>hello federation #cc0</p>" || m.Title != "" {
		t.Fatalf("body/title wrong: %q / %q", m.Body, m.Title)
	}
	if m.License != "CC0-1.0" {
		t.Fatalf("license = %q, want CC0-1.0 from the #cc0 hashtag", m.License)
	}
	if m.ContentUri != "https://md.test/users/alice/statuses/10" {
		t.Fatalf("content_uri wrong: %q", m.ContentUri)
	}
	if m.RemoteCreatedAt != time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC).Unix() {
		t.Fatalf("remote_created_at wrong: %d", m.RemoteCreatedAt)
	}
	var meta map[string]any
	if err := json.Unmarshal(m.ProtocolMetadata, &meta); err != nil {
		t.Fatal(err)
	}
	if meta["hash_rule"] != "ap-canonical-v1" {
		t.Fatalf("hash_rule missing from protocol_metadata: %v", meta)
	}
	if _, ok := meta["supersedes_content_id"]; ok {
		t.Fatal("first anchor must not claim to supersede anything")
	}
	// The submitted hash must equal apcanon's own computation.
	obj := mustParseAS2(t, publicNote)
	want := mustHash(t, obj)
	if base64.StdEncoding.EncodeToString(want) != base64.StdEncoding.EncodeToString(m.ContentHash) {
		t.Fatal("submitted hash does not match ap-canonical-v1")
	}

	// Dedupe: the same (uri, hash) again produces nothing.
	if err := b.IngestURI(t.Context(), "https://md.test/users/alice/statuses/10", acct); err != nil {
		t.Fatal(err)
	}
	if len(sub.msgs) != 1 {
		t.Fatalf("duplicate anchor: %d submissions", len(sub.msgs))
	}
}

// P5 acceptance: an edit keeps the uri, changes the hash, and the new
// record carries supersedes_content_id.
func TestIngestEditReanchorsWithSupersedes(t *testing.T) {
	objects := map[string]string{"/users/alice/statuses/10": publicNote}
	b, sub := testBridge(t, objects)
	acct := &Account{Acct: "alice", DisplayName: "Alice"}

	if err := b.IngestURI(t.Context(), "https://md.test/users/alice/statuses/10", acct); err != nil {
		t.Fatal(err)
	}
	// Record a known content id for the first anchor, as the warm-up would.
	for _, rec := range b.state.Seen {
		rec.ContentID = 42
	}

	objects["/users/alice/statuses/10"] = editedNote
	if err := b.IngestURI(t.Context(), "https://md.test/users/alice/statuses/10", acct); err != nil {
		t.Fatal(err)
	}
	if len(sub.msgs) != 2 {
		t.Fatalf("edit must re-anchor: %d submissions", len(sub.msgs))
	}
	var meta map[string]any
	if err := json.Unmarshal(sub.msgs[1].ProtocolMetadata, &meta); err != nil {
		t.Fatal(err)
	}
	if meta["supersedes_content_id"] != "42" {
		t.Fatalf("supersedes_content_id = %v, want 42", meta["supersedes_content_id"])
	}
	// The edit's hash differs from the original's.
	if string(sub.msgs[0].ContentHash) == string(sub.msgs[1].ContentHash) {
		t.Fatal("edit did not change the hash")
	}
}

// P5 acceptance: a followers-only post produces nothing. Visibility is
// asserted from the AS2 audience even though a REST layer might have
// claimed otherwise.
func TestIngestSkipsFollowersOnly(t *testing.T) {
	b, sub := testBridge(t, map[string]string{"/users/alice/statuses/11": followersOnly})
	if err := b.IngestURI(t.Context(), "https://md.test/users/alice/statuses/11",
		&Account{Acct: "alice"}); err != nil {
		t.Fatal(err)
	}
	if len(sub.msgs) != 0 {
		t.Fatalf("followers-only post must not be anchored, got %d", len(sub.msgs))
	}
}

// P5 acceptance: a boost produces nothing. The filter lives in the
// discovery loop (pollOnce); this drives it through a fake timeline.
func TestPollSkipsBoosts(t *testing.T) {
	objects := map[string]string{"/users/alice/statuses/10": publicNote}
	b, sub := testBridge(t, objects)
	b.cfg.Backfill = 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/timelines/home") {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte(`[
			{"id":"99","uri":"https://md.test/users/carol/statuses/99","reblog":{"id":"10","uri":"https://md.test/users/alice/statuses/10"},
			 "account":{"acct":"carol","display_name":"Carol","uri":"https://md.test/users/carol"},
			 "content":"","created_at":"2026-09-10T12:00:00Z"},
			{"id":"10","uri":"https://md.test/users/alice/statuses/10","url":"https://md.test/@alice/10",
			 "account":{"acct":"alice","display_name":"Alice","uri":"https://md.test/users/alice"},
			 "content":"<p>hello federation</p>","created_at":"2026-09-10T10:00:00Z"}
		]`))
	}))
	defer srv.Close()
	b.masto = NewMastodonClient(srv.URL, "token")

	if err := b.pollOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(sub.msgs) != 1 {
		t.Fatalf("boost must be skipped: %d submissions", len(sub.msgs))
	}
	if sub.msgs[0].RemoteContentId == "99" {
		t.Fatal("the boost wrapper was anchored instead of the original")
	}
}

// Replies map to blog_reply.
func TestIngestReplyType(t *testing.T) {
	b, sub := testBridge(t, map[string]string{"/users/bob/statuses/12": replyNote})
	if err := b.IngestURI(t.Context(), "https://md.test/users/bob/statuses/12",
		&Account{Acct: "bob@md.test", DisplayName: "Bob"}); err != nil {
		t.Fatal(err)
	}
	if len(sub.msgs) != 1 || sub.msgs[0].ContentType != "blog_reply" {
		t.Fatalf("reply must map to blog_reply: %+v", sub.msgs)
	}
}

func mustParseAS2(t *testing.T, raw string) map[string]any {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		t.Fatal(err)
	}
	return obj
}

func mustHash(t *testing.T, obj map[string]any) []byte {
	t.Helper()
	h, err := hashForTest(obj)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func hashForTest(obj map[string]any) ([]byte, error) {
	h, err := apcanon.Hash(obj)
	if err != nil {
		return nil, err
	}
	return h[:], nil
}

// The warm-up guarantee: a wiped state file must not re-anchor history.
// This is the round trip that would have caught all three bugs at once —
// the quoted-uint64 decode, the base64 hash, and the cache being keyed by
// a different URI than ingestion looks up.
func TestWarmCacheFromChainPreventsReAnchor(t *testing.T) {
	objects := map[string]string{"/users/alice/statuses/10": publicNote}
	b, sub := testBridge(t, objects)

	const uri = "https://md.test/users/alice/statuses/10"
	acct := &Account{Acct: "alice", DisplayName: "Alice", URI: "https://md.test/users/alice"}
	if err := b.IngestURI(t.Context(), uri, acct); err != nil {
		t.Fatal(err)
	}
	if len(sub.msgs) != 1 {
		t.Fatalf("setup: want 1 submission, got %d", len(sub.msgs))
	}
	anchored := sub.msgs[0]

	// The chain answers in ITS OWN encoding: uint64 quoted, bytes base64.
	lcd := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "list_federated_content") {
			w.WriteHeader(404)
			return
		}
		if got := r.URL.Query().Get("peer_id"); got != "md.test" {
			t.Errorf("peer_id filter = %q", got)
		}
		resp := map[string]any{
			"content": []map[string]any{{
				"id":                "7",
				"content_uri":       anchored.ContentUri,
				"remote_content_id": anchored.RemoteContentId,
				"content_hash":      base64.StdEncoding.EncodeToString(anchored.ContentHash),
				"received_at":       "1757671300",
				"protocol_metadata": json.RawMessage(anchored.ProtocolMetadata),
			}},
			"pagination": map[string]any{"total": "1"},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer lcd.Close()

	// Wipe local state the way a lost volume would.
	fresh, err := LoadState(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatal(err)
	}
	chain, err := sdaptx.New(sdaptx.Config{
		LCD: lcd.URL, ChainID: "sparkdream-dev-1", Bech32Prefix: "sprkdrm",
		Denom: "usparz.sparkdreamdev",
		Mnemonic: "abandon abandon abandon abandon abandon abandon " +
			"abandon abandon abandon abandon abandon about",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := WarmCacheFromChain(t.Context(), chain, "md.test", fresh); err != nil {
		t.Fatalf("warm cache: %v", err)
	}

	// The warmed cache must be keyed by the SAME uri ingestion looks up.
	if !fresh.Has(uri, anchored.ContentHash) {
		t.Fatal("warmed cache does not know the anchored (uri, hash) — the key is wrong")
	}
	rec, ok := fresh.LatestFor(uri)
	if !ok || rec.ContentID != 7 {
		t.Fatalf("warm-up must recover the content id for supersedes chaining, got %+v", rec)
	}

	// And re-running ingestion against it must submit nothing.
	b.state = fresh
	sub.msgs = nil
	if err := b.IngestURI(t.Context(), uri, acct); err != nil {
		t.Fatal(err)
	}
	if len(sub.msgs) != 0 {
		t.Fatalf("a warmed cache must suppress re-anchoring, got %d submissions", len(sub.msgs))
	}
}

// content_uri must be the AS2 id, not the web permalink: the verifier
// re-fetches content_uri and has to reach the representation that was
// hashed, and the warm cache keys on it.
func TestAnchorsAS2IdNotWebPermalink(t *testing.T) {
	const withURL = `{
	  "id":"https://md.test/users/alice/statuses/10",
	  "type":"Note",
	  "attributedTo":"https://md.test/users/alice",
	  "url":"https://md.test/@alice/10",
	  "content":"<p>hello #cc0</p>",
	  "published":"2026-09-10T10:00:00Z",
	  "to":["https://www.w3.org/ns/activitystreams#Public"]
	}`
	b, sub := testBridge(t, map[string]string{"/users/alice/statuses/10": withURL})
	acct := &Account{Acct: "alice", DisplayName: "Alice", URI: "https://md.test/users/alice"}
	if err := b.IngestURI(t.Context(), "https://md.test/users/alice/statuses/10", acct); err != nil {
		t.Fatal(err)
	}
	m := sub.msgs[0]
	if m.ContentUri != "https://md.test/users/alice/statuses/10" {
		t.Fatalf("content_uri must be the AS2 id, got %q", m.ContentUri)
	}
	var meta map[string]any
	if err := json.Unmarshal(m.ProtocolMetadata, &meta); err != nil {
		t.Fatal(err)
	}
	if meta["content_url"] != "https://md.test/@alice/10" {
		t.Fatalf("the permalink belongs in protocol_metadata.content_url, got %v", meta["content_url"])
	}
}

// A failed submission must stay retryable: recording it would mark
// rejected content as anchored forever. This is the hazard SYNC-mode
// broadcast created by reporting CheckTx as success.
func TestFailedSubmitIsNotRecorded(t *testing.T) {
	b, sub := testBridge(t, map[string]string{"/users/alice/statuses/10": publicNote})
	sub.err = errors.New("tx failed in DeliverTx with code 2358: inbound rate limit exceeded")

	const uri = "https://md.test/users/alice/statuses/10"
	acct := &Account{Acct: "alice", URI: "https://md.test/users/alice"}
	if err := b.IngestURI(t.Context(), uri, acct); err == nil {
		t.Fatal("a failed submission must surface as an error")
	}

	sub.err = nil
	if err := b.IngestURI(t.Context(), uri, acct); err != nil {
		t.Fatal(err)
	}
	if len(sub.msgs) != 1 {
		t.Fatalf("the retry must go through, got %d submissions", len(sub.msgs))
	}
}

// A duplicate-content rejection is benign — the chain already holds it —
// so it caches rather than retrying forever.
func TestDuplicateContentIsCachedNotRetried(t *testing.T) {
	b, sub := testBridge(t, map[string]string{"/users/alice/statuses/10": publicNote})
	sub.err = ErrDuplicateContent

	const uri = "https://md.test/users/alice/statuses/10"
	acct := &Account{Acct: "alice", URI: "https://md.test/users/alice"}
	if err := b.IngestURI(t.Context(), uri, acct); err != nil {
		t.Fatalf("a duplicate must not surface as an error: %v", err)
	}

	sub.err = nil
	if err := b.IngestURI(t.Context(), uri, acct); err != nil {
		t.Fatal(err)
	}
	if len(sub.msgs) != 0 {
		t.Fatal("a cached duplicate must not be resubmitted")
	}
}

// Backfill: the outbox lives on ?page=true, the actor URI is absolute so
// it must not be pasted onto the local instance base, and the bridge's
// OAuth token must never leave the local instance.
func TestBackfillFetchesOutboxPageWithoutLeakingToken(t *testing.T) {
	var sawAuth bool
	var sawPage bool
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			sawAuth = true
		}
		if r.URL.Path != "/users/alice/outbox" {
			w.WriteHeader(404)
			return
		}
		if r.URL.Query().Get("page") == "true" {
			sawPage = true
			_, _ = w.Write([]byte(`{"type":"OrderedCollectionPage","orderedItems":[
				{"type":"Create","object":{"id":"https://md.test/users/alice/statuses/10"}},
				{"type":"Announce","object":"https://elsewhere.test/users/bob/statuses/1"}
			]}`))
			return
		}
		// Collection root: no inline items, just a `first` link.
		_, _ = w.Write([]byte(`{"type":"OrderedCollection","totalItems":2,"first":"` +
			r.URL.Scheme + `"}`))
	}))
	defer remote.Close()

	m := NewMastodonClient("https://local.instance", "SECRET-TOKEN")
	uris, err := m.Outbox(context.Background(), remote.URL+"/users/alice")
	if err != nil {
		t.Fatalf("Outbox: %v", err)
	}
	if !sawPage {
		t.Fatal("the outbox must be requested with ?page=true; the root carries no items")
	}
	if sawAuth {
		t.Fatal("the local OAuth token must never be sent to a remote instance")
	}
	if len(uris) != 1 || uris[0] != "https://md.test/users/alice/statuses/10" {
		t.Fatalf("want only the Create object id, got %v", uris)
	}
}

// A failed outbox pass must be retried on the next sighting, not
// permanently written off.
func TestBackfillRetriesAfterFailure(t *testing.T) {
	b, _ := testBridge(t, map[string]string{})
	b.cfg.Backfill = 5
	b.masto = NewMastodonClient("https://local.instance", "token")

	acct := &Account{Acct: "alice", URI: "https://unreachable.invalid/users/alice"}
	b.backfill(t.Context(), acct)

	if b.state.Backfilled(acct.URI) {
		t.Fatal("a failed outbox pass must not mark the account done")
	}
}
