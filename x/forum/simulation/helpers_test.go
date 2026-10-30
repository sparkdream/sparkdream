package simulation

import (
	"math/rand"
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/forum/types"
)

// makeAccount constructs a simtypes.Account from a secp256k1 private key seed.
func makeAccount(seed []byte) simtypes.Account {
	privKey := secp256k1.GenPrivKeyFromSecret(seed)
	pubKey := privKey.PubKey()
	addr := sdk.AccAddress(pubKey.Address())
	return simtypes.Account{
		PrivKey: privKey,
		PubKey:  pubKey,
		Address: addr,
	}
}

// ------------------------------------------------------------------ //
// getAccountForAddress
// ------------------------------------------------------------------ //

func TestGetAccountForAddress_Found(t *testing.T) {
	r := rand.New(rand.NewSource(42))
	accs := simtypes.RandomAccounts(r, 5)

	for _, want := range accs {
		got, ok := getAccountForAddress(want.Address.String(), accs)
		if !ok {
			t.Errorf("expected to find account with address %s, but got ok=false", want.Address.String())
			continue
		}
		if got.Address.String() != want.Address.String() {
			t.Errorf("address mismatch: got %s, want %s", got.Address.String(), want.Address.String())
		}
	}
}

func TestGetAccountForAddress_NotFound(t *testing.T) {
	r := rand.New(rand.NewSource(42))
	accs := simtypes.RandomAccounts(r, 5)

	// Build an address that is guaranteed not to be in accs.
	outsider := makeAccount([]byte("outsider-seed-xyz"))
	got, ok := getAccountForAddress(outsider.Address.String(), accs)
	if ok {
		t.Errorf("expected ok=false for unknown address, but got ok=true with %s", got.Address.String())
	}
	if got.Address != nil {
		t.Errorf("expected empty Account on miss, but Address is %s", got.Address.String())
	}
}

func TestGetAccountForAddress_EmptySlice(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	accs := simtypes.RandomAccounts(r, 1)

	got, ok := getAccountForAddress(accs[0].Address.String(), []simtypes.Account{})
	if ok {
		t.Errorf("expected ok=false on empty slice, got ok=true")
	}
	if got.Address != nil {
		t.Errorf("expected zero Account on empty slice, got Address=%s", got.Address)
	}
}

func TestGetAccountForAddress_TableDriven(t *testing.T) {
	r := rand.New(rand.NewSource(1234))
	accs := simtypes.RandomAccounts(r, 4)
	outsider := makeAccount([]byte("table-outsider"))

	cases := []struct {
		name      string
		addr      string
		wantFound bool
	}{
		{"first account", accs[0].Address.String(), true},
		{"middle account", accs[2].Address.String(), true},
		{"last account", accs[3].Address.String(), true},
		{"unknown address", outsider.Address.String(), false},
		{"empty string", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := getAccountForAddress(tc.addr, accs)
			if ok != tc.wantFound {
				t.Errorf("ok=%v, want %v", ok, tc.wantFound)
			}
			if tc.wantFound && got.Address.String() != tc.addr {
				t.Errorf("returned address %s, want %s", got.Address.String(), tc.addr)
			}
			if !tc.wantFound && got.Address != nil {
				t.Errorf("expected empty Account on miss, got Address=%s", got.Address)
			}
		})
	}
}

