package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"sparkdream/tools/apcanon"
	"sparkdream/x/federation/types"
)

const as2Note = `{
  "@context":"https://www.w3.org/ns/activitystreams",
  "id":"https://md.test/users/alice/statuses/50",
  "type":"Note",
  "attributedTo":"https://md.test/users/alice",
  "content":"<p>verify me</p>",
  "published":"2026-09-12T10:00:00Z",
  "to":["https://www.w3.org/ns/activitystreams#Public"]
}`

// recordingVerifier wires a Verifier whose alarms are captured, so the
// mismatch path can be asserted positively rather than only by the
// absence of a submission.
type harness struct {
	v         *Verifier
	submitted []types.MsgVerifyContent
	alarms    []string
	fetchErrs int
	mu        sync.Mutex
}

func newHarness(t *testing.T, anchoredHashB64 string) *harness {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept"), "application/activity+json") {
			t.Errorf("Accept = %q", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "application/activity+json")
		_, _ = w.Write([]byte(as2Note))
	}))
	t.Cleanup(srv.Close)

	h := &harness{}
	h.v = &Verifier{
		cfg:           Config{PeerIDs: []string{"md.test"}, AlarmWebhook: ""},
		self:          "sprkdrm1verifier",
		alarmed:       map[string]bool{},
		fetchFailures: map[string]int{},
		fetch: func(ctx context.Context, uri string) (map[string]any, error) {
			h.mu.Lock()
			fail := h.fetchErrs > 0
			if fail {
				h.fetchErrs--
			}
			h.mu.Unlock()
			if fail {
				return nil, context.DeadlineExceeded
			}
			// httptest binds loopback, which apcanon's destination guard
			// refuses by design; the guard is covered in tools/apcanon.
			obj, _, err := apcanon.Fetch(ctx,
				strings.Replace(uri, "https://md.test", srv.URL, 1),
				apcanon.FetchOptions{AllowPrivateHosts: true})
			return obj, err
		},
		broadcast: func(ctx context.Context, msg *types.MsgVerifyContent) (string, error) {
			msg.Creator = "sprkdrm1verifier"
			h.submitted = append(h.submitted, *msg)
			return "TXHASH", nil
		},
	}
	h.v.alarmSink = func(msg string) { h.alarms = append(h.alarms, msg) }
	h.v.list = func(ctx context.Context, peerID string) ([]PendingContent, error) {
		return []PendingContent{{
			ID:             "7",
			ContentURI:     "https://md.test/users/alice/statuses/50",
			ContentHashB64: anchoredHashB64,
			SubmittedBy:    "sprkdrm1operator",
			PeerID:         peerID,
			// The displayed fields, faithful to as2Note (display.go).
			Body:            "<p>verify me</p>",
			ContentType:     "blog_post",
			RemoteCreatedAt: "1789207200", // 2026-09-12T10:00:00Z
		}}, nil
	}
	return h
}

// anchoredHashOf returns the hash the chain would hold, in the chain's
// own wire encoding: proto bytes over the LCD is BASE64.
func anchoredHashOf(t *testing.T) string {
	t.Helper()
	obj, err := apcanon.Parse([]byte(as2Note))
	if err != nil {
		t.Fatal(err)
	}
	h, err := apcanon.Hash(obj)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(h[:])
}

