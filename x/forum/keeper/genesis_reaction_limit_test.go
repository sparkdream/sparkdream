package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
)

// TestGenesisReactionLimitKinds: one member holds a record per budget (the
// shared reaction budget under the bare address, flag/downvote/follow under
// prefixed keys). The genesis round trip must put each back under its own
// key, and validation must not treat them as duplicates.
func TestGenesisReactionLimitKinds(t *testing.T) {
	addr := "phoenix"
	var limits []types.UserReactionLimit
	for i, kind := range []types.ReactionLimitKind{
		types.ReactionLimitKind_REACTION_LIMIT_KIND_REACTION,
		types.ReactionLimitKind_REACTION_LIMIT_KIND_FLAG,
		types.ReactionLimitKind_REACTION_LIMIT_KIND_DOWNVOTE,
		types.ReactionLimitKind_REACTION_LIMIT_KIND_FOLLOW,
	} {
		limits = append(limits, types.UserReactionLimit{UserAddress: addr, Kind: kind, CurrentDayCount: uint64(i + 1), CurrentDayStart: 86400})
	}
	gs := types.DefaultGenesis()
	gs.UserReactionLimitMap = limits
	require.NoError(t, gs.Validate())

	f := initFixture(t)
	require.NoError(t, f.keeper.InitGenesis(f.ctx, *gs))
	for _, key := range []string{addr, "flag_" + addr, "downvote_" + addr, "follow_" + addr} {
		has, err := f.keeper.UserReactionLimit.Has(f.ctx, key)
		require.NoError(t, err)
		require.True(t, has, key)
	}
	flag, err := f.keeper.UserReactionLimit.Get(f.ctx, "flag_"+addr)
	require.NoError(t, err)
	require.Equal(t, uint64(2), flag.CurrentDayCount)

	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.ElementsMatch(t, limits, exported.UserReactionLimitMap)

	// The same record twice is still a duplicate.
	gs.UserReactionLimitMap = append(gs.UserReactionLimitMap, limits[1])
	require.Error(t, gs.Validate())
}