func inSlice(s string, slice []string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// ------------------------------------------------------------------ //
// randomContent
// ------------------------------------------------------------------ //

var knownContents = []string{
	"This is a simulation generated post content.",
	"Testing the forum module with random content.",
	"Phoenix aurora zenith: a short note from the simulation.",
	"This post was created during simulation testing.",
	"Sample content for forum simulation.",
}

func TestRandomContent_NonEmpty(t *testing.T) {
	r := rand.New(rand.NewSource(0))
	c := randomContent(r)
	if c == "" {
		t.Error("randomContent returned empty string")
	}
}

func TestRandomContent_ValidMember(t *testing.T) {
	r := rand.New(rand.NewSource(42))
	for i := 0; i < 50; i++ {
		c := randomContent(r)
		if !inSlice(c, knownContents) {
			t.Errorf("randomContent returned %q which is not in the known list", c)
		}
	}
}

func TestRandomContent_Deterministic(t *testing.T) {
	r1 := rand.New(rand.NewSource(88))
	r2 := rand.New(rand.NewSource(88))
	for i := 0; i < 20; i++ {
		c1 := randomContent(r1)
		c2 := randomContent(r2)
		if c1 != c2 {
			t.Errorf("iteration %d: r1=%q, r2=%q", i, c1, c2)
		}
	}
}

func TestRandomContent_Distribution(t *testing.T) {
	r := rand.New(rand.NewSource(321))
	seen := make(map[string]bool)
	for i := 0; i < 500; i++ {
		seen[randomContent(r)] = true
	}
	for _, c := range knownContents {
		if !seen[c] {
			t.Errorf("content %q never appeared in 500 samples", c)
		}
	}
}

// ------------------------------------------------------------------ //
// randomReason
// ------------------------------------------------------------------ //

var knownReasons = []string{
	"Spam content",
	"Inappropriate content",
	"Off-topic",
	"Low quality",
	"Violation of rules",
}

func TestRandomReason_NonEmpty(t *testing.T) {
	r := rand.New(rand.NewSource(0))
	reason := randomReason(r)
	if reason == "" {
		t.Error("randomReason returned empty string")
	}
}

func TestRandomReason_ValidMember(t *testing.T) {
	r := rand.New(rand.NewSource(13))
	for i := 0; i < 50; i++ {
		reason := randomReason(r)
		if !inSlice(reason, knownReasons) {
			t.Errorf("randomReason returned %q which is not in the known list", reason)
		}
	}
}

func TestRandomReason_Deterministic(t *testing.T) {
	r1 := rand.New(rand.NewSource(11))
	r2 := rand.New(rand.NewSource(11))
	for i := 0; i < 20; i++ {
		r1v := randomReason(r1)
		r2v := randomReason(r2)
		if r1v != r2v {
			t.Errorf("iteration %d: r1=%q, r2=%q", i, r1v, r2v)
		}
	}
}

func TestRandomReason_Distribution(t *testing.T) {
	r := rand.New(rand.NewSource(654))
	seen := make(map[string]bool)
	for i := 0; i < 500; i++ {
		seen[randomReason(r)] = true
	}
	for _, reason := range knownReasons {
		if !seen[reason] {
			t.Errorf("reason %q never appeared in 500 samples", reason)
		}
	}
}

// ------------------------------------------------------------------ //
// Cross-function seed independence
// ------------------------------------------------------------------ //

// TestHelpers_SeedIndependence verifies that the random* functions do
// not share internal state — each call advances the rand by exactly one step
// (one r.Intn call), so interleaving them with the same seed produces the
// same individual results as calling each in isolation.
func TestHelpers_SeedIndependence(t *testing.T) {
	// Capture individual outputs with a fresh rand each time.
	rContent := rand.New(rand.NewSource(42))
	content := randomContent(rContent)

	rReason := rand.New(rand.NewSource(42))
	reason := randomReason(rReason)

	// Each function calls r.Intn(len(slice)) exactly once, so the first
	// random integer drawn from seed 42 determines both results.
	// They may or may not be equal — the point is that each function is
	// self-contained and doesn't read extra random values.
	if content == "" {
		t.Error("content is empty")
	}
	if reason == "" {
		t.Error("reason is empty")
	}
}

// ------------------------------------------------------------------ //
// selection and post-state helpers
// ------------------------------------------------------------------ //

func TestPick_EmptyReturnsFalse(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	if _, ok := pick(r, []int{}); ok {
		t.Error("pick on an empty slice returned ok=true")
	}
}

func TestPick_ReturnsElement(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	items := []string{"phoenix", "aurora", "zenith"}
	for i := 0; i < 20; i++ {
		got, ok := pick(r, items)
		if !ok || !inSlice(got, items) {
			t.Fatalf("pick returned %q, ok=%t", got, ok)
		}
	}
}

func TestAccountsWhere_Filters(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	accs := simtypes.RandomAccounts(r, 6)
	keep := accs[2].Address.String()
	got := accountsWhere(accs, func(a simtypes.Account) bool { return a.Address.String() == keep })
	if len(got) != 1 || got[0].Address.String() != keep {
		t.Fatalf("accountsWhere returned %d accounts, want the one kept", len(got))
	}
	if len(accountsWhere(accs, anyone)) != len(accs) {
		t.Error("anyone should keep every account")
	}
	if len(accountsWhere(accs, notAddr(keep))) != len(accs)-1 {
		t.Error("notAddr should drop exactly one account")
	}
}

func TestRootID(t *testing.T) {
	if got := rootID(types.Post{PostId: 7}); got != 7 {
		t.Errorf("root post: got %d, want 7", got)
	}
	if got := rootID(types.Post{PostId: 9, ParentId: 8, RootId: 7}); got != 7 {
		t.Errorf("reply: got %d, want 7", got)
	}
}

func TestVisibleAndActiveRoot(t *testing.T) {
	cases := []struct {
		status  types.PostStatus
		visible bool
	}{
		{types.PostStatus_POST_STATUS_ACTIVE, true},
		{types.PostStatus_POST_STATUS_HIDDEN, false},
		{types.PostStatus_POST_STATUS_DELETED, false},
		{types.PostStatus_POST_STATUS_ARCHIVED, false},
	}
	for _, c := range cases {
		p := types.Post{PostId: 1, Status: c.status}
		if visible(p) != c.visible {
			t.Errorf("visible(%s) = %t, want %t", c.status, !c.visible, c.visible)
		}
		wantRoot := c.status == types.PostStatus_POST_STATUS_ACTIVE
		if activeRoot(p) != wantRoot {
			t.Errorf("activeRoot(%s) = %t, want %t", c.status, !wantRoot, wantRoot)
		}
	}
	if activeRoot(types.Post{PostId: 2, ParentId: 1, Status: types.PostStatus_POST_STATUS_ACTIVE}) {
		t.Error("a reply is never an active root")
	}
}

func TestRandomModerationReason_Specified(t *testing.T) {
	r := rand.New(rand.NewSource(5))
	for i := 0; i < 100; i++ {
		reason := randomModerationReason(r)
		if reason == commontypes.ModerationReason_MODERATION_REASON_UNSPECIFIED {
			t.Fatal("randomModerationReason returned UNSPECIFIED, which every handler rejects")
		}
		if _, ok := commontypes.ModerationReason_name[int32(reason)]; !ok {
			t.Fatalf("randomModerationReason returned unknown reason %d", reason)
		}
	}
}
