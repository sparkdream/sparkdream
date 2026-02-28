package simulation

import (
	"math/rand"
	"regexp"
	"strings"
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
)

// newDeterministicRand returns a deterministic *rand.Rand for use in tests.
func newDeterministicRand(seed int64) *rand.Rand {
	return rand.New(rand.NewSource(seed))
}

// buildSimAccount creates a simtypes.Account from a secp256k1 private key.
func buildSimAccount(privKey *secp256k1.PrivKey) simtypes.Account {
	pubKey := privKey.PubKey()
	addr := sdk.AccAddress(pubKey.Address())
	return simtypes.Account{
		PrivKey: privKey,
		PubKey:  pubKey,
		Address: addr,
	}
}

// ---------------------------------------------------------------------------
// randomGuildName
// ---------------------------------------------------------------------------

func TestRandomGuildName_NonEmpty(t *testing.T) {
	r := newDeterministicRand(42)
	for i := 0; i < 50; i++ {
		name := randomGuildName(r)
		if name == "" {
			t.Fatalf("iteration %d: randomGuildName returned empty string", i)
		}
	}
}

func TestRandomGuildName_HasExpectedPrefix(t *testing.T) {
	// All names are formatted as "The <word> <number>".
	r := newDeterministicRand(7)
	for i := 0; i < 100; i++ {
		name := randomGuildName(r)
		if !strings.HasPrefix(name, "The ") {
			t.Fatalf("iteration %d: expected name to start with 'The ', got %q", i, name)
		}
	}
}

func TestRandomGuildName_ContainsKnownWord(t *testing.T) {
	known := []string{
		"Warriors", "Explorers", "Guardians", "Pioneers",
		"Champions", "Legends", "Defenders", "Builders",
	}
	r := newDeterministicRand(99)
	for i := 0; i < 200; i++ {
		name := randomGuildName(r)
		found := false
		for _, w := range known {
			if strings.Contains(name, w) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("iteration %d: guild name %q does not contain any known word", i, name)
		}
	}
}

func TestRandomGuildName_Deterministic(t *testing.T) {
	seed := int64(1234)
	name1 := randomGuildName(newDeterministicRand(seed))
	name2 := randomGuildName(newDeterministicRand(seed))
	if name1 != name2 {
		t.Fatalf("same seed produced different results: %q vs %q", name1, name2)
	}
}

// ---------------------------------------------------------------------------
// randomDisplayName
// ---------------------------------------------------------------------------

func TestRandomDisplayName_NonEmpty(t *testing.T) {
	r := newDeterministicRand(42)
	for i := 0; i < 50; i++ {
		name := randomDisplayName(r)
		if name == "" {
			t.Fatalf("iteration %d: randomDisplayName returned empty string", i)
		}
	}
}

func TestRandomDisplayName_ContainsKnownPrefix(t *testing.T) {
	prefixes := []string{
		"Cool", "Epic", "Super", "Mega", "Ultra", "Pro", "Elite", "Master",
	}
	r := newDeterministicRand(55)
	for i := 0; i < 200; i++ {
		name := randomDisplayName(r)
		found := false
		for _, p := range prefixes {
			if strings.HasPrefix(name, p) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("iteration %d: display name %q does not start with a known prefix", i, name)
		}
	}
}

func TestRandomDisplayName_ContainsKnownSuffix(t *testing.T) {
	suffixes := []string{
		"Player", "Gamer", "User", "Member", "Hero", "Star", "Champion", "Legend",
	}
	r := newDeterministicRand(77)
	for i := 0; i < 200; i++ {
		name := randomDisplayName(r)
		found := false
		for _, s := range suffixes {
			if strings.Contains(name, s) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("iteration %d: display name %q does not contain a known suffix", i, name)
		}
	}
}

func TestRandomDisplayName_Deterministic(t *testing.T) {
	seed := int64(9999)
	d1 := randomDisplayName(newDeterministicRand(seed))
	d2 := randomDisplayName(newDeterministicRand(seed))
	if d1 != d2 {
		t.Fatalf("same seed produced different results: %q vs %q", d1, d2)
	}
}

