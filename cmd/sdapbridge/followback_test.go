package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
)

// fakeFollowBackInstance is the follow-back side of a Mastodon instance: the bridge
// account's followers and follows, sign-ups, and the wallet addresses of
// wallet sign-in accounts.
type fakeFollowBackInstance struct {
	mu            sync.Mutex
	registrations bool
	followers     map[string]string // id -> acct
	following     map[string]string // id -> acct
	wallets       map[string]string // id -> address hex
	followed      []string
	unfollowed    []string
	asked         [][]string
}

func (f *fakeFollowBackInstance) handler(t *testing.T) http.HandlerFunc {
	list := func(m map[string]string) []Account {
		var out []Account
		for id, acct := range m {
			out = append(out, Account{ID: id, Acct: acct})
		}
		return out
	}
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case r.URL.Path == "/api/v1/accounts/verify_credentials":
			_ = json.NewEncoder(w).Encode(Account{ID: "self", Acct: "bridgedev"})
		case r.URL.Path == "/api/v2/instance":
			_ = json.NewEncoder(w).Encode(map[string]any{"registrations": map[string]bool{"enabled": f.registrations}})
		case r.URL.Path == "/api/v1/accounts/self/followers":
			_ = json.NewEncoder(w).Encode(list(f.followers))
		case r.URL.Path == "/api/v1/accounts/self/following":
			_ = json.NewEncoder(w).Encode(list(f.following))
		case r.URL.Path == "/api/v1/sparkdream/wallet_addresses":
			ids := r.URL.Query()["id[]"]
			sort.Strings(ids)
			f.asked = append(f.asked, ids)
			out := map[string]string{}
			for _, id := range ids {
				// the instance answers only for accounts that follow the bridge
				if _, follows := f.followers[id]; follows && f.wallets[id] != "" {
					out[id] = f.wallets[id]
				}
			}
			_ = json.NewEncoder(w).Encode(out)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/follow"):
			id := strings.Split(r.URL.Path, "/")[4]
			f.followed = append(f.followed, id)
			f.following[id] = f.followers[id]
			_, _ = w.Write([]byte("{}"))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/unfollow"):
			id := strings.Split(r.URL.Path, "/")[4]
			f.unfollowed = append(f.unfollowed, id)
			delete(f.following, id)
			_, _ = w.Write([]byte("{}"))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}
}

func (f *fakeFollowBackInstance) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.followed, f.unfollowed, f.asked = nil, nil, nil
}

// addresses of one chain's members, and of someone who is a member only of
// another linked chain (the same instance serves both)
var (
	aliceAddr = strings.Repeat("a1", 20)
	bobAddr   = strings.Repeat("b0", 20)
	daveAddr  = strings.Repeat("d4", 20)
)

func followBackBridge(t *testing.T, f *fakeFollowBackInstance, open bool, members map[string]bool, down *bool) *Bridge {
	t.Helper()
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	b, _ := testBridge(t, nil)
	b.cfg.FollowBack = true
	b.masto = NewMastodonClient(srv.URL, "tok")
	b.gateSource = func(context.Context, string) (authorGate, error) {
		if open {
			return buildGate([]string{"*"}, false, nil), nil
		}
		return buildGate([]string{"@alice@md.test"}, false, nil), nil
	}
	b.members = func(_ context.Context, addr []byte) (bool, error) {
		if down != nil && *down {
			return false, errors.New("lcd unreachable")
		}
		return members[hex.EncodeToString(addr)], nil
	}
	return b
}

