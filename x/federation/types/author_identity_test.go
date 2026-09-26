package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/types"
)

func TestNormalizeAuthorIdentity(t *testing.T) {
	for in, want := range map[string]string{
		"@alice@phoenix.example":              "alice@phoenix.example",
		"alice@phoenix.example":               "alice@phoenix.example",
		" @Alice@Phoenix.Example ":            "alice@phoenix.example",
		"https://phoenix.example/@alice":      "alice@phoenix.example",
		"https://phoenix.example/@alice/":     "alice@phoenix.example",
		"https://PHOENIX.example/users/Alice": "alice@phoenix.example",
		"http://phoenix.example:8080/@alice":  "alice@phoenix.example",
	} {
		got, ok := types.NormalizeAuthorIdentity(in)
		require.True(t, ok, in)
		require.Equal(t, want, got, in)
	}
	for _, bad := range []string{
		"", "alice", "@alice", "@@phoenix.example", "a@b@c",
		"https://phoenix.example/", "https://phoenix.example/@alice/statuses/1",
		"https://phoenix.example/about", "https:///@alice",
	} {
		_, ok := types.NormalizeAuthorIdentity(bad)
		require.False(t, ok, bad)
	}
	require.NoError(t, types.ValidateAllowedIdentities([]string{"*", "@a@b.example", "https://b.example/@c"}))
	require.Error(t, types.ValidateAllowedIdentities([]string{"nobody"}))
}

func TestGenesisValidatesAllowedIdentities(t *testing.T) {
	gs := types.DefaultGenesis()
	gs.PeerPolicies = []types.PeerPolicy{{PeerId: "phoenix.example", AllowedIdentities: []string{"*", "@alice@phoenix.example"}}}
	require.NoError(t, gs.Validate())
	gs.PeerPolicies[0].AllowedIdentities = []string{"alice"}
	require.Error(t, gs.Validate())
}
