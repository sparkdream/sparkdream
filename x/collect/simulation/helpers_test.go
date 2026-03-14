package simulation

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
	commontypes "sparkdream/x/common/types"
)

// makeTestAccounts builds n simtypes.Account values using a deterministic rand.
func makeTestAccounts(r *rand.Rand, n int) []simtypes.Account {
	accs := make([]simtypes.Account, n)
	for i := range accs {
		seed := make([]byte, 15)
		if _, err := r.Read(seed); err != nil {
			panic(err)
		}
		privKey := secp256k1.GenPrivKeyFromSecret(seed)
		pubKey := privKey.PubKey()
		addr := sdk.AccAddress(pubKey.Address())
		accs[i] = simtypes.Account{
			PrivKey: privKey,
			PubKey:  pubKey,
			Address: addr,
		}
	}
	return accs
}

// --- getAccountForAddress ---

func TestGetAccountForAddress_Found(t *testing.T) {
	r := rand.New(rand.NewSource(42))
	accs := makeTestAccounts(r, 5)

	// Each account in the slice should be findable by its own address string.
	for _, want := range accs {
		got, ok := getAccountForAddress(want.Address.String(), accs)
		require.True(t, ok, "expected account to be found for address %s", want.Address)
		require.Equal(t, want.Address.String(), got.Address.String())
	}
}

func TestGetAccountForAddress_NotFound(t *testing.T) {
	r := rand.New(rand.NewSource(42))
	accs := makeTestAccounts(r, 3)

	// Generate an address that is guaranteed not to be in the slice.
	seed := make([]byte, 15)
	_, _ = r.Read(seed)
	outsider := sdk.AccAddress(secp256k1.GenPrivKeyFromSecret(seed).PubKey().Address())

	got, ok := getAccountForAddress(outsider.String(), accs)
	require.False(t, ok)
	require.Empty(t, got.Address)
}

func TestGetAccountForAddress_EmptySlice(t *testing.T) {
	got, ok := getAccountForAddress("anyaddress", []simtypes.Account{})
	require.False(t, ok)
	require.Empty(t, got.Address)
}

func TestGetAccountForAddress_TableDriven(t *testing.T) {
	r := rand.New(rand.NewSource(99))
	accs := makeTestAccounts(r, 4)

	tests := []struct {
		name      string
		addr      string
		wantFound bool
	}{
		{
			name:      "first account",
			addr:      accs[0].Address.String(),
			wantFound: true,
		},
		{
			name:      "last account",
			addr:      accs[3].Address.String(),
			wantFound: true,
		},
		{
			name:      "empty string address",
			addr:      "",
			wantFound: false,
		},
		{
			name:      "garbage address",
			addr:      "cosmos1notarealaddress",
			wantFound: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := getAccountForAddress(tc.addr, accs)
			require.Equal(t, tc.wantFound, ok)
			if tc.wantFound {
				require.Equal(t, tc.addr, got.Address.String())
			} else {
				require.Empty(t, got.Address)
			}
		})
	}
}

// --- pickDifferentAccount ---

func TestPickDifferentAccount_ExcludesTarget(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	accs := makeTestAccounts(r, 10)
	exclude := accs[0].Address.String()

	// Run many iterations to confirm we never get the excluded account back.
	for i := 0; i < 200; i++ {
		got, ok := pickDifferentAccount(r, accs, exclude)
		require.True(t, ok)
		require.NotEqual(t, exclude, got.Address.String(),
			"iteration %d returned the excluded account", i)
	}
}

func TestPickDifferentAccount_SingleAccount_ReturnsFalse(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	accs := makeTestAccounts(r, 1)

	got, ok := pickDifferentAccount(r, accs, accs[0].Address.String())
	require.False(t, ok)
	require.Empty(t, got.Address)
}

func TestPickDifferentAccount_EmptySlice_ReturnsFalse(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	got, ok := pickDifferentAccount(r, []simtypes.Account{}, "anyaddr")
	require.False(t, ok)
	require.Empty(t, got.Address)
}

