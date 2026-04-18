package types_test

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/name/types"
)

func TestModuleConstants(t *testing.T) {
	require.Equal(t, "name", types.ModuleName)
	require.Equal(t, types.ModuleName, types.StoreKey)
	require.Equal(t, "gov", types.GovModuleName)
}

func TestCollectionsPrefixesAreDistinct(t *testing.T) {
	prefixes := map[string][]byte{
		"Params":        types.ParamsKey,
		"Names":         types.KeyNames,
		"Owners":        types.KeyOwners,
		"Disputes":      types.KeyDisputes,
		"OwnerNames":    types.KeyOwnerNames,
		"DisputeStakes": types.KeyDisputeStakes,
		"ContestStakes": types.KeyContestStakes,
	}

	seen := make(map[string]string, len(prefixes))
	for name, p := range prefixes {
		require.NotEmpty(t, p, "prefix %s should not be empty", name)
		key := string(p)
		if other, ok := seen[key]; ok {
			t.Fatalf("prefix collision between %s and %s", name, other)
		}
		seen[key] = name
	}
}

func TestLegacyParamKeysDistinct(t *testing.T) {
	keys := [][]byte{
		types.KeyBlockedNames,
		types.KeyMinNameLength,
		types.KeyMaxNameLength,
		types.KeyMaxNamesPerAddress,
		types.KeyRegistrationFee,
		types.KeyExpirationDuration,
	}
	seen := make(map[string]bool, len(keys))
	for _, k := range keys {
		require.NotEmpty(t, k)
		require.False(t, seen[string(k)], "duplicate legacy key: %s", k)
		seen[string(k)] = true
	}
}

func TestDefaultValuesSanity(t *testing.T) {
	require.Greater(t, types.DefaultMinNameLength, uint64(0))
	require.Greater(t, types.DefaultMaxNameLength, types.DefaultMinNameLength)
	require.Greater(t, types.DefaultMaxNamesPerAddress, uint64(0))
	require.Greater(t, types.DefaultExpirationDuration, time.Duration(0))
	require.True(t, types.DefaultRegistrationFee.IsValid())
	require.False(t, types.DefaultRegistrationFee.Amount.IsNegative())
	require.True(t, types.DefaultDisputeStakeDream.GTE(math.ZeroInt()))
	require.True(t, types.DefaultContestStakeDream.GTE(math.ZeroInt()))
	require.Greater(t, types.DefaultDisputeTimeoutBlocks, uint64(0))
}

func TestDefaultBlockedNamesHasCoreEntries(t *testing.T) {
	require.NotEmpty(t, types.DefaultBlockedNames)

	must := []string{"admin", "root", "gov", "treasury", "sparkdream"}
	present := make(map[string]bool, len(types.DefaultBlockedNames))
	for _, n := range types.DefaultBlockedNames {
		require.NotEmpty(t, n, "blocked names should never contain empty strings")
		present[n] = true
	}
	for _, name := range must {
		require.True(t, present[name], "expected %q in DefaultBlockedNames", name)
	}
}