// ---------------------------------------------------------------------------
// randomUsername
// ---------------------------------------------------------------------------

var alphanumericRe = regexp.MustCompile(`^[a-z0-9]+$`)

func TestRandomUsername_ExactlyEightChars(t *testing.T) {
	r := newDeterministicRand(42)
	for i := 0; i < 100; i++ {
		u := randomUsername(r)
		if len(u) != 8 {
			t.Fatalf("iteration %d: expected length 8, got %d (username=%q)", i, len(u), u)
		}
	}
}

func TestRandomUsername_Alphanumeric(t *testing.T) {
	r := newDeterministicRand(13)
	for i := 0; i < 100; i++ {
		u := randomUsername(r)
		if !alphanumericRe.MatchString(u) {
			t.Fatalf("iteration %d: username %q contains non-alphanumeric characters", i, u)
		}
	}
}

func TestRandomUsername_LowercaseOnly(t *testing.T) {
	r := newDeterministicRand(21)
	for i := 0; i < 100; i++ {
		u := randomUsername(r)
		if u != strings.ToLower(u) {
			t.Fatalf("iteration %d: username %q contains uppercase characters", i, u)
		}
	}
}

func TestRandomUsername_NonEmpty(t *testing.T) {
	r := newDeterministicRand(0)
	for i := 0; i < 50; i++ {
		u := randomUsername(r)
		if u == "" {
			t.Fatalf("iteration %d: randomUsername returned empty string", i)
		}
	}
}

func TestRandomUsername_Deterministic(t *testing.T) {
	seed := int64(777)
	u1 := randomUsername(newDeterministicRand(seed))
	u2 := randomUsername(newDeterministicRand(seed))
	if u1 != u2 {
		t.Fatalf("same seed produced different results: %q vs %q", u1, u2)
	}
}

func TestRandomUsername_DifferentSeeds(t *testing.T) {
	// With very high probability two different seeds should produce different
	// usernames (charset^8 = 36^8 ≈ 2.8 trillion combinations).
	u1 := randomUsername(newDeterministicRand(1))
	u2 := randomUsername(newDeterministicRand(2))
	if u1 == u2 {
		// This is astronomically unlikely; fail to surface a bug if it happens.
		t.Fatalf("different seeds produced identical username %q", u1)
	}
}

// ---------------------------------------------------------------------------
// randomQuestId
// ---------------------------------------------------------------------------

func TestRandomQuestId_NonEmpty(t *testing.T) {
	r := newDeterministicRand(42)
	for i := 0; i < 50; i++ {
		id := randomQuestId(r)
		if id == "" {
			t.Fatalf("iteration %d: randomQuestId returned empty string", i)
		}
	}
}

func TestRandomQuestId_HasPrefix(t *testing.T) {
	r := newDeterministicRand(5)
	for i := 0; i < 50; i++ {
		id := randomQuestId(r)
		if !strings.HasPrefix(id, "quest_") {
			t.Fatalf("iteration %d: quest ID %q does not have 'quest_' prefix", i, id)
		}
	}
}

func TestRandomQuestId_NumericSuffix(t *testing.T) {
	numericRe := regexp.MustCompile(`^quest_\d+$`)
	r := newDeterministicRand(6)
	for i := 0; i < 50; i++ {
		id := randomQuestId(r)
		if !numericRe.MatchString(id) {
			t.Fatalf("iteration %d: quest ID %q does not match expected pattern", i, id)
		}
	}
}

func TestRandomQuestId_Deterministic(t *testing.T) {
	seed := int64(111)
	id1 := randomQuestId(newDeterministicRand(seed))
	id2 := randomQuestId(newDeterministicRand(seed))
	if id1 != id2 {
		t.Fatalf("same seed produced different results: %q vs %q", id1, id2)
	}
}

// ---------------------------------------------------------------------------
// randomTitleId
// ---------------------------------------------------------------------------

