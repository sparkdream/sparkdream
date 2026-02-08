package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerAbortSeasonTransition(t *testing.T) {
	t.Run("invalid authority address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.AbortSeasonTransition(f.ctx, &types.MsgAbortSeasonTransition{
			Authority: "invalid-address",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid authority address")
	})

	t.Run("not authorized", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		nonAuthority := TestAddrCreator
		nonAuthorityStr, _ := f.addressCodec.BytesToString(nonAuthority)

		_, err := ms.AbortSeasonTransition(ctx, &types.MsgAbortSeasonTransition{
			Authority: nonAuthorityStr,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotAuthorized)
	})

	t.Run("no active transition", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		// No transition state exists

		_, err := ms.AbortSeasonTransition(ctx, &types.MsgAbortSeasonTransition{
			Authority: authority,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNoActiveTransition)
	})

	t.Run("transition already complete", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		// Set transition state to complete
		state := types.SeasonTransitionState{
			Phase: types.TransitionPhase_TRANSITION_PHASE_COMPLETE,
		}
		k.SeasonTransitionState.Set(ctx, state)

		_, err := ms.AbortSeasonTransition(ctx, &types.MsgAbortSeasonTransition{
			Authority: authority,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNoActiveTransition)
	})

	t.Run("cannot abort after critical phase", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		// Set transition state to after snapshot (critical phase started)
		state := types.SeasonTransitionState{
			Phase: types.TransitionPhase_TRANSITION_PHASE_ARCHIVE_REPUTATION,
		}
		k.SeasonTransitionState.Set(ctx, state)

		_, err := ms.AbortSeasonTransition(ctx, &types.MsgAbortSeasonTransition{
			Authority: authority,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrTransitionTooFarToAbort)
	})

	t.Run("successful abort during snapshot phase", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())
		params, _ := k.Params.Get(ctx)

		// Set up an ending season
		season, _ := k.Season.Get(ctx)
		season.Status = types.SeasonStatus_SEASON_STATUS_ENDING
		k.Season.Set(ctx, season)

		// Set transition state to snapshot phase (can still abort)
		state := types.SeasonTransitionState{
			Phase: types.TransitionPhase_TRANSITION_PHASE_SNAPSHOT,
		}
		k.SeasonTransitionState.Set(ctx, state)

		_, err := ms.AbortSeasonTransition(ctx, &types.MsgAbortSeasonTransition{
			Authority: authority,
		})

		require.NoError(t, err)

		// Verify season is active again
		season, err = k.Season.Get(ctx)
		require.NoError(t, err)
		require.Equal(t, types.SeasonStatus_SEASON_STATUS_ACTIVE, season.Status)

		// Verify end block was extended by grace period
		require.Equal(t, ctx.BlockHeight()+int64(params.TransitionGracePeriod), season.EndBlock)

		// Verify transition state was cleared
		_, err = k.SeasonTransitionState.Get(ctx)
		require.Error(t, err) // Should not exist
	})
}