// P6 acceptance: unattended VERIFIED records end to end — a matching
// re-fetch submits MsgVerifyContent with the anchored hash.
func TestVerifyMatchingSubmits(t *testing.T) {
	h := newHarness(t, anchoredHashOf(t))
	if err := h.v.poll(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(h.submitted) != 1 {
		t.Fatalf("want 1 MsgVerifyContent, got %d (alarms: %v)", len(h.submitted), h.alarms)
	}
	m := h.submitted[0]
	if m.ContentId != 7 {
		t.Fatalf("content id = %d, want 7 (the quoted \"7\" from the LCD)", m.ContentId)
	}
	want, _ := base64.StdEncoding.DecodeString(anchoredHashOf(t))
	if string(m.ContentHash) != string(want) {
		t.Fatal("submitted hash must equal the anchored hash on a match")
	}
}

// The exact response body the chain's marshaler produces must decode.
// This is the fixture that would have caught both wire-encoding bugs:
// uint64 comes back as a QUOTED string and bytes as BASE64, and the
// previous struct declared `ID uint64` + hex, so the whole response was
// rejected and the runner silently verified nothing.
func TestPendingContentDecodesRealLCDShape(t *testing.T) {
	const body = `{"content":[{"id":"7","peer_id":"md.test","remote_content_id":"50",` +
		`"content_type":"blog_post","creator_identity":"@alice@md.test","creator_name":"Alice",` +
		`"title":"","body":"<p>verify me</p>","content_uri":"https://md.test/users/alice/statuses/50",` +
		`"protocol_metadata":null,"remote_created_at":"1757671200","received_at":"1757671300",` +
		`"submitted_by":"sprkdrm1operator","status":"FEDERATED_CONTENT_STATUS_PENDING_VERIFICATION",` +
		`"expires_at":"1757757700","content_hash":"3q2+7w=="}],"pagination":{"total":"1"}}`

	var out struct {
		Content []PendingContent `json:"content"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("real LCD response must decode: %v", err)
	}
	if len(out.Content) != 1 {
		t.Fatalf("got %d rows", len(out.Content))
	}
	c := out.Content[0]
	if c.ID != "7" {
		t.Fatalf("id = %q", c.ID)
	}
	if _, err := c.AnchoredHash(); err == nil {
		t.Fatal("a 4-byte hash must be rejected as the wrong length")
	}

	full := sha256.Sum256([]byte("x"))
	c.ContentHashB64 = base64.StdEncoding.EncodeToString(full[:])
	got, err := c.AnchoredHash()
	if err != nil {
		t.Fatalf("AnchoredHash: %v", err)
	}
	if string(got) != string(full[:]) {
		t.Fatal("base64 round-trip wrong")
	}
}

// P6 acceptance: an induced mismatch ALARMS rather than submits — and
// the alarm must actually fire, which the previous test never checked.
func TestMismatchAlarmsNotSubmits(t *testing.T) {
	other := sha256.Sum256([]byte("a totally different object"))
	h := newHarness(t, base64.StdEncoding.EncodeToString(other[:]))
	if err := h.v.poll(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(h.submitted) != 0 {
		t.Fatalf("mismatch must NOT auto-submit (DISPUTED strands the bond during bring-up), got %d", len(h.submitted))
	}
	if len(h.alarms) != 1 {
		t.Fatalf("mismatch must alarm exactly once, got %d", len(h.alarms))
	}
	if !strings.Contains(h.alarms[0], "HASH MISMATCH") {
		t.Fatalf("alarm text = %q", h.alarms[0])
	}
}

// The alarm latches: a mismatch that persists across polls pages once,
// not every PollInterval forever.
func TestMismatchAlarmLatches(t *testing.T) {
	other := sha256.Sum256([]byte("a totally different object"))
	h := newHarness(t, base64.StdEncoding.EncodeToString(other[:]))
	for i := 0; i < 4; i++ {
		if err := h.v.poll(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if len(h.alarms) != 1 {
		t.Fatalf("persistent mismatch must alarm once, got %d", len(h.alarms))
	}
}

// A post that stops resolving is the "anchor then withdraw" fraud shape
// as well as an ordinary outage. Repeated failure must escalate, not
// scroll past in a log forever.
func TestPersistentFetchFailureAlarms(t *testing.T) {
	h := newHarness(t, anchoredHashOf(t))
	h.v.cfg.FetchFailureAlarmAt = 3
	h.fetchErrs = 10

	for i := 0; i < 3; i++ {
		if err := h.v.poll(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if len(h.alarms) != 1 {
		t.Fatalf("want one unfetchable alarm after 3 failures, got %d: %v", len(h.alarms), h.alarms)
	}
	if !strings.Contains(h.alarms[0], "UNFETCHABLE") {
		t.Fatalf("alarm text = %q", h.alarms[0])
	}
	if len(h.submitted) != 0 {
		t.Fatal("an unfetchable post must never be verified")
	}
}

// A transient failure that recovers resets the counter instead of
// creeping toward a false alarm.
func TestTransientFetchFailureDoesNotAlarm(t *testing.T) {
	h := newHarness(t, anchoredHashOf(t))
	h.v.cfg.FetchFailureAlarmAt = 3
	h.fetchErrs = 2

	for i := 0; i < 4; i++ {
		if err := h.v.poll(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if len(h.alarms) != 0 {
		t.Fatalf("a recovered blip must not alarm: %v", h.alarms)
	}
	if len(h.submitted) == 0 {
		t.Fatal("after recovery the match must submit")
	}
}

// The runner refuses content submitted by its own address even before
// the chain's ErrSelfVerification would reject it.
func TestRefusesSelfSubmitted(t *testing.T) {
	h := newHarness(t, anchoredHashOf(t))
	h.v.list = func(ctx context.Context, peerID string) ([]PendingContent, error) {
		return []PendingContent{{
			ID: "8", ContentURI: "https://md.test/users/alice/statuses/50",
			ContentHashB64: anchoredHashOf(t), SubmittedBy: h.v.self, PeerID: peerID,
		}}, nil
	}
	if err := h.v.poll(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(h.submitted) != 0 {
		t.Fatal("self-submitted content must be skipped locally")
	}
}

// A malformed anchored hash is reported, never broadcast.
func TestMalformedAnchoredHashSkipped(t *testing.T) {
	h := newHarness(t, "not-base64!!")
	if err := h.v.poll(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(h.submitted) != 0 {
		t.Fatal("malformed anchored hash must not be broadcast")
	}
}

// supersededHarness serves an object with the given AS2 `updated` and lists
// a record whose protocol_metadata claims anchoredUpdated (nil = JSON null)
// and whose hash matches nothing, the shape an edit leaves behind.
func supersededHarness(t *testing.T, fetchedUpdated string, anchoredUpdated any) *harness {
	t.Helper()
	other := sha256.Sum256([]byte("the version before the edit"))
	h := newHarness(t, base64.StdEncoding.EncodeToString(other[:]))
	obj, err := apcanon.Parse([]byte(as2Note))
	if err != nil {
		t.Fatal(err)
	}
	if fetchedUpdated != "" {
		obj["updated"] = fetchedUpdated
	}
	h.v.fetch = func(context.Context, string) (map[string]any, error) { return obj, nil }
	meta, _ := json.Marshal(map[string]any{"hash_rule": apcanon.HashRuleName, "updated": anchoredUpdated})
	list := h.v.list
	h.v.list = func(ctx context.Context, peerID string) ([]PendingContent, error) {
		pending, err := list(ctx, peerID)
		for i := range pending {
			pending[i].ProtocolMetadataB64 = base64.StdEncoding.EncodeToString(meta)
		}
		return pending, err
	}
	return h
}

// An edit after anchoring is not fraud: the anchored version is gone and
// the bridge re-anchors the new one. Neither verify nor page -- the bridge
// now re-anchors every edit, so paging would fire on every edit.
func TestEditedAfterAnchorNeitherVerifiesNorAlarms(t *testing.T) {
	for _, anchored := range []any{nil, "2026-09-12T10:30:00Z"} {
		h := supersededHarness(t, "2026-09-12T11:00:00Z", anchored)
		for i := 0; i < 3; i++ {
			if err := h.v.poll(t.Context()); err != nil {
				t.Fatal(err)
			}
		}
		if len(h.submitted) != 0 || len(h.alarms) != 0 {
			t.Fatalf("anchored updated=%v: superseded record got %d submissions, %d alarms",
				anchored, len(h.submitted), len(h.alarms))
		}
	}
}

// A mismatch on the SAME version is the canonicalizer-bug-or-fraud case
// and must still page; so must one whose fetched version is not later
// than the anchored claim (a doctored record cannot opt out by lying).
func TestMismatchOnSameOrOlderVersionStillAlarms(t *testing.T) {
	cases := []struct {
		name     string
		fetched  string
		anchored any
	}{
		{"never edited", "", nil},
		{"same version", "2026-09-12T11:00:00Z", "2026-09-12T11:00:00Z"},
		{"fetched older than claimed", "2026-09-12T11:00:00Z", "2026-09-12T12:00:00Z"},
	}
	for _, c := range cases {
		h := supersededHarness(t, c.fetched, c.anchored)
		if err := h.v.poll(t.Context()); err != nil {
			t.Fatal(err)
		}
		if len(h.submitted) != 0 || len(h.alarms) != 1 || !strings.Contains(h.alarms[0], "HASH MISMATCH") {
			t.Fatalf("%s: want one HASH MISMATCH alarm and no submission, got %d submissions, alarms=%q",
				c.name, len(h.submitted), h.alarms)
		}
	}
}

// A post that does not belong to the peer is never fetched or verified,
// however well its hash would match: that would vouch for misattributed
// content.
func TestForeignHostAlarmsWithoutFetching(t *testing.T) {
	h := newHarness(t, anchoredHashOf(t))
	fetched := 0
	h.v.fetch = func(context.Context, string) (map[string]any, error) {
		fetched++
		return apcanon.Parse([]byte(as2Note))
	}
	list := h.v.list
	h.v.list = func(ctx context.Context, peerID string) ([]PendingContent, error) {
		pending, err := list(ctx, peerID)
		for i := range pending {
			pending[i].ContentURI = "https://aurora.test/users/alice/statuses/50"
		}
		return pending, err
	}
	if err := h.v.poll(t.Context()); err != nil {
		t.Fatal(err)
	}
	if fetched != 0 || len(h.submitted) != 0 {
		t.Fatalf("foreign post: %d fetches, %d submissions; want none", fetched, len(h.submitted))
	}
	if len(h.alarms) != 1 || !strings.Contains(h.alarms[0], "PROVENANCE") {
		t.Fatalf("want one PROVENANCE alarm, got %q", h.alarms)
	}

	// Listed in the policy's content_hosts: verified normally.
	h.v.alarmed = map[string]bool{}
	h.v.hosts = func(context.Context, string) ([]string, error) { return []string{"aurora.test"}, nil }
	if err := h.v.poll(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(h.submitted) != 1 {
		t.Fatalf("a content_hosts entry must be verifiable, got %d submissions", len(h.submitted))
	}
}

// Several peers per runner: a failing LCD listing for one peer must not
// stop verification on the others, nor clear its alarm latches (which
// would re-page a human for the same condition on the next poll).
func TestMultiPeerPollIsolatesFailures(t *testing.T) {
	h := newHarness(t, anchoredHashOf(t))
	h.v.cfg.PeerIDs = []string{"broken.test", "md.test"}
	list := h.v.list
	h.v.list = func(ctx context.Context, peerID string) ([]PendingContent, error) {
		if peerID == "broken.test" {
			return nil, context.DeadlineExceeded
		}
		return list(ctx, peerID)
	}
	h.v.alarmed["99"] = true // a latched alarm on broken.test's content

	if err := h.v.poll(t.Context()); err == nil {
		t.Fatal("the failing peer's error must still be reported")
	}
	if len(h.submitted) != 1 {
		t.Fatalf("md.test content must still be verified, got %d submissions", len(h.submitted))
	}
	if !h.v.alarmed["99"] {
		t.Fatal("an incomplete pass must not clear latches")
	}
}

// A misattributed record (right host, another instance's handle) is not
// verified either.
func TestForeignCreatorAlarms(t *testing.T) {
	h := newHarness(t, anchoredHashOf(t))
	list := h.v.list
	h.v.list = func(ctx context.Context, peerID string) ([]PendingContent, error) {
		pending, err := list(ctx, peerID)
		for i := range pending {
			pending[i].CreatorIdentity = "@alice@aurora.test"
		}
		return pending, err
	}
	if err := h.v.poll(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(h.submitted) != 0 || len(h.alarms) != 1 || !strings.Contains(h.alarms[0], "PROVENANCE") {
		t.Fatalf("want one PROVENANCE alarm and no verification, got %d submissions, alarms=%q", len(h.submitted), h.alarms)
	}
}