func TestPickDifferentAccount_AllExcluded_ReturnsFalse(t *testing.T) {
	// Two identical address entries (unusual but tests the filtered-empty path).
	r := rand.New(rand.NewSource(2))
	base := makeTestAccounts(r, 1)
	accs := []simtypes.Account{base[0], base[0]}
	exclude := base[0].Address.String()

	got, ok := pickDifferentAccount(r, accs, exclude)
	require.False(t, ok)
	require.Empty(t, got.Address)
}

func TestPickDifferentAccount_TwoAccounts_AlwaysReturnsOther(t *testing.T) {
	r := rand.New(rand.NewSource(55))
	accs := makeTestAccounts(r, 2)

	for i := 0; i < 50; i++ {
		got, ok := pickDifferentAccount(r, accs, accs[0].Address.String())
		require.True(t, ok)
		require.Equal(t, accs[1].Address.String(), got.Address.String())
	}
}

// --- randomCollectionName ---

var validCollectionNames = []string{
	"phoenix-gallery", "aurora-vault", "zenith-archive", "nebula-catalog",
	"prism-set", "vortex-shelf", "cascade-library", "ember-trove",
	"atlas-digest", "horizon-collection", "pulse-registry", "forge-index",
}

func TestRandomCollectionName_NonEmpty(t *testing.T) {
	r := rand.New(rand.NewSource(11))
	for i := 0; i < 50; i++ {
		name := randomCollectionName(r)
		require.NotEmpty(t, name)
	}
}

func TestRandomCollectionName_ValidValues(t *testing.T) {
	r := rand.New(rand.NewSource(22))
	nameSet := make(map[string]bool, len(validCollectionNames))
	for _, n := range validCollectionNames {
		nameSet[n] = true
	}
	for i := 0; i < 200; i++ {
		name := randomCollectionName(r)
		require.True(t, nameSet[name], "unexpected collection name %q", name)
	}
}

func TestRandomCollectionName_Deterministic(t *testing.T) {
	// Same seed → same sequence.
	r1 := rand.New(rand.NewSource(33))
	r2 := rand.New(rand.NewSource(33))
	for i := 0; i < 20; i++ {
		require.Equal(t, randomCollectionName(r1), randomCollectionName(r2))
	}
}

// --- randomTitle ---

var validTitles = []string{
	"sunrise-entry", "moonlight-piece", "starfall-item", "dewdrop-record",
	"thunder-artifact", "coral-fragment", "crystal-shard", "amber-relic",
}

func TestRandomTitle_NonEmpty(t *testing.T) {
	r := rand.New(rand.NewSource(44))
	for i := 0; i < 50; i++ {
		title := randomTitle(r)
		require.NotEmpty(t, title)
	}
}

func TestRandomTitle_ValidValues(t *testing.T) {
	r := rand.New(rand.NewSource(55))
	titleSet := make(map[string]bool, len(validTitles))
	for _, v := range validTitles {
		titleSet[v] = true
	}
	for i := 0; i < 200; i++ {
		title := randomTitle(r)
		require.True(t, titleSet[title], "unexpected title %q", title)
	}
}

func TestRandomTitle_Deterministic(t *testing.T) {
	r1 := rand.New(rand.NewSource(66))
	r2 := rand.New(rand.NewSource(66))
	for i := 0; i < 20; i++ {
		require.Equal(t, randomTitle(r1), randomTitle(r2))
	}
}

// --- randomDescription ---

var validDescriptions = []string{
	"A simulation-generated entry for testing",
	"Sample content created during chain simulation",
	"Auto-generated description for sim coverage",
	"Test description from simulation framework",
}

func TestRandomDescription_NonEmpty(t *testing.T) {
	r := rand.New(rand.NewSource(77))
	for i := 0; i < 50; i++ {
		desc := randomDescription(r)
		require.NotEmpty(t, desc)
	}
}

func TestRandomDescription_ValidValues(t *testing.T) {
	r := rand.New(rand.NewSource(88))
	descSet := make(map[string]bool, len(validDescriptions))
	for _, d := range validDescriptions {
		descSet[d] = true
	}
	for i := 0; i < 200; i++ {
		desc := randomDescription(r)
		require.True(t, descSet[desc], "unexpected description %q", desc)
	}
}

