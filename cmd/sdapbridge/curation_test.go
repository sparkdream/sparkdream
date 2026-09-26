package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
)

// The bridge's view of a peer's gate must match the chain's rule: both the
// allowlist ("*" = anyone) and, when set, the curation collection.
func TestBuildGate(t *testing.T) {
	col := []string{"https://md.test/@alice", "@carol@md.test", "not a handle"}
	cases := []struct {
		name          string
		allowed       []string
		hasCollection bool
		admits        map[string]bool
	}{
		{"nothing configured admits nobody", nil, false, map[string]bool{"@alice@md.test": false}},
		{"open", []string{"*"}, false, map[string]bool{"@alice@md.test": true, "@zed@md.test": true}},
		{"list only", []string{"@alice@md.test"}, false, map[string]bool{"@alice@md.test": true, "@carol@md.test": false}},
		{"collection only", []string{"*"}, true, map[string]bool{"@alice@md.test": true, "@Carol@md.test": true, "@bob@md.test": false}},
		{"both", []string{"@alice@md.test", "@bob@md.test"}, true, map[string]bool{"@alice@md.test": true, "@bob@md.test": false, "@carol@md.test": false}},
	}
	for _, c := range cases {
		g := buildGate(c.allowed, c.hasCollection, col)
		for h, want := range c.admits {
			if got := g.admits(h); got != want {
				t.Errorf("%s: admits(%s) = %v, want %v", c.name, h, got, want)
			}
		}
	}
}

// An uncurated author is skipped before consent is looked at; an unreadable
// gate leaves the decision to the chain.
func TestCuratedGatesAllowed(t *testing.T) {
	b, _ := testBridge(t, nil)
	calls := 0
	b.gateSource = func(_ context.Context, peer string) (authorGate, error) {
		calls++
		return buildGate([]string{"*"}, true, []string{"@alice@md.test"}), nil
	}
	alice := &Account{Acct: "alice", URI: "https://md.test/users/alice"}
	bob := &Account{Acct: "bob", URI: "https://md.test/users/bob"}
	if !b.allowed(t.Context(), alice) {
		t.Fatal("curated alice refused")
	}
	if b.allowed(t.Context(), bob) {
		t.Fatal("uncurated bob allowed")
	}
	if _, ok := b.state.ConsentSince[bob.URI]; ok {
		t.Fatal("a curation refusal must not touch the consent record")
	}
	b.allowed(t.Context(), alice)
	if calls != 1 {
		t.Fatalf("gate read %d times, want 1 (cached)", calls)
	}

	unread, _ := testBridge(t, nil)
	unread.gateSource = func(context.Context, string) (authorGate, error) { return authorGate{}, context.DeadlineExceeded }
	if !unread.allowed(t.Context(), bob) {
		t.Fatal("an unreadable gate must defer to the chain, not refuse")
	}
}

// fakeFollows is a Mastodon instance's follow API for one bridge account.
type fakeFollows struct {
	mu        sync.Mutex
	following map[string]string // id -> acct
	accounts  map[string]string // acct -> id
	followed  []string
	unfollows []string
}

func (f *fakeFollows) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case r.URL.Path == "/api/v1/accounts/verify_credentials":
			_ = json.NewEncoder(w).Encode(Account{ID: "self", Acct: "bridge"})
		case r.URL.Path == "/api/v1/accounts/self/following":
			var out []Account
			for id, acct := range f.following {
				out = append(out, Account{ID: id, Acct: acct})
			}
			_ = json.NewEncoder(w).Encode(out)
		case r.URL.Path == "/api/v1/accounts/lookup":
			id, ok := f.accounts[r.URL.Query().Get("acct")]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(Account{ID: id})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/follow"):
			id := strings.Split(r.URL.Path, "/")[4]
			f.followed = append(f.followed, id)
			_, _ = w.Write([]byte("{}"))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/unfollow"):
			id := strings.Split(r.URL.Path, "/")[4]
			f.unfollows = append(f.unfollows, id)
			_, _ = w.Write([]byte("{}"))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}
}

// The bridge follows exactly the curated authors: missing ones followed,
// dropped ones unfollowed, follows on other hosts left alone.
func TestSyncFollowsToCuration(t *testing.T) {
	f := &fakeFollows{
		following: map[string]string{"1": "alice", "2": "bob", "9": "zed@elsewhere.example"},
		accounts:  map[string]string{"alice": "1", "bob": "2", "carol": "3"},
	}
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	b, _ := testBridge(t, nil)
	b.masto = NewMastodonClient(srv.URL, "tok")
	b.gateSource = func(context.Context, string) (authorGate, error) {
		return buildGate([]string{"*"}, true, []string{"@alice@md.test", "@carol@md.test"}), nil
	}
	if err := b.syncFollows(t.Context()); err != nil {
		t.Fatal(err)
	}
	sort.Strings(f.followed)
	if strings.Join(f.followed, ",") != "3" || strings.Join(f.unfollows, ",") != "2" {
		t.Fatalf("followed %v unfollowed %v, want [3] and [2]", f.followed, f.unfollows)
	}

	// an open gate: the bridge's follows are left entirely alone
	f.followed, f.unfollows = nil, nil
	open, _ := testBridge(t, nil)
	open.masto = NewMastodonClient(srv.URL, "tok")
	open.gateSource = func(context.Context, string) (authorGate, error) { return buildGate([]string{"*"}, false, nil), nil }
	if err := open.syncFollows(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(f.followed)+len(f.unfollows) != 0 {
		t.Fatalf("open gate changed follows: %v %v", f.followed, f.unfollows)
	}
}
