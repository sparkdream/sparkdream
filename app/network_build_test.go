package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckNetworkBuild(t *testing.T) {
	// Non-canonical chain-ids (local chains, sister chains) run any build.
	require.NoError(t, checkNetworkBuild(""))
	require.NoError(t, checkNetworkBuild("sparkdream"))
	require.NoError(t, checkNetworkBuild("phoenix-1"))

	for chainID, want := range canonicalNetworkChainIDs {
		err := checkNetworkBuild(chainID)
		if want == builtForNetwork {
			require.NoError(t, err, chainID)
		} else {
			require.ErrorContains(t, err, "-tags "+want, chainID)
		}
	}
}
