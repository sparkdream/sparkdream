package keeper_test

import (
	"testing"

	"sparkdream/x/blog/types"

	"github.com/stretchr/testify/require"
)

func TestGenesis(t *testing.T) {
	genesisState := types.GenesisState{
		Params: types.DefaultParams(),
	}

	f := initFixture(t)
	err := f.keeper.InitGenesis(f.ctx, genesisState)
	require.NoError(t, err)
	got, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NotNil(t, got)

	require.EqualExportedValues(t, genesisState.Params, got.Params)
}

// The promotion queue (authors admitted as members with ephemeral content
// still pending) is hook-driven, so genesis carries it verbatim.
func TestGenesisKeepsPromotionQueue(t *testing.T) {
	f := initFixture(t)
	f.keeper.EnqueueAuthorForPromotion(f.ctx, "phoenix")
	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, exported.PromotionQueue, 1)

	g := initFixture(t)
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *exported))
	require.True(t, g.keeper.IsAuthorQueuedForPromotion(g.ctx, "phoenix"))
	again, err := g.keeper.ExportGenesis(g.ctx)
	require.NoError(t, err)
	require.Equal(t, exported.PromotionQueue, again.PromotionQueue)
}

// A deleted post or reply keeps its expires_at but has left both expiry
// indexes on the live chain, so an import must not put it back: the stores
// of an exported and re-imported chain have to match (TestAppImportExport).
func TestGenesisDoesNotReindexDeletedEphemerals(t *testing.T) {
	f := initFixture(t)
	gs := types.GenesisState{
		Params: types.DefaultParams(),
		Posts: []types.Post{
			{Id: 0, Creator: "phoenix", Status: types.PostStatus_POST_STATUS_ACTIVE, ExpiresAt: 1_900_000_000},
			{Id: 1, Creator: "aurora", Status: types.PostStatus_POST_STATUS_DELETED, ExpiresAt: 1_900_000_000},
			{Id: 2, Creator: "zenith", Status: types.PostStatus_POST_STATUS_ACTIVE}, // permanent
		},
		PostCount: 3,
		Replies: []types.Reply{
			{Id: 0, PostId: 0, Creator: "aurora", Status: types.ReplyStatus_REPLY_STATUS_ACTIVE, ExpiresAt: 1_900_000_000},
			{Id: 1, PostId: 0, Creator: "zenith", Status: types.ReplyStatus_REPLY_STATUS_DELETED, ExpiresAt: 1_900_000_000},
		},
		ReplyCount: 2,
	}
	require.NoError(t, f.keeper.InitGenesis(f.ctx, gs))
	// One live ephemeral post and one live ephemeral reply; nothing for the
	// deleted ones or the permanent post.
	require.Equal(t, 2, f.keeper.CountPrefixForTest(f.ctx, types.ExpiryKey))
	require.Equal(t, 2, f.keeper.CountPrefixForTest(f.ctx, types.EphemeralByAuthorKey))
}
