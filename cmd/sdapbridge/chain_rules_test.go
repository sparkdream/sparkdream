package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sparkdream/internal/sdaptx"
	"sparkdream/tools/apcanon"
	"sparkdream/x/federation/types"
)

const selfOperator = "sprkdrm1self"

func hashOf(t *testing.T, raw string) []byte {
	t.Helper()
	obj, err := apcanon.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	h, err := apcanon.Hash(obj)
	if err != nil {
		t.Fatal(err)
	}
	return h[:]
}

// A post from another instance the bridge account follows is not this
// peer's content: skip it before fetching, so it is neither anchored under
// the wrong peer nor submitted just to be rejected.
func TestIngestSkipsForeignHost(t *testing.T) {
	b, sub := testBridge(t, map[string]string{"/users/alice/statuses/10": publicNote})
	fetch := &countingFetcher{AS2Source: b.fetcher}
	b.fetcher = fetch

	if err := b.IngestURI(t.Context(), "https://aurora.test/users/zed/statuses/1", nil); err != nil {
		t.Fatal(err)
	}
	if fetch.n != 0 || len(sub.msgs) != 0 {
		t.Fatalf("foreign post: %d fetches, %d submissions; want none", fetch.n, len(sub.msgs))
	}

	if err := b.IngestURI(t.Context(), noteURI, nil); err != nil {
		t.Fatal(err)
	}
	if len(sub.msgs) != 1 {
		t.Fatalf("the peer's own post must still be anchored: %d submissions", len(sub.msgs))
	}
}

// An edit of a version this bridge anchored carries the on-chain link.
func TestEditOfOwnRecordSetsSupersedes(t *testing.T) {
	objects := map[string]string{"/users/alice/statuses/10": publicNote}
	b, sub := testBridge(t, objects)
	b.operator = selfOperator

	if err := b.IngestURI(t.Context(), noteURI, nil); err != nil {
		t.Fatal(err)
	}
	objects["/users/alice/statuses/10"] = editedNote
	if err := b.IngestURI(t.Context(), noteURI, nil); err != nil {
		t.Fatal(err)
	}
	if len(sub.msgs) != 2 || sub.msgs[1].Supersedes == nil || sub.msgs[1].Supersedes.ContentId != 1 {
		t.Fatalf("edit of our own record must supersede content 1: %+v", sub.msgs[len(sub.msgs)-1].Supersedes)
	}
}

// The chain only lets an operator supersede its own records, so an edit of
// a version another bridge anchored gets the metadata link only.
func TestEditOfAnotherOperatorsRecordHasNoChainLink(t *testing.T) {
	b, sub := testBridge(t, map[string]string{"/users/alice/statuses/10": editedNote})
	b.operator = selfOperator
	b.state.Record(noteURI, hashOf(t, publicNote), 7, "sprkdrm1other", "", time.Now().Unix())

	if err := b.IngestURI(t.Context(), noteURI, nil); err != nil {
		t.Fatal(err)
	}
	if len(sub.msgs) != 1 || sub.msgs[0].Supersedes != nil {
		t.Fatalf("another operator's record must not be named in supersedes: %+v", sub.msgs)
	}
	var meta map[string]any
	_ = json.Unmarshal(sub.msgs[0].ProtocolMetadata, &meta)
	if meta["supersedes_content_id"] != "7" {
		t.Fatalf("metadata link = %v, want 7", meta["supersedes_content_id"])
	}
}

// A refused link (a concurrent edit got there first) must not cost the new
// version its anchor.
func TestSupersedeRejectedResubmitsWithoutLink(t *testing.T) {
	objects := map[string]string{"/users/alice/statuses/10": publicNote}
	b, sub := testBridge(t, objects)
	b.operator = selfOperator
	if err := b.IngestURI(t.Context(), noteURI, nil); err != nil {
		t.Fatal(err)
	}

	sub.hook = func(msg *types.MsgSubmitFederatedContent) error {
		if msg.Supersedes != nil {
			return fmt.Errorf("%w: already superseded", ErrSupersedeRejected)
		}
		return nil
	}
	objects["/users/alice/statuses/10"] = editedNote
	if err := b.IngestURI(t.Context(), noteURI, nil); err != nil {
		t.Fatal(err)
	}
	if len(sub.msgs) != 2 || sub.msgs[1].Supersedes != nil {
		t.Fatalf("want the edit anchored without the link, got %d submissions", len(sub.msgs))
	}
}