func TestRandomTitleId_NonEmpty(t *testing.T) {
	r := newDeterministicRand(42)
	for i := 0; i < 50; i++ {
		id := randomTitleId(r)
		if id == "" {
			t.Fatalf("iteration %d: randomTitleId returned empty string", i)
		}
	}
}

func TestRandomTitleId_HasPrefix(t *testing.T) {
	r := newDeterministicRand(8)
	for i := 0; i < 50; i++ {
		id := randomTitleId(r)
		if !strings.HasPrefix(id, "title_") {
			t.Fatalf("iteration %d: title ID %q does not have 'title_' prefix", i, id)
		}
	}
}

func TestRandomTitleId_NumericSuffix(t *testing.T) {
	numericRe := regexp.MustCompile(`^title_\d+$`)
	r := newDeterministicRand(9)
	for i := 0; i < 50; i++ {
		id := randomTitleId(r)
		if !numericRe.MatchString(id) {
			t.Fatalf("iteration %d: title ID %q does not match expected pattern", i, id)
		}
	}
}

func TestRandomTitleId_Deterministic(t *testing.T) {
	seed := int64(222)
	id1 := randomTitleId(newDeterministicRand(seed))
	id2 := randomTitleId(newDeterministicRand(seed))
	if id1 != id2 {
		t.Fatalf("same seed produced different results: %q vs %q", id1, id2)
	}
}

// ---------------------------------------------------------------------------
// randomAchievementId
// ---------------------------------------------------------------------------

func TestRandomAchievementId_NonEmpty(t *testing.T) {
	r := newDeterministicRand(42)
	for i := 0; i < 50; i++ {
		id := randomAchievementId(r)
		if id == "" {
			t.Fatalf("iteration %d: randomAchievementId returned empty string", i)
		}
	}
}

func TestRandomAchievementId_HasPrefix(t *testing.T) {
	r := newDeterministicRand(3)
	for i := 0; i < 50; i++ {
		id := randomAchievementId(r)
		if !strings.HasPrefix(id, "achievement_") {
			t.Fatalf("iteration %d: achievement ID %q does not have 'achievement_' prefix", i, id)
		}
	}
}

func TestRandomAchievementId_NumericSuffix(t *testing.T) {
	numericRe := regexp.MustCompile(`^achievement_\d+$`)
	r := newDeterministicRand(4)
	for i := 0; i < 50; i++ {
		id := randomAchievementId(r)
		if !numericRe.MatchString(id) {
			t.Fatalf("iteration %d: achievement ID %q does not match expected pattern", i, id)
		}
	}
}

func TestRandomAchievementId_Deterministic(t *testing.T) {
	seed := int64(333)
	id1 := randomAchievementId(newDeterministicRand(seed))
	id2 := randomAchievementId(newDeterministicRand(seed))
	if id1 != id2 {
		t.Fatalf("same seed produced different results: %q vs %q", id1, id2)
	}
}

// ---------------------------------------------------------------------------
// getAccountForAddress
// ---------------------------------------------------------------------------

// buildAccounts returns n deterministically-generated simtypes.Account values.
func buildAccounts(n int) []simtypes.Account {
	r := newDeterministicRand(12345)
	return simtypes.RandomAccounts(r, n)
}

func TestGetAccountForAddress_EmptySlice(t *testing.T) {
	_, found := getAccountForAddress("cosmos1anything", []simtypes.Account{})
	if found {
		t.Fatal("expected found=false for empty accounts slice")
	}
}

func TestGetAccountForAddress_MatchesFirst(t *testing.T) {
	accs := buildAccounts(5)
	target := accs[0]
	addr := target.Address.String()

	got, found := getAccountForAddress(addr, accs)
	if !found {
		t.Fatalf("expected to find account with address %s", addr)
	}
	if !got.Address.Equals(target.Address) {
		t.Fatalf("returned wrong account: got %s, want %s", got.Address, target.Address)
	}
}

