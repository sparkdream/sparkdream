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

func TestCheckTestBuildStart(t *testing.T) {
	t.Setenv(AllowTestBuildEnv, "")
	if builtForNetwork != "" {
		require.NoError(t, CheckTestBuildStart("phoenix-1"))
		return
	}
	require.NoError(t, CheckTestBuildStart(""))
	require.NoError(t, CheckTestBuildStart("sparkdream"))
	require.ErrorContains(t, CheckTestBuildStart("phoenix-1"), "UNTAGGED TEST BUILD")
	require.ErrorContains(t, CheckTestBuildStart("phoenix-1"), AllowTestBuildEnv)

	t.Setenv(AllowTestBuildEnv, "1")
	require.NoError(t, CheckTestBuildStart("phoenix-1"))
}

func TestTestBuildWarning(t *testing.T) {
	if builtForNetwork != "" {
		require.Empty(t, testBuildWarning("phoenix-1"))
		return
	}
	require.Empty(t, testBuildWarning(""))
	require.Contains(t, testBuildWarning("phoenix-1"), "UNTAGGED TEST BUILD")
}
