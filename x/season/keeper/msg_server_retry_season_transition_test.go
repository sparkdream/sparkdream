package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerRetrySeasonTransition(t *testing.T) {
	t.Run("invalid authority address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.RetrySeasonTransition(f.ctx, &types.MsgRetrySeasonTransition{
			Authority: "invalid-address",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid authority address")
	})

	t.Run("not gov authority", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		nonAuthority := TestAddrCreator
		nonAuthorityStr, _ := f.addressCodec.BytesToString(nonAuthority)

		_, err := ms.RetrySeasonTransition(ctx, &types.MsgRetrySeasonTransition{
			Authority: nonAuthorityStr,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotGovAuthority)
	})

	t.Run("no active transition", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		// No transition state exists

		_, err := ms.RetrySeasonTransition(ctx, &types.MsgRetrySeasonTransition{
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

		_, err := ms.RetrySeasonTransition(ctx, &types.MsgRetrySeasonTransition{
			Authority: authority,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNoActiveTransition)
	})

	t.Run("not in recovery mode - no recovery state", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		// Set transition state to active phase
		state := types.SeasonTransitionState{
			Phase: types.TransitionPhase_TRANSITION_PHASE_ARCHIVE_REPUTATION,
		}
		k.SeasonTransitionState.Set(ctx, state)

		// No recovery state exists

		_, err := ms.RetrySeasonTransition(ctx, &types.MsgRetrySeasonTransition{
			Authority: authority,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotInRecoveryMode)
	})

	t.Run("not in recovery mode - recovery mode false", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		// Set transition state to active phase
		state := types.SeasonTransitionState{
			Phase: types.TransitionPhase_TRANSITION_PHASE_ARCHIVE_REPUTATION,
		}
		k.SeasonTransitionState.Set(ctx, state)

		// Set recovery state with RecoveryMode = false
		recovery := types.TransitionRecoveryState{
			RecoveryMode: false,
			FailureCount: 2,
		}
		k.TransitionRecoveryState.Set(ctx, recovery)

		_, err := ms.RetrySeasonTransition(ctx, &types.MsgRetrySeasonTransition{
			Authority: authority,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotInRecoveryMode)
	})

	t.Run("successful retry", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		// Set transition state to active phase with some progress
		state := types.SeasonTransitionState{
			Phase:          types.TransitionPhase_TRANSITION_PHASE_ARCHIVE_REPUTATION,
			ProcessedCount: 50,
			LastProcessed:  "some_member",
		}
		k.SeasonTransitionState.Set(ctx, state)

		// Set recovery state with RecoveryMode = true
		recovery := types.TransitionRecoveryState{
			RecoveryMode: true,
			FailureCount: 3,
		}
		k.TransitionRecoveryState.Set(ctx, recovery)

		_, err := ms.RetrySeasonTransition(ctx, &types.MsgRetrySeasonTransition{
			Authority: authority,
		})

		require.NoError(t, err)

		// Verify transition state was reset for retry
		state, err = k.SeasonTransitionState.Get(ctx)
		require.NoError(t, err)
		require.Equal(t, uint64(0), state.ProcessedCount)
		require.Equal(t, "", state.LastProcessed)

		// Verify recovery mode was cleared
		recovery, err = k.TransitionRecoveryState.Get(ctx)
		require.NoError(t, err)
		require.False(t, recovery.RecoveryMode)
	})

	t.Run("retry preserves current phase", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		// Set transition state to reset_reputation phase
		state := types.SeasonTransitionState{
			Phase:          types.TransitionPhase_TRANSITION_PHASE_RESET_REPUTATION,
			ProcessedCount: 100,
			LastProcessed:  "last_member",
		}
		k.SeasonTransitionState.Set(ctx, state)

		// Set recovery state
		recovery := types.TransitionRecoveryState{
			RecoveryMode: true,
			FailureCount: 1,
		}
		k.TransitionRecoveryState.Set(ctx, recovery)

		_, err := ms.RetrySeasonTransition(ctx, &types.MsgRetrySeasonTransition{
			Authority: authority,
		})

		require.NoError(t, err)

		// Verify phase was preserved
		state, err = k.SeasonTransitionState.Get(ctx)
		require.NoError(t, err)
		require.Equal(t, types.TransitionPhase_TRANSITION_PHASE_RESET_REPUTATION, state.Phase)
	})
}
