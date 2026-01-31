package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerExtendSeason(t *testing.T) {
	t.Run("invalid authority address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.ExtendSeason(f.ctx, &types.MsgExtendSeason{
			Authority:       "invalid-address",
			ExtensionEpochs: 1,
			Reason:          "Test extension",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid authority address")
	})

	t.Run("not commons council", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		nonAuthority := TestAddrCreator
		nonAuthorityStr, _ := f.addressCodec.BytesToString(nonAuthority)

		_, err := ms.ExtendSeason(ctx, &types.MsgExtendSeason{
			Authority:       nonAuthorityStr,
			ExtensionEpochs: 1,
			Reason:          "Test extension",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotCommonsCouncil)
	})

	t.Run("season not active", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		// Set season to ending
		season, _ := k.Season.Get(ctx)
		season.Status = types.SeasonStatus_SEASON_STATUS_ENDING
		k.Season.Set(ctx, season)

		_, err := ms.ExtendSeason(ctx, &types.MsgExtendSeason{
			Authority:       authority,
			ExtensionEpochs: 1,
			Reason:          "Test extension",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrSeasonNotActive)
	})

	t.Run("max extensions reached", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		params, _ := k.Params.Get(ctx)

		// Set season with max extensions already reached
		season, _ := k.Season.Get(ctx)
		season.Status = types.SeasonStatus_SEASON_STATUS_ACTIVE
		season.ExtensionsCount = uint64(params.MaxSeasonExtensions)
		k.Season.Set(ctx, season)

		_, err := ms.ExtendSeason(ctx, &types.MsgExtendSeason{
			Authority:       authority,
			ExtensionEpochs: 1,
			Reason:          "Test extension",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrMaxExtensionsReached)
	})

	t.Run("extension too long", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		params, _ := k.Params.Get(ctx)

		// Ensure season is active
		season, _ := k.Season.Get(ctx)
		season.Status = types.SeasonStatus_SEASON_STATUS_ACTIVE
		season.ExtensionsCount = 0
		k.Season.Set(ctx, season)

		_, err := ms.ExtendSeason(ctx, &types.MsgExtendSeason{
			Authority:       authority,
			ExtensionEpochs: params.MaxExtensionEpochs + 1, // Too many epochs
			Reason:          "Test extension",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrExtensionTooLong)
	})

	t.Run("zero extension not allowed", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		// Ensure season is active
		season, _ := k.Season.Get(ctx)
		season.Status = types.SeasonStatus_SEASON_STATUS_ACTIVE
		season.ExtensionsCount = 0
		k.Season.Set(ctx, season)

		_, err := ms.ExtendSeason(ctx, &types.MsgExtendSeason{
			Authority:       authority,
			ExtensionEpochs: 0, // Zero extension
			Reason:          "Test extension",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrExtensionTooLong)
	})

	t.Run("successful extension", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		params, _ := k.Params.Get(ctx)

		// Setup active season
		season, _ := k.Season.Get(ctx)
		season.Status = types.SeasonStatus_SEASON_STATUS_ACTIVE
		season.ExtensionsCount = 0
		originalEndBlock := season.EndBlock
		k.Season.Set(ctx, season)

		_, err := ms.ExtendSeason(ctx, &types.MsgExtendSeason{
			Authority:       authority,
			ExtensionEpochs: 2,
			Reason:          "Community event extension",
		})

		require.NoError(t, err)

		// Verify extension
		season, _ = k.Season.Get(ctx)
		expectedEndBlock := originalEndBlock + (2 * params.EpochBlocks)
		require.Equal(t, expectedEndBlock, season.EndBlock)
		require.Equal(t, uint64(1), season.ExtensionsCount)
		require.Equal(t, uint64(2), season.TotalExtensionEpochs)
	})

	t.Run("first extension stores original end block", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		// Setup active season with no original end block
		season, _ := k.Season.Get(ctx)
		season.Status = types.SeasonStatus_SEASON_STATUS_ACTIVE
		season.ExtensionsCount = 0
		season.OriginalEndBlock = 0
		originalEndBlock := season.EndBlock
		k.Season.Set(ctx, season)

		_, err := ms.ExtendSeason(ctx, &types.MsgExtendSeason{
			Authority:       authority,
			ExtensionEpochs: 1,
			Reason:          "First extension",
		})

		require.NoError(t, err)

		// Verify original end block was saved
		season, _ = k.Season.Get(ctx)
		require.Equal(t, originalEndBlock, season.OriginalEndBlock)
	})
}
