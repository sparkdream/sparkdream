package simulation

import (
	"math/rand"
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/stretchr/testify/require"
)

// newDeterministicRand returns a *rand.Rand seeded with a fixed value so that
// tests are reproducible across runs.
func newDeterministicRand(seed int64) *rand.Rand {
	return rand.New(rand.NewSource(seed))
}

// newSimAccount constructs a minimal simtypes.Account with a real secp256k1
// private key so that acc.Address.String() returns a valid bech32 address.
func newSimAccount(seed byte) simtypes.Account {
	privKey := secp256k1.GenPrivKeyFromSecret([]byte{seed})
	pubKey := privKey.PubKey()
	addr := sdk.AccAddress(pubKey.Address())
	return simtypes.Account{
		PrivKey: privKey,
		PubKey:  pubKey,
		Address: addr,
	}
}

// ---------------------------------------------------------------------------
// randomZKPublicKey
// ---------------------------------------------------------------------------

func TestRandomZKPublicKey_Length(t *testing.T) {
	r := newDeterministicRand(42)
	key := randomZKPublicKey(r)
	require.Len(t, key, 32, "randomZKPublicKey must return exactly 32 bytes")
}

func TestRandomZKPublicKey_NotAllZero(t *testing.T) {
	// With a fixed seed the output is deterministic and extremely unlikely to be
	// all-zero; this guards against an implementation that always returns zeros.
	r := newDeterministicRand(42)
	key := randomZKPublicKey(r)
	allZero := true
	for _, b := range key {
		if b != 0 {
			allZero = false
			break
		}
	}
	require.False(t, allZero, "randomZKPublicKey should not return an all-zero slice")
}

func TestRandomZKPublicKey_DifferentSeeds(t *testing.T) {
	key1 := randomZKPublicKey(newDeterministicRand(1))
	key2 := randomZKPublicKey(newDeterministicRand(2))
	require.NotEqual(t, key1, key2, "different seeds should produce different keys")
}

func TestRandomZKPublicKey_SameSeedDeterministic(t *testing.T) {
	key1 := randomZKPublicKey(newDeterministicRand(99))
	key2 := randomZKPublicKey(newDeterministicRand(99))
	require.Equal(t, key1, key2, "same seed must produce the same key")
}

// ---------------------------------------------------------------------------
// randomNullifier
// ---------------------------------------------------------------------------

func TestRandomNullifier_Length(t *testing.T) {
	r := newDeterministicRand(42)
	n := randomNullifier(r)
	require.Len(t, n, 32, "randomNullifier must return exactly 32 bytes")
}

func TestRandomNullifier_SameSeedDeterministic(t *testing.T) {
	n1 := randomNullifier(newDeterministicRand(7))
	n2 := randomNullifier(newDeterministicRand(7))
	require.Equal(t, n1, n2, "same seed must produce the same nullifier")
}

// randomNullifier delegates to randomZKPublicKey, so the two calls with the
// same seed and the same rand state must yield the same bytes.
func TestRandomNullifier_EquivalentToZKPublicKey(t *testing.T) {
	seed := int64(55)
	r1 := newDeterministicRand(seed)
	r2 := newDeterministicRand(seed)
	require.Equal(t, randomZKPublicKey(r1), randomNullifier(r2),
		"randomNullifier and randomZKPublicKey must consume rand identically")
}

// ---------------------------------------------------------------------------
// randomProof
// ---------------------------------------------------------------------------

func TestRandomProof_Length(t *testing.T) {
	r := newDeterministicRand(42)
	proof := randomProof(r)
	require.Len(t, proof, 192, "randomProof must return exactly 192 bytes")
}

func TestRandomProof_SameSeedDeterministic(t *testing.T) {
	p1 := randomProof(newDeterministicRand(13))
	p2 := randomProof(newDeterministicRand(13))
	require.Equal(t, p1, p2, "same seed must produce the same proof")
}

func TestRandomProof_DifferentSeeds(t *testing.T) {
	p1 := randomProof(newDeterministicRand(100))
	p2 := randomProof(newDeterministicRand(200))
	require.NotEqual(t, p1, p2, "different seeds should produce different proofs")
}

// ---------------------------------------------------------------------------
// randomVoteCommitment
// ---------------------------------------------------------------------------

func TestRandomVoteCommitment_Length(t *testing.T) {
	r := newDeterministicRand(42)
	c := randomVoteCommitment(r)
	require.Len(t, c, 32, "randomVoteCommitment must return exactly 32 bytes")
}

