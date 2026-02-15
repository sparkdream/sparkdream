package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerUpdateOperationalParams(t *testing.T) {
	t.Run("gov authority succeeds (nil commonsKeeper fallback)", func(t *testing.T) {
		// initFixture passes nil for commonsKeeper, so IsCouncilAuthorized
		// falls back to IsGovAuthority.
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		authorityStr, err := f.addressCodec.BytesToString(f.keeper.GetAuthority())
		require.NoError(t, err)

		resp, err := ms.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{
			Authority:         authorityStr,
			OperationalParams: types.DefaultSeasonOperationalParams(),
		})

		require.NoError(t, err)
		require.NotNil(t, resp)

		// Verify params were applied
		params, err := f.keeper.Params.Get(f.ctx)
		require.NoError(t, err)
		require.Equal(t, types.DefaultEpochBlocks, params.EpochBlocks)
		require.Equal(t, types.DefaultSeasonDurationEpochs, params.SeasonDurationEpochs)
	})

	t.Run("council authorized succeeds (mock commonsKeeper)", func(t *testing.T) {
		randomAddr := sdk.AccAddress([]byte("council_operator"))

		// Build fixture with a commonsKeeper mock that authorizes everyone
		mock := &mockCommonsKeeper{
			IsCouncilAuthorizedFn: func(ctx context.Context, addr string, council string, committee string) bool {
				return true
			},
		}
		f := initFixtureWithCommons(t, mock)
		ms := keeper.NewMsgServerImpl(f.keeper)

		randomAddrStr, err := f.addressCodec.BytesToString(randomAddr)
		require.NoError(t, err)

		resp, err := ms.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{
			Authority:         randomAddrStr,
			OperationalParams: types.DefaultSeasonOperationalParams(),
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
	})

	t.Run("unauthorized fails", func(t *testing.T) {
		// Build fixture with a commonsKeeper mock that denies everyone
		mock := &mockCommonsKeeper{
			IsCouncilAuthorizedFn: func(ctx context.Context, addr string, council string, committee string) bool {
				return false
			},
		}
		f := initFixtureWithCommons(t, mock)
		ms := keeper.NewMsgServerImpl(f.keeper)

		nonAuthority := sdk.AccAddress([]byte("random_user_____"))
		nonAuthorityStr, err := f.addressCodec.BytesToString(nonAuthority)
		require.NoError(t, err)

		_, err = ms.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{
			Authority:         nonAuthorityStr,
			OperationalParams: types.DefaultSeasonOperationalParams(),
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrInvalidSigner)
		require.Contains(t, err.Error(), "not authorized")
	})

	t.Run("invalid params fails (EpochBlocks zero)", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		authorityStr, err := f.addressCodec.BytesToString(f.keeper.GetAuthority())
		require.NoError(t, err)

		// Use default params but set EpochBlocks to 0 (must be positive)
		opParams := types.DefaultSeasonOperationalParams()
		opParams.EpochBlocks = 0

		_, err = ms.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{
			Authority:         authorityStr,
			OperationalParams: opParams,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "epoch_blocks must be positive")
	})

	t.Run("governance-only fields preserved", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		authorityStr, err := f.addressCodec.BytesToString(f.keeper.GetAuthority())
		require.NoError(t, err)

		// Set custom governance-only fields before the operational update
		params, err := f.keeper.Params.Get(f.ctx)
		require.NoError(t, err)

		// Modify governance-only fields to non-default values
		customThresholds := []uint64{0, 50, 150, 300, 500, 750, 1050, 1400, 1800, 2250}
		params.LevelThresholds = customThresholds
		params.BaselineReputation = math.LegacyMustNewDecFromStr("0.75")
		params.MaxGuildMembers = 42

		err = f.keeper.Params.Set(f.ctx, params)
		require.NoError(t, err)

		// Apply operational params update
		resp, err := ms.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{
			Authority:         authorityStr,
			OperationalParams: types.DefaultSeasonOperationalParams(),
		})

		require.NoError(t, err)
		require.NotNil(t, resp)

		// Verify governance-only fields are preserved
		updatedParams, err := f.keeper.Params.Get(f.ctx)
		require.NoError(t, err)

		require.Equal(t, customThresholds, updatedParams.LevelThresholds,
			"LevelThresholds should be preserved")
		require.True(t, updatedParams.BaselineReputation.Equal(math.LegacyMustNewDecFromStr("0.75")),
			"BaselineReputation should be preserved at 0.75, got %s", updatedParams.BaselineReputation)
		require.Equal(t, uint32(42), updatedParams.MaxGuildMembers,
			"MaxGuildMembers should be preserved at 42")

		// Verify operational fields were updated (to default values)
		require.Equal(t, types.DefaultEpochBlocks, updatedParams.EpochBlocks)
		require.Equal(t, types.DefaultSeasonDurationEpochs, updatedParams.SeasonDurationEpochs)
		require.Equal(t, types.DefaultMinGuildMembers, updatedParams.MinGuildMembers)
	})
}
