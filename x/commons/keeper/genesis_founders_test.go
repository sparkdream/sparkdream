package keeper

import (
	"testing"

	"sparkdream/x/commons/types"

	"github.com/stretchr/testify/require"
)

// genesisFounders with no overrides falls back to the build's compiled-in
// founders (testparams tag in unit tests) and resolves the founder by the
// FounderName display-name convention.
func TestGenesisFounders_Defaults(t *testing.T) {
	names, handles, founderAddr := genesisFounders(nil)

	require.Equal(t, GenesisNames, names)
	require.Equal(t, GenesisHandles, handles)
	require.NotEmpty(t, founderAddr)
	require.Equal(t, FounderName, names[founderAddr])
}

// A non-empty founding_members list replaces the compiled-in founders
// entirely: names, handles, and the founder (now matched by address).
func TestGenesisFounders_Override(t *testing.T) {
	overrides := []types.FoundingMember{
		{Address: "sprkdrm1aaa", DisplayName: "Ada", Handles: []string{"ada", "lovelace"}, Founder: true},
		{Address: "sprkdrm1bbb", DisplayName: "Grace"},
	}

	names, handles, founderAddr := genesisFounders(overrides)

	require.Equal(t, map[string]string{"sprkdrm1aaa": "Ada", "sprkdrm1bbb": "Grace"}, names)
	require.Equal(t, map[string][]string{"sprkdrm1aaa": {"ada", "lovelace"}}, handles)
	require.Equal(t, "sprkdrm1aaa", founderAddr)

	// Compiled-in founders must not leak into the override set.
	for addr := range GenesisNames {
		require.NotContains(t, names, addr)
	}
}
