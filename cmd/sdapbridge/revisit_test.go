package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const noteURI = "https://md.test/users/alice/statuses/10"

// fakeInstance is the bridge's own Mastodon: the REST side (timeline,
// status lookup, search) whose edited_at can run ahead of the AS2 side,
// the way Mastodon's 3-minute AS2 cache makes it do.
type fakeInstance struct {
	mu        sync.Mutex
	editedAt  *string // REST edited_at of status 10
	lookups   int     // GET /api/v1/statuses?id[]= requests
	searches  int     // GET /api/v2/search requests
	timeline  bool    // whether the home timeline lists status 10
	searchHit bool    // whether search resolves noteURI
}

func (f *fakeInstance) status() map[string]any {
	return map[string]any{
		"id":         "10",
		"uri":        noteURI,
		"url":        "https://md.test/@alice/10",
		"created_at": "2026-09-10T10:00:00.000Z",
		"edited_at":  f.editedAt,
		"account":    map[string]any{"acct": "alice", "display_name": "Alice", "uri": "https://md.test/users/alice"},
	}
}

func (f *fakeInstance) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		var out any
		switch r.URL.Path {
		case "/api/v1/timelines/home":
			// Honour since_id like Mastodon: an edited status keeps its id,
			// so once the cursor passes it the timeline never lists it again.
			list := []any{}
			if since := r.URL.Query().Get("since_id"); f.timeline && (since == "" || statusIDLess(since, "10")) {
				list = append(list, f.status())
			}
			out = list
		case "/api/v1/statuses":
			f.lookups++
			list := []any{}
			for _, id := range r.URL.Query()["id[]"] {
				if id == "10" {
					list = append(list, f.status())
				}
			}
			out = list
		case "/api/v2/search":
			f.searches++
			if r.URL.Query().Get("resolve") != "true" {
				t.Errorf("URL lookups need resolve=true, got %q", r.URL.RawQuery)
			}
			list := []any{}
			if f.searchHit && r.URL.Query().Get("q") == noteURI {
				list = append(list, f.status())
			}
			out = map[string]any{"statuses": list}
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (f *fakeInstance) setEditedAt(v string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.editedAt = &v
}

// countingFetcher counts AS2 fetches, to prove an unedited status costs
// the revisit sweep no remote request.
type countingFetcher struct {
	AS2Source
	n int
}

func (c *countingFetcher) Fetch(ctx context.Context, uri string) (map[string]any, error) {
	c.n++
	return c.AS2Source.Fetch(ctx, uri)
}

// revisitBridge anchors status 10 through the home timeline, the path
// that supplies a REST id, and returns everything a revisit test drives.
func revisitBridge(t *testing.T) (*Bridge, *fakeSubmitter, map[string]string, *fakeInstance, *countingFetcher) {
	t.Helper()
	objects := map[string]string{"/users/alice/statuses/10": publicNote}
	b, sub := testBridge(t, objects)
	inst := &fakeInstance{timeline: true}
	b.masto = NewMastodonClient(inst.server(t).URL, "token")
	fetch := &countingFetcher{AS2Source: b.fetcher}
	b.fetcher = fetch
	b.cfg.RevisitWindow = time.Hour

	if err := b.pollOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(sub.msgs) != 1 {
		t.Fatalf("setup: want the original anchored, got %d submissions", len(sub.msgs))
	}
	tr, ok := b.state.Tracking(noteURI)
	if !ok || tr.StatusID != "10" || tr.Updated != "" {
		t.Fatalf("setup: tracking = %+v (ok=%v), want status 10 with no updated", tr, ok)
	}
	// Give the first anchor a content id, as the confirmed tx would.
	for _, rec := range b.state.Seen {
		rec.ContentID = 42
	}
	return b, sub, objects, inst, fetch
}

// P5 acceptance: an edit produces a second record carrying
// supersedes_content_id -- found by the revisit sweep, since the home
// timeline never re-lists an edited status.
func TestRevisitReanchorsEdit(t *testing.T) {
	b, sub, objects, inst, _ := revisitBridge(t)

	objects["/users/alice/statuses/10"] = editedNote
	inst.setEditedAt("2026-09-10T11:00:00.482Z")

	// The timeline cursor has moved past status 10: polling alone must
	// not find the edit. This is the gap the sweep exists to close.
	if err := b.pollOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(sub.msgs) != 1 {
		t.Fatalf("the timeline re-listed an edit (%d submissions); the premise of the sweep changed", len(sub.msgs))
	}

	if err := b.revisitOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(sub.msgs) != 2 {
		t.Fatalf("revisit must re-anchor the edit: %d submissions", len(sub.msgs))
	}
	var meta map[string]any
	if err := json.Unmarshal(sub.msgs[1].ProtocolMetadata, &meta); err != nil {
		t.Fatal(err)
	}
	if meta["supersedes_content_id"] != "42" {
		t.Fatalf("supersedes_content_id = %v, want 42", meta["supersedes_content_id"])
	}
	if tr, _ := b.state.Tracking(noteURI); tr.Updated != "2026-09-10T11:00:00Z" {
		t.Fatalf("tracked updated = %q, want the AS2 value of the version hashed", tr.Updated)
	}

	// A second sweep over the same edit is a no-op.
	if err := b.revisitOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(sub.msgs) != 2 {
		t.Fatalf("an already-anchored edit was re-submitted: %d submissions", len(sub.msgs))
	}
}

// REST reports the edit before AS2 serves it (Mastodon's AS2 cache). The
// sweep must not hash the stale body or mark the edit handled; the next
// sweep, once AS2 catches up, anchors it.
func TestRevisitWaitsOutStaleAS2(t *testing.T) {
	b, sub, objects, inst, _ := revisitBridge(t)

	inst.setEditedAt("2026-09-10T11:00:00.482Z") // AS2 still serves publicNote
	if err := b.revisitOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(sub.msgs) != 1 {
		t.Fatalf("a stale AS2 body must not be anchored: %d submissions", len(sub.msgs))
	}
	if tr, _ := b.state.Tracking(noteURI); tr.Updated != "" {
		t.Fatalf("stale fetch recorded updated=%q; the edit would be lost", tr.Updated)
	}

	objects["/users/alice/statuses/10"] = editedNote
	if err := b.revisitOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(sub.msgs) != 2 {
		t.Fatalf("the edit must be anchored once AS2 catches up: %d submissions", len(sub.msgs))
	}
}

// An unedited status costs one batched REST lookup and no AS2 fetch.
func TestRevisitSkipsUnedited(t *testing.T) {
	b, sub, _, inst, fetch := revisitBridge(t)
	before := fetch.n

	if err := b.revisitOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if fetch.n != before {
		t.Fatalf("an unedited status was re-fetched over AS2 (%d fetches)", fetch.n-before)
	}
	if inst.lookups != 1 {
		t.Fatalf("want one batched REST lookup, got %d", inst.lookups)
	}
	if len(sub.msgs) != 1 {
		t.Fatalf("nothing changed, yet %d submissions", len(sub.msgs))
	}
}

// Statuses anchored before the window are dropped rather than revisited
// forever.
func TestRevisitWindowExpires(t *testing.T) {
	b, _, _, inst, _ := revisitBridge(t)
	b.state.Tracked[noteURI].Since = time.Now().Add(-2 * time.Hour).Unix()

	if err := b.revisitOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if inst.lookups != 0 {
		t.Fatalf("an aged-out status was still looked up (%d requests)", inst.lookups)
	}
	if _, ok := b.state.Tracking(noteURI); ok {
		t.Fatal("an aged-out status must be dropped from the revisit list")
	}
}

// A backfilled status has no REST id; one resolve=true search supplies
// it, after which it is revisitable like any other.
func TestBackfilledStatusResolvesRESTID(t *testing.T) {
	b, _ := testBridge(t, map[string]string{"/users/alice/statuses/10": publicNote})
	inst := &fakeInstance{searchHit: true}
	b.masto = NewMastodonClient(inst.server(t).URL, "token")

	acct := &Account{Acct: "alice", URI: "https://md.test/users/alice"}
	if err := b.IngestURI(t.Context(), noteURI, acct); err != nil {
		t.Fatal(err)
	}
	if tr, _ := b.state.Tracking(noteURI); tr.StatusID != "10" {
		t.Fatalf("backfilled status tracked with id %q, want 10 from search", tr.StatusID)
	}
	// Re-seeing it must not search again.
	if err := b.IngestURI(t.Context(), noteURI, acct); err != nil {
		t.Fatal(err)
	}
	if inst.searches != 1 {
		t.Fatalf("want one search for the REST id, got %d", inst.searches)
	}
}

func TestEditedSince(t *testing.T) {
	s := func(v string) *string { return &v }
	cases := []struct {
		name     string
		editedAt *string
		updated  string
		want     bool
	}{
		{"never edited", nil, "", false},
		{"first edit, nothing hashed after it", s("2026-09-10T11:00:00.482Z"), "", true},
		// REST carries milliseconds, AS2 whole seconds: same edit.
		{"same second", s("2026-09-10T11:00:00.482Z"), "2026-09-10T11:00:00Z", false},
		{"newer edit", s("2026-09-10T11:05:00.001Z"), "2026-09-10T11:00:00Z", true},
		{"AS2 ahead of REST", s("2026-09-10T11:00:00.000Z"), "2026-09-10T11:05:00Z", false},
		{"unparseable counts as an edit", s("yesterday"), "2026-09-10T11:00:00Z", true},
	}
	for _, c := range cases {
		if got := editedSince(c.editedAt, c.updated); got != c.want {
			t.Errorf("%s: editedSince = %v, want %v", c.name, got, c.want)
		}
	}
}

// Guard against a batch the instance would reject outright.
func TestStatusesRejectsOversizedBatch(t *testing.T) {
	m := NewMastodonClient("https://local.instance", "token")
	ids := strings.Split(strings.Repeat("1,", maxStatusesPerLookup+1), ",")[:maxStatusesPerLookup+1]
	if _, err := m.Statuses(context.Background(), ids); err == nil {
		t.Fatal("a batch over the per-request cap must be refused before it is sent")
	}
}

// Mastodon 4.7 numeric actor ids carry no username; deriving one from the
// path produced "@117318268488994076@host/ap".
func TestHandleFromActorRefusesNumericIDs(t *testing.T) {
	if got := handleFromActor("https://md.test/users/alice", "md.test"); got != "@alice@md.test" {
		t.Fatalf("username actor id: got %q", got)
	}
	if got := handleFromActor("http://localhost:3000/ap/users/117318268488994076", "localhost"); got != "" {
		t.Fatalf("numeric actor id must yield no handle, got %q", got)
	}
}