func TestFollowBackMembersOfThisChain(t *testing.T) {
	f := &fakeFollowBackInstance{
		followers: map[string]string{
			"1": "alice",                 // wallet account, a member here
			"2": "bob",                   // wallet account, a member of another chain only
			"3": "carol",                 // not a wallet account
			"4": "zed@elsewhere.example", // remote: never followed back
		},
		following: map[string]string{"9": "dave"}, // followed by hand
		wallets:   map[string]string{"1": aliceAddr, "2": bobAddr, "9": daveAddr},
	}
	down := false
	b := followBackBridge(t, f, true, map[string]bool{aliceAddr: true}, &down)

	if err := b.syncFollowBacks(t.Context()); err != nil {
		t.Fatal(err)
	}
	if strings.Join(f.followed, ",") != "1" || len(f.unfollowed) != 0 {
		t.Fatalf("followed %v unfollowed %v, want [1] and none", f.followed, f.unfollowed)
	}
	// only local followers were asked about, never the remote one
	if len(f.asked) != 1 || strings.Join(f.asked[0], ",") != "1,2,3" {
		t.Fatalf("asked about %v, want [1 2 3]", f.asked)
	}
	if got := b.state.FollowBacks(); strings.Join(got, ",") != "1" {
		t.Fatalf("recorded follow-backs %v", got)
	}

	// a second pass changes nothing
	f.reset()
	if err := b.syncFollowBacks(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(f.followed)+len(f.unfollowed) != 0 {
		t.Fatalf("second pass followed %v unfollowed %v", f.followed, f.unfollowed)
	}

	// the chain cannot be asked: nothing is undone on a maybe
	f.reset()
	down = true
	if err := b.syncFollowBacks(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(f.followed)+len(f.unfollowed) != 0 {
		t.Fatalf("with the chain down: followed %v unfollowed %v", f.followed, f.unfollowed)
	}
	down = false

	// alice unfollows the bridge: it unfollows her back, and only her
	f.reset()
	delete(f.followers, "1")
	if err := b.syncFollowBacks(t.Context()); err != nil {
		t.Fatal(err)
	}
	if strings.Join(f.unfollowed, ",") != "1" || len(f.followed) != 0 {
		t.Fatalf("after alice unfollowed: followed %v unfollowed %v, want unfollow [1]", f.followed, f.unfollowed)
	}
	if len(b.state.FollowBacks()) != 0 {
		t.Fatalf("follow-back still recorded: %v", b.state.FollowBacks())
	}
	if _, kept := f.following["9"]; !kept {
		t.Fatal("a follow made by hand was undone")
	}
}

func TestFollowBackUndoneWhenMembershipEnds(t *testing.T) {
	f := &fakeFollowBackInstance{
		followers: map[string]string{"1": "alice"},
		following: map[string]string{},
		wallets:   map[string]string{"1": aliceAddr},
	}
	members := map[string]bool{aliceAddr: true}
	b := followBackBridge(t, f, true, members, nil)
	if err := b.syncFollowBacks(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.reset()
	members[aliceAddr] = false // zeroed, or gone inactive
	if err := b.syncFollowBacks(t.Context()); err != nil {
		t.Fatal(err)
	}
	if strings.Join(f.unfollowed, ",") != "1" {
		t.Fatalf("unfollowed %v, want [1]", f.unfollowed)
	}
}

func TestFollowBackOnlyOnAnOpenPeerWithSignUpsClosed(t *testing.T) {
	cases := []struct {
		name          string
		open          bool
		registrations bool
		enabled       bool
	}{
		{name: "curated peer: curation decides", open: false},
		{name: "sign-ups open or by approval", open: true, registrations: true, enabled: true},
		{name: "turned off", open: true, enabled: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeFollowBackInstance{
				registrations: tc.registrations,
				followers:     map[string]string{"1": "alice"},
				following:     map[string]string{},
				wallets:       map[string]string{"1": aliceAddr},
			}
			b := followBackBridge(t, f, tc.open, map[string]bool{aliceAddr: true}, nil)
			if !tc.enabled && tc.open && !tc.registrations {
				b.cfg.FollowBack = false
			}
			if err := b.syncFollowBacks(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(f.followed)+len(f.unfollowed) != 0 || len(f.asked) != 0 {
				t.Fatalf("followed %v unfollowed %v asked %v", f.followed, f.unfollowed, f.asked)
			}
		})
	}
}

func TestMemberActive(t *testing.T) {
	for raw, want := range map[string]bool{
		``:                         true, // proto3 zero
		`null`:                     true,
		`"MEMBER_STATUS_ACTIVE"`:   true,
		`0`:                        true,
		`"MEMBER_STATUS_INACTIVE"`: false,
		`"MEMBER_STATUS_ZEROED"`:   false,
		`2`:                        false,
	} {
		if got := memberActive(json.RawMessage(raw)); got != want {
			t.Errorf("memberActive(%s) = %v, want %v", raw, got, want)
		}
	}
}
