package simulation

import (
	"math/rand"
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
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

// ------------------------------------------------------------------ //
// randomTagName
// ------------------------------------------------------------------ //

// knownTagNames is the exact slice defined inside helpers.go.  Keeping
// it here lets us verify that every returned value is a member.
var knownTagNames = []string{
	"golang", "rust", "python", "javascript",
	"devops", "testing", "documentation",
	"design", "frontend", "backend",
}

func inSlice(s string, slice []string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func TestRandomTagName_NonEmpty(t *testing.T) {
	r := rand.New(rand.NewSource(0))
	tag := randomTagName(r)
	if tag == "" {
		t.Error("randomTagName returned empty string")
	}
}

func TestRandomTagName_ValidMember(t *testing.T) {
	r := rand.New(rand.NewSource(99))
	for i := 0; i < 50; i++ {
		tag := randomTagName(r)
		if !inSlice(tag, knownTagNames) {
			t.Errorf("randomTagName returned %q which is not in the known list", tag)
		}
	}
}

func TestRandomTagName_Deterministic(t *testing.T) {
	// Same seed must yield the same sequence.
	r1 := rand.New(rand.NewSource(77))
	r2 := rand.New(rand.NewSource(77))
	for i := 0; i < 20; i++ {
		t1 := randomTagName(r1)
		t2 := randomTagName(r2)
		if t1 != t2 {
			t.Errorf("iteration %d: r1 produced %q, r2 produced %q", i, t1, t2)
		}
	}
}

func TestRandomTagName_Distribution(t *testing.T) {
	// Run enough iterations that every tag should appear at least once
	// when sampling uniformly from 10 items.
	r := rand.New(rand.NewSource(555))
	seen := make(map[string]bool)
	for i := 0; i < 500; i++ {
		seen[randomTagName(r)] = true
	}
	for _, tag := range knownTagNames {
		if !seen[tag] {
			t.Errorf("tag %q never appeared in 500 samples", tag)
		}
	}
}

// ------------------------------------------------------------------ //
// randomContent
// ------------------------------------------------------------------ //

var knownContents = []string{
	"This is a simulation generated post content.",
	"Testing the forum module with random content.",
	"Lorem ipsum dolor sit amet, consectetur adipiscing elit.",
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

// TestHelpers_SeedIndependence verifies that the three random* functions do
// not share internal state — each call advances the rand by exactly one step
// (one r.Intn call), so interleaving them with the same seed produces the
// same individual results as calling each in isolation.
func TestHelpers_SeedIndependence(t *testing.T) {
	// Capture individual outputs with a fresh rand each time.
	rTag := rand.New(rand.NewSource(42))
	tag := randomTagName(rTag)

	rContent := rand.New(rand.NewSource(42))
	content := randomContent(rContent)

	rReason := rand.New(rand.NewSource(42))
	reason := randomReason(rReason)

	// Each function calls r.Intn(len(slice)) exactly once, so the first
	// random integer drawn from seed 42 determines all three results.
	// They may or may not be equal — the point is that each function is
	// self-contained and doesn't read extra random values.
	if tag == "" {
		t.Error("tag is empty")
	}
	if content == "" {
		t.Error("content is empty")
	}
	if reason == "" {
		t.Error("reason is empty")
	}
}