// A post the policy refuses outright must not pin the timeline cursor: the
// next post still gets anchored and the cursor moves past both.
func TestPermanentRejectionDoesNotBlockTheCursor(t *testing.T) {
	objects := map[string]string{
		"/users/alice/statuses/10": publicNote,
		"/users/bob/statuses/12":   replyNote,
	}
	b, sub := testBridge(t, objects)
	sub.hook = func(msg *types.MsgSubmitFederatedContent) error {
		if msg.ContentType == "blog_post" {
			return fmt.Errorf("%w (code 2310): content type not in peer policy", ErrPermanentRejection)
		}
		return nil
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[
			{"id":"12","uri":"https://md.test/users/bob/statuses/12","account":{"acct":"bob","uri":"https://md.test/users/bob"}},
			{"id":"10","uri":"https://md.test/users/alice/statuses/10","account":{"acct":"alice","uri":"https://md.test/users/alice"}}
		]`))
	}))
	defer srv.Close()
	b.masto = NewMastodonClient(srv.URL, "token")

	if err := b.pollOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(sub.msgs) != 1 || sub.msgs[0].ContentType != "blog_reply" {
		t.Fatalf("the reply after the refused post must be anchored: %d submissions", len(sub.msgs))
	}
	if got := b.state.LastSeenStatusID(); got != "12" {
		t.Fatalf("cursor = %q, want 12 (past the refused post)", got)
	}
}

func TestClassifyDeliverTx(t *testing.T) {
	cases := []struct {
		code uint32
		want error
	}{
		{types.ErrDuplicateContent.ABCICode(), ErrDuplicateContent},
		{types.ErrInvalidSupersede.ABCICode(), ErrSupersedeRejected},
		{types.ErrContentTypeNotAllowed.ABCICode(), ErrPermanentRejection},
		{types.ErrIdentityBlocked.ABCICode(), ErrPermanentRejection},
		{types.ErrContentHostMismatch.ABCICode(), ErrPermanentRejection},
		{types.ErrCreatorHostMismatch.ABCICode(), ErrPermanentRejection},
		{types.ErrPeerNotActive.ABCICode(), ErrPermanentRejection},
		{types.ErrBridgeNotFound.ABCICode(), ErrPermanentRejection},
		{types.ErrRateLimitExceeded.ABCICode(), ErrPeerThrottled},
	}
	for _, c := range cases {
		if err := classifyDeliverTx(sdaptx.TxResult{Code: c.code}); !errors.Is(err, c.want) {
			t.Errorf("code %d: got %v, want %v", c.code, err, c.want)
		}
	}
	// Anything else (the per-peer rate limit, a suspended bridge) stays
	// retryable: none of the sentinels.
	err := classifyDeliverTx(sdaptx.TxResult{Code: types.ErrBridgeNotActive.ABCICode()})
	if errors.Is(err, ErrPermanentRejection) || errors.Is(err, ErrDuplicateContent) || errors.Is(err, ErrSupersedeRejected) {
		t.Fatalf("a suspended bridge must stay retryable, got %v", err)
	}
}

// reconcileInstance serves verify_credentials, a two-page follow list, and
// per-account statuses. Status 10 (alice) never reached the home timeline.
func reconcileInstance(t *testing.T, statuses map[string]string) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/accounts/verify_credentials":
			_, _ = w.Write([]byte(`{"id":"1","acct":"sdapbridge"}`))
		case r.URL.Path == "/api/v1/accounts/1/following" && r.URL.Query().Get("max_id") == "":
			w.Header().Set("Link", `<`+srv.URL+`/api/v1/accounts/1/following?limit=80&max_id=5>; rel="next"`)
			_, _ = w.Write([]byte(`[{"id":"2","acct":"alice","uri":"https://md.test/users/alice"}]`))
		case r.URL.Path == "/api/v1/accounts/1/following":
			_, _ = w.Write([]byte(`[{"id":"3","acct":"zed@aurora.test","uri":"https://aurora.test/users/zed"}]`))
		case strings.HasPrefix(r.URL.Path, "/api/v1/accounts/") && strings.HasSuffix(r.URL.Path, "/statuses"):
			if r.URL.Query().Get("min_id") == "" || r.URL.Query().Get("exclude_reblogs") != "true" {
				t.Errorf("statuses query = %q", r.URL.RawQuery)
			}
			id := strings.Split(r.URL.Path, "/")[4]
			if body, ok := statuses[id]; ok && r.URL.Query().Get("min_id") != "10" {
				_, _ = w.Write([]byte(body))
				return
			}
			_, _ = w.Write([]byte(`[]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// P5 safety net: posts that never reached a starved home timeline are
// found by reading followed accounts directly, and nothing else is: not
// boosts, not followers-only posts, not posts too young to have missed a
// poll, not other instances' posts.
func TestReconcileAnchorsWhatTheTimelineMissed(t *testing.T) {
	b, sub := testBridge(t, map[string]string{"/users/alice/statuses/10": publicNote})
	b.cfg.PollInterval = 30 * time.Second
	b.cfg.ReconcileLookback = 24 * time.Hour
	b.startedAt = time.Now().Add(-48 * time.Hour)
	old := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	young := time.Now().UTC().Format(time.RFC3339)
	acct := `"account":{"acct":"alice","uri":"https://md.test/users/alice"}`
	srv := reconcileInstance(t, map[string]string{
		// newest-first, as Mastodon returns it
		"2": `[
			{"id":"14","uri":"https://md.test/users/alice/statuses/14","visibility":"public","created_at":"` + young + `",` + acct + `},
			{"id":"13","uri":"https://md.test/users/alice/statuses/13","visibility":"private","created_at":"` + old + `",` + acct + `},
			{"id":"11","uri":"https://md.test/users/alice/statuses/11","visibility":"public","created_at":"` + old + `","reblog":{"id":"9"},` + acct + `},
			{"id":"10","uri":"https://md.test/users/alice/statuses/10","visibility":"public","created_at":"` + old + `",` + acct + `}
		]`,
		"3": `[{"id":"20","uri":"https://aurora.test/users/zed/statuses/20","visibility":"public","created_at":"` + old + `","account":{"acct":"zed@aurora.test"}}]`,
	})
	b.masto = NewMastodonClient(srv.URL, "token")

	if err := b.reconcileOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(sub.msgs) != 1 || sub.msgs[0].ContentUri != noteURI {
		t.Fatalf("want only status 10 anchored, got %d submissions", len(sub.msgs))
	}
	if b.selfID != "1" {
		t.Fatalf("own account id not cached: %q", b.selfID)
	}

	// Already anchored: a second sweep submits nothing.
	if err := b.reconcileOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(sub.msgs) != 1 {
		t.Fatalf("a second sweep re-anchored: %d submissions", len(sub.msgs))
	}
}

// Mastodon ids are (unix ms << 16) | sequence. Checked against a real id
// from the v4.7.2 test instance, created 2026-09-23T18:16:21Z.
func TestSnowflakeAt(t *testing.T) {
	const realID = 117321720215797647
	created := time.Date(2026, 9, 23, 18, 16, 21, 0, time.UTC)
	lo, _ := parseUint(snowflakeAt(created))
	hi, _ := parseUint(snowflakeAt(created.Add(time.Second)))
	if !(lo <= realID && realID < hi) {
		t.Fatalf("snowflakeAt brackets [%d, %d), want it to contain %d", lo, hi, uint64(realID))
	}
}

func parseUint(s string) (uint64, error) {
	var v uint64
	_, err := fmt.Sscanf(s, "%d", &v)
	return v, err
}

// A Link header pointing off-instance is never followed: the request would
// carry the app token.
func TestNextLinkStaysOnInstance(t *testing.T) {
	base := "https://md.test"
	if got := nextLink(`<https://md.test/api/v1/accounts/1/following?max_id=5>; rel="next", <https://md.test/x>; rel="prev"`, base); got != "/api/v1/accounts/1/following?max_id=5" {
		t.Fatalf("on-instance next = %q", got)
	}
	if got := nextLink(`<https://evil.test/steal>; rel="next"`, base); got != "" {
		t.Fatalf("off-instance next must be ignored, got %q", got)
	}
	if got := nextLink(`<https://md.test.evil.test/steal>; rel="next"`, base); got != "" {
		t.Fatalf("look-alike host must be ignored, got %q", got)
	}
}

// An attribution the chain would refuse is skipped before submitting.
func TestForeignCreatorIsSkippedBeforeSubmit(t *testing.T) {
	b, sub := testBridge(t, map[string]string{"/users/alice/statuses/10": publicNote})
	acct := &Account{Acct: "alice@aurora.test", URI: "https://aurora.test/users/alice"}
	if err := b.IngestURI(t.Context(), noteURI, acct); err != nil {
		t.Fatal(err)
	}
	if len(sub.msgs) != 0 {
		t.Fatal("a post attributed to another instance's handle must not be submitted")
	}
	b.peers.hosts["md.test"] = []string{"aurora.test"} // the policy lists it
	if err := b.IngestURI(t.Context(), noteURI, acct); err != nil {
		t.Fatal(err)
	}
	if len(sub.msgs) != 1 {
		t.Fatal("a listed content host must be accepted")
	}
}
