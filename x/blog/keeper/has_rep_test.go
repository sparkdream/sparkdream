package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/types"
)

// BLG-10: a missing rep keeper must not silently degrade in a network build.
// Untagged test builds keep the fallback (hasRep reports false); network
// builds (-tags mainnet|testnet|devnet) panic, which fails the tx or halts the
// block hook. Run `go test -tags mainnet ./x/blog/keeper -run TestHasRep` to
// exercise the network branch.
func TestHasRepFailsClosedInNetworkBuilds(t *testing.T) {
	k := Keeper{late: &lateKeepers{}}
	if types.RepKeeperRequired {
		require.Panics(t, func() { k.hasRep() })
		return
	}
	require.False(t, k.hasRep())
	require.False(t, k.canActAs(t.Context(), "phoenix", "aurora"))
}