func TestRandomVoteCommitment_SameSeedDeterministic(t *testing.T) {
	c1 := randomVoteCommitment(newDeterministicRand(21))
	c2 := randomVoteCommitment(newDeterministicRand(21))
	require.Equal(t, c1, c2, "same seed must produce the same commitment")
}

// randomVoteCommitment delegates to randomZKPublicKey, so the two calls must
// produce the same bytes when seeded identically.
func TestRandomVoteCommitment_EquivalentToZKPublicKey(t *testing.T) {
	seed := int64(77)
	r1 := newDeterministicRand(seed)
	r2 := newDeterministicRand(seed)
	require.Equal(t, randomZKPublicKey(r1), randomVoteCommitment(r2),
		"randomVoteCommitment and randomZKPublicKey must consume rand identically")
}

// ---------------------------------------------------------------------------
// randomProposalTitle
// ---------------------------------------------------------------------------

var knownTitles = []string{
	"Parameter Update Proposal",
	"Budget Allocation Decision",
	"Council Election Vote",
	"Community Initiative Review",
	"Governance Policy Change",
	"Resource Distribution Plan",
	"Technical Upgrade Approval",
	"Membership Policy Update",
	"Treasury Spending Proposal",
	"Protocol Enhancement Review",
}

func TestRandomProposalTitle_NonEmpty(t *testing.T) {
	r := newDeterministicRand(42)
	title := randomProposalTitle(r)
	require.NotEmpty(t, title, "randomProposalTitle must return a non-empty string")
}

func TestRandomProposalTitle_InKnownList(t *testing.T) {
	r := newDeterministicRand(42)
	// Sample many times to confirm every result is from the expected pool.
	for i := 0; i < 100; i++ {
		title := randomProposalTitle(r)
		require.Contains(t, knownTitles, title,
			"randomProposalTitle returned a value not in the known list: %q", title)
	}
}

func TestRandomProposalTitle_SameSeedDeterministic(t *testing.T) {
	t1 := randomProposalTitle(newDeterministicRand(8))
	t2 := randomProposalTitle(newDeterministicRand(8))
	require.Equal(t, t1, t2, "same seed must produce the same title")
}

// ---------------------------------------------------------------------------
// randomProposalDescription
// ---------------------------------------------------------------------------

var knownDescriptions = []string{
	"This proposal seeks to adjust system parameters for improved performance.",
	"A budget allocation request for the upcoming development cycle.",
	"Electing new council members for the governance structure.",
	"Review of community-driven initiative for approval.",
	"Proposed changes to governance policies and procedures.",
	"Plan for equitable distribution of community resources.",
}

func TestRandomProposalDescription_NonEmpty(t *testing.T) {
	r := newDeterministicRand(42)
	desc := randomProposalDescription(r)
	require.NotEmpty(t, desc, "randomProposalDescription must return a non-empty string")
}

func TestRandomProposalDescription_InKnownList(t *testing.T) {
	r := newDeterministicRand(42)
	for i := 0; i < 100; i++ {
		desc := randomProposalDescription(r)
		require.Contains(t, knownDescriptions, desc,
			"randomProposalDescription returned a value not in the known list: %q", desc)
	}
}

func TestRandomProposalDescription_SameSeedDeterministic(t *testing.T) {
	d1 := randomProposalDescription(newDeterministicRand(9))
	d2 := randomProposalDescription(newDeterministicRand(9))
	require.Equal(t, d1, d2, "same seed must produce the same description")
}

// ---------------------------------------------------------------------------
// randomVoteOptions
// ---------------------------------------------------------------------------

func TestRandomVoteOptions_Count(t *testing.T) {
	opts := randomVoteOptions()
	require.Len(t, opts, 3, "randomVoteOptions must return exactly 3 options")
}

func TestRandomVoteOptions_IDs(t *testing.T) {
	tests := []struct {
		idx        int
		expectedID uint32
	}{
		{0, 0},
		{1, 1},
		{2, 2},
	}
	opts := randomVoteOptions()
	for _, tc := range tests {
		t.Run("", func(t *testing.T) {
			require.Equal(t, tc.expectedID, opts[tc.idx].Id,
				"option at index %d must have ID %d", tc.idx, tc.expectedID)
		})
	}
}