func TestGetAccountForAddress_MatchesLast(t *testing.T) {
	accs := buildAccounts(5)
	target := accs[len(accs)-1]
	addr := target.Address.String()

	got, found := getAccountForAddress(addr, accs)
	if !found {
		t.Fatalf("expected to find account with address %s", addr)
	}
	if !got.Address.Equals(target.Address) {
		t.Fatalf("returned wrong account: got %s, want %s", got.Address, target.Address)
	}
}

func TestGetAccountForAddress_MatchesMiddle(t *testing.T) {
	accs := buildAccounts(9)
	target := accs[4]
	addr := target.Address.String()

	got, found := getAccountForAddress(addr, accs)
	if !found {
		t.Fatalf("expected to find account with address %s", addr)
	}
	if !got.Address.Equals(target.Address) {
		t.Fatalf("returned wrong account: got %s, want %s", got.Address, target.Address)
	}
}

func TestGetAccountForAddress_NoMatch(t *testing.T) {
	accs := buildAccounts(5)
	// Build an address that is definitely not in the slice.
	outsiderKey := secp256k1.GenPrivKeyFromSecret([]byte("outsider-key-that-is-not-in-slice"))
	outsiderAddr := sdk.AccAddress(outsiderKey.PubKey().Address()).String()

	_, found := getAccountForAddress(outsiderAddr, accs)
	if found {
		t.Fatalf("expected found=false for address %s not in accounts slice", outsiderAddr)
	}
}

func TestGetAccountForAddress_ReturnsEmptyAccountOnMiss(t *testing.T) {
	accs := buildAccounts(3)
	outsiderKey := secp256k1.GenPrivKeyFromSecret([]byte("another-outsider-seed-xyz"))
	outsiderAddr := sdk.AccAddress(outsiderKey.PubKey().Address()).String()

	acc, found := getAccountForAddress(outsiderAddr, accs)
	if found {
		t.Fatal("expected found=false")
	}
	// The returned Account should be a zero value (empty address).
	if acc.Address != nil {
		t.Fatalf("expected zero-value Account on miss, got Address=%s", acc.Address)
	}
}

func TestGetAccountForAddress_TableDriven(t *testing.T) {
	accs := buildAccounts(6)

	tests := []struct {
		name      string
		addr      string
		wantFound bool
	}{
		{
			name:      "first account found",
			addr:      accs[0].Address.String(),
			wantFound: true,
		},
		{
			name:      "third account found",
			addr:      accs[2].Address.String(),
			wantFound: true,
		},
		{
			name:      "last account found",
			addr:      accs[5].Address.String(),
			wantFound: true,
		},
		{
			name:      "unknown address not found",
			addr:      sdk.AccAddress(secp256k1.GenPrivKeyFromSecret([]byte("table-driven-outsider")).PubKey().Address()).String(),
			wantFound: false,
		},
		{
			name:      "empty string not found",
			addr:      "",
			wantFound: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, found := getAccountForAddress(tc.addr, accs)
			if found != tc.wantFound {
				t.Fatalf("found=%v, wantFound=%v (addr=%q)", found, tc.wantFound, tc.addr)
			}
			if tc.wantFound && got.Address.String() != tc.addr {
				t.Fatalf("returned account address %s does not match requested %s", got.Address, tc.addr)
			}
			if !tc.wantFound && got.Address != nil {
				t.Fatalf("expected zero-value Account on miss, got non-nil Address=%s", got.Address)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Cross-function consistency: random ID functions produce distinct namespaces
// ---------------------------------------------------------------------------

func TestRandomIds_DistinctNamespaces(t *testing.T) {
	r := newDeterministicRand(500)
	questID := randomQuestId(r)
	titleID := randomTitleId(r)
	achievementID := randomAchievementId(r)

	if questID == titleID {
		t.Fatalf("quest ID and title ID collided: %q", questID)
	}
	if questID == achievementID {
		t.Fatalf("quest ID and achievement ID collided: %q", questID)
	}
	if titleID == achievementID {
		t.Fatalf("title ID and achievement ID collided: %q", titleID)
	}
}