func TestRandomDescription_Deterministic(t *testing.T) {
	r1 := rand.New(rand.NewSource(99))
	r2 := rand.New(rand.NewSource(99))
	for i := 0; i < 20; i++ {
		require.Equal(t, randomDescription(r1), randomDescription(r2))
	}
}

// --- randomTag ---

var validTags = []string{
	"art", "science", "history", "tech", "nature",
	"music", "code", "design", "research", "education",
}

func TestRandomTag_NonEmpty(t *testing.T) {
	r := rand.New(rand.NewSource(111))
	for i := 0; i < 50; i++ {
		tag := randomTag(r)
		require.NotEmpty(t, tag)
	}
}

func TestRandomTag_ValidValues(t *testing.T) {
	r := rand.New(rand.NewSource(222))
	tagSet := make(map[string]bool, len(validTags))
	for _, v := range validTags {
		tagSet[v] = true
	}
	for i := 0; i < 200; i++ {
		tag := randomTag(r)
		require.True(t, tagSet[tag], "unexpected tag %q", tag)
	}
}

func TestRandomTag_Deterministic(t *testing.T) {
	r1 := rand.New(rand.NewSource(333))
	r2 := rand.New(rand.NewSource(333))
	for i := 0; i < 20; i++ {
		require.Equal(t, randomTag(r1), randomTag(r2))
	}
}

// --- randomURI ---

func TestRandomURI_HasIPFSPrefix(t *testing.T) {
	r := rand.New(rand.NewSource(444))
	for i := 0; i < 50; i++ {
		uri := randomURI(r)
		require.True(t, strings.HasPrefix(uri, "ipfs://Qm"),
			"URI %q does not start with 'ipfs://Qm'", uri)
	}
}

func TestRandomURI_SufficientLength(t *testing.T) {
	r := rand.New(rand.NewSource(555))
	// "ipfs://Qm" (9 chars) + 40 random chars = 49 total minimum.
	for i := 0; i < 50; i++ {
		uri := randomURI(r)
		require.GreaterOrEqual(t, len(uri), 49,
			"URI %q is shorter than expected", uri)
	}
}

func TestRandomURI_NonEmpty(t *testing.T) {
	r := rand.New(rand.NewSource(666))
	for i := 0; i < 50; i++ {
		require.NotEmpty(t, randomURI(r))
	}
}

func TestRandomURI_Deterministic(t *testing.T) {
	r1 := rand.New(rand.NewSource(777))
	r2 := rand.New(rand.NewSource(777))
	for i := 0; i < 20; i++ {
		require.Equal(t, randomURI(r1), randomURI(r2))
	}
}

// --- randomCollectionType ---

var validCollectionTypes = map[types.CollectionType]bool{
	types.CollectionType_COLLECTION_TYPE_NFT:     true,
	types.CollectionType_COLLECTION_TYPE_LINK:    true,
	types.CollectionType_COLLECTION_TYPE_ONCHAIN: true,
	types.CollectionType_COLLECTION_TYPE_MIXED:   true,
}

func TestRandomCollectionType_NonZero(t *testing.T) {
	r := rand.New(rand.NewSource(888))
	for i := 0; i < 50; i++ {
		ct := randomCollectionType(r)
		require.NotEqual(t, types.CollectionType_COLLECTION_TYPE_UNSPECIFIED, ct,
			"iteration %d returned UNSPECIFIED", i)
	}
}

func TestRandomCollectionType_ValidValues(t *testing.T) {
	r := rand.New(rand.NewSource(999))
	for i := 0; i < 200; i++ {
		ct := randomCollectionType(r)
		require.True(t, validCollectionTypes[ct],
			"unexpected collection type %v", ct)
	}
}

func TestRandomCollectionType_CoverageAcrossSeeds(t *testing.T) {
	// Confirm that all four valid types can be produced given enough iterations.
	r := rand.New(rand.NewSource(1234))
	seen := make(map[types.CollectionType]bool)
	for i := 0; i < 500; i++ {
		seen[randomCollectionType(r)] = true
	}
	for ct := range validCollectionTypes {
		require.True(t, seen[ct], "collection type %v was never produced", ct)
	}
}

