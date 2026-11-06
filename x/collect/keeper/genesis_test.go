package keeper_test

import (
	"context"
	"errors"
	"testing"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
	commontypes "sparkdream/x/common/types"
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

// The promotion queue is hook-driven, so genesis carries it verbatim.
func TestGenesisKeepsPromotionQueue(t *testing.T) {
	f := initFixture(t)
	require.NoError(t, f.keeper.PromotionQueue.Set(f.ctx, "phoenix", 60))
	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, exported.PromotionQueue, 1)

	g := initFixture(t)
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *exported))
	at, err := g.keeper.PromotionQueue.Get(g.ctx, "phoenix")
	require.NoError(t, err)
	require.Equal(t, int64(60), at)
}

// COL-201: a settled unappealed hide whose collection delete failed is still
// waiting for its retry after an export/import round trip.
func TestGenesisKeepsDeferredHiddenCollectionDelete(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)
	collID := f.createTTLCollection(t, f.owner, 1_000_000)
	resp, err := f.msgServer.HideContent(f.ctx, &types.MsgHideContent{
		Creator:    f.sentinel,
		TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		TargetId:   collID,
		ReasonCode: commontypes.ModerationReason_MODERATION_REASON_SPAM,
	})
	require.NoError(t, err)
	hr, err := f.keeper.HideRecord.Get(f.ctx, resp.HideRecordId)
	require.NoError(t, err)
	f.bankKeeper.sendCoinsFromModuleToAccountFn = func(context.Context, string, sdk.AccAddress, sdk.Coins) error {
		return errors.New("module account short")
	}
	f.setBlockHeight(hr.AppealDeadline + 1)
	require.NoError(t, f.keeper.PruneExpired(f.ctx))
	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NoError(t, exported.Validate())

	g := initTestFixture(t)
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *exported))
	n := 0
	require.NoError(t, g.keeper.HideRecordExpiry.Walk(g.ctx, nil, func(key collections.Pair[int64, uint64]) (bool, error) {
		n++
		require.Equal(t, hr.Id, key.K2())
		require.LessOrEqual(t, key.K1(), hr.AppealDeadline+2, "due no later than the deferred retry")
		return false, nil
	}))
	require.Equal(t, 1, n, "the deferred delete is re-indexed exactly once")

	g.setBlockHeight(hr.AppealDeadline + 2)
	require.NoError(t, g.keeper.PruneExpired(g.ctx))
	_, err = g.keeper.Collection.Get(g.ctx, collID)
	require.Error(t, err, "the imported chain finishes the delete")
}