func TestRandomVoteOptions_Labels(t *testing.T) {
	tests := []struct {
		idx           int
		expectedLabel string
	}{
		{0, "yes"},
		{1, "no"},
		{2, "abstain"},
	}
	opts := randomVoteOptions()
	for _, tc := range tests {
		t.Run(tc.expectedLabel, func(t *testing.T) {
			require.Equal(t, tc.expectedLabel, opts[tc.idx].Label,
				"option at index %d must have label %q", tc.idx, tc.expectedLabel)
		})
	}
}

func TestRandomVoteOptions_NoNilElements(t *testing.T) {
	opts := randomVoteOptions()
	for i, opt := range opts {
		require.NotNil(t, opt, "option at index %d must not be nil", i)
	}
}

func TestRandomVoteOptions_Idempotent(t *testing.T) {
	// Each call should return an equivalent slice (no shared mutation).
	opts1 := randomVoteOptions()
	opts2 := randomVoteOptions()
	require.Len(t, opts1, len(opts2))
	for i := range opts1 {
		require.Equal(t, opts1[i].Id, opts2[i].Id)
		require.Equal(t, opts1[i].Label, opts2[i].Label)
	}
}

// ---------------------------------------------------------------------------
// pickDifferentAccount
// ---------------------------------------------------------------------------

func TestPickDifferentAccount_ExcludesSpecified(t *testing.T) {
	accs := []simtypes.Account{
		newSimAccount(1),
		newSimAccount(2),
		newSimAccount(3),
	}
	excluded := accs[0].Address.String()
	r := newDeterministicRand(42)

	// Sample many times; the excluded address must never appear.
	for i := 0; i < 50; i++ {
		acc, ok := pickDifferentAccount(r, accs, excluded)
		require.True(t, ok, "should find a different account when others are available")
		require.NotEqual(t, excluded, acc.Address.String(),
			"returned account must not equal the excluded address")
	}
}

func TestPickDifferentAccount_SingleAccountExcluded_ReturnsFalse(t *testing.T) {
	acc := newSimAccount(1)
	accs := []simtypes.Account{acc}
	excluded := acc.Address.String()
	r := newDeterministicRand(42)

	_, ok := pickDifferentAccount(r, accs, excluded)
	require.False(t, ok, "should return false when the only account is excluded")
}

func TestPickDifferentAccount_EmptySlice_ReturnsFalse(t *testing.T) {
	r := newDeterministicRand(42)
	_, ok := pickDifferentAccount(r, []simtypes.Account{}, "anything")
	require.False(t, ok, "should return false for an empty account slice")
}

func TestPickDifferentAccount_TwoAccounts_AlwaysReturnsOther(t *testing.T) {
	acc1 := newSimAccount(10)
	acc2 := newSimAccount(20)
	accs := []simtypes.Account{acc1, acc2}
	excluded := acc1.Address.String()
	r := newDeterministicRand(42)

	for i := 0; i < 20; i++ {
		acc, ok := pickDifferentAccount(r, accs, excluded)
		require.True(t, ok)
		require.Equal(t, acc2.Address.String(), acc.Address.String(),
			"with two accounts the non-excluded one must always be returned")
	}
}

func TestPickDifferentAccount_ExcludeNotPresent(t *testing.T) {
	// When the excluded address is not in the slice at all, any account may be
	// returned.
	accs := []simtypes.Account{
		newSimAccount(1),
		newSimAccount(2),
	}
	r := newDeterministicRand(42)

	acc, ok := pickDifferentAccount(r, accs, "cosmos1notinthelist")
	require.True(t, ok)
	// The result must still be one of the accounts in the slice.
	found := false
	for _, a := range accs {
		if a.Address.String() == acc.Address.String() {
			found = true
			break
		}
	}
	require.True(t, found, "returned account must be from the provided slice")
}

func TestPickDifferentAccount_Deterministic(t *testing.T) {
	accs := []simtypes.Account{
		newSimAccount(1),
		newSimAccount(2),
		newSimAccount(3),
	}
	excluded := accs[0].Address.String()

	acc1, ok1 := pickDifferentAccount(newDeterministicRand(77), accs, excluded)
	acc2, ok2 := pickDifferentAccount(newDeterministicRand(77), accs, excluded)

	require.True(t, ok1)
	require.True(t, ok2)
	require.Equal(t, acc1.Address.String(), acc2.Address.String(),
		"same seed must produce the same selected account")
}