func TestRandomCollectionType_Deterministic(t *testing.T) {
	r1 := rand.New(rand.NewSource(4321))
	r2 := rand.New(rand.NewSource(4321))
	for i := 0; i < 20; i++ {
		require.Equal(t, randomCollectionType(r1), randomCollectionType(r2))
	}
}

// --- randomModerationReason ---

var validModerationReasons = map[commontypes.ModerationReason]bool{
	commontypes.ModerationReason_MODERATION_REASON_SPAM:             true,
	commontypes.ModerationReason_MODERATION_REASON_HARASSMENT:       true,
	commontypes.ModerationReason_MODERATION_REASON_MISINFORMATION:   true,
	commontypes.ModerationReason_MODERATION_REASON_INAPPROPRIATE:    true,
	commontypes.ModerationReason_MODERATION_REASON_POLICY_VIOLATION: true,
}

func TestRandomModerationReason_NonZero(t *testing.T) {
	r := rand.New(rand.NewSource(10))
	for i := 0; i < 50; i++ {
		reason := randomModerationReason(r)
		require.NotEqual(t, commontypes.ModerationReason_MODERATION_REASON_UNSPECIFIED, reason,
			"iteration %d returned UNSPECIFIED", i)
	}
}

func TestRandomModerationReason_ValidValues(t *testing.T) {
	r := rand.New(rand.NewSource(20))
	for i := 0; i < 200; i++ {
		reason := randomModerationReason(r)
		require.True(t, validModerationReasons[reason],
			"unexpected moderation reason %v", reason)
	}
}

func TestRandomModerationReason_CoverageAcrossSeeds(t *testing.T) {
	r := rand.New(rand.NewSource(30))
	seen := make(map[commontypes.ModerationReason]bool)
	for i := 0; i < 500; i++ {
		seen[randomModerationReason(r)] = true
	}
	for reason := range validModerationReasons {
		require.True(t, seen[reason], "moderation reason %v was never produced", reason)
	}
}

func TestRandomModerationReason_Deterministic(t *testing.T) {
	r1 := rand.New(rand.NewSource(40))
	r2 := rand.New(rand.NewSource(40))
	for i := 0; i < 20; i++ {
		require.Equal(t, randomModerationReason(r1), randomModerationReason(r2))
	}
}

// --- randomCurationVerdict ---

func TestRandomCurationVerdict_OnlyValidValues(t *testing.T) {
	r := rand.New(rand.NewSource(50))
	for i := 0; i < 200; i++ {
		verdict := randomCurationVerdict(r)
		require.True(t,
			verdict == types.CurationVerdict_CURATION_VERDICT_UP ||
				verdict == types.CurationVerdict_CURATION_VERDICT_DOWN,
			"unexpected verdict %v at iteration %d", verdict, i)
	}
}

func TestRandomCurationVerdict_NonZero(t *testing.T) {
	r := rand.New(rand.NewSource(60))
	for i := 0; i < 50; i++ {
		verdict := randomCurationVerdict(r)
		require.NotEqual(t, types.CurationVerdict_CURATION_VERDICT_UNSPECIFIED, verdict,
			"iteration %d returned UNSPECIFIED", i)
	}
}

func TestRandomCurationVerdict_BothValuesProduced(t *testing.T) {
	r := rand.New(rand.NewSource(70))
	seenUp := false
	seenDown := false
	for i := 0; i < 200; i++ {
		verdict := randomCurationVerdict(r)
		if verdict == types.CurationVerdict_CURATION_VERDICT_UP {
			seenUp = true
		}
		if verdict == types.CurationVerdict_CURATION_VERDICT_DOWN {
			seenDown = true
		}
		if seenUp && seenDown {
			break
		}
	}
	require.True(t, seenUp, "CURATION_VERDICT_UP was never produced")
	require.True(t, seenDown, "CURATION_VERDICT_DOWN was never produced")
}

func TestRandomCurationVerdict_Deterministic(t *testing.T) {
	r1 := rand.New(rand.NewSource(80))
	r2 := rand.New(rand.NewSource(80))
	for i := 0; i < 20; i++ {
		require.Equal(t, randomCurationVerdict(r1), randomCurationVerdict(r2))
	}
}
