package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerSkipTransitionPhase(t *testing.T) {
	t.Run("invalid authority address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.SkipTransitionPhase(f.ctx, &types.MsgSkipTransitionPhase{
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

		_, err := ms.SkipTransitionPhase(ctx, &types.MsgSkipTransitionPhase{
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

		_, err := ms.SkipTransitionPhase(ctx, &types.MsgSkipTransitionPhase{
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

		_, err := ms.SkipTransitionPhase(ctx, &types.MsgSkipTransitionPhase{
			Authority: authority,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNoActiveTransition)
	})

	t.Run("cannot skip archive reputation phase", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		// Set transition state to archive reputation phase
		state := types.SeasonTransitionState{
			Phase: types.TransitionPhase_TRANSITION_PHASE_ARCHIVE_REPUTATION,
		}
		k.SeasonTransitionState.Set(ctx, state)

		_, err := ms.SkipTransitionPhase(ctx, &types.MsgSkipTransitionPhase{
			Authority: authority,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrCannotSkipCriticalPhase)
	})

	t.Run("cannot skip reset reputation phase", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		// Set transition state to reset reputation phase
		state := types.SeasonTransitionState{
			Phase: types.TransitionPhase_TRANSITION_PHASE_RESET_REPUTATION,
		}
		k.SeasonTransitionState.Set(ctx, state)

		_, err := ms.SkipTransitionPhase(ctx, &types.MsgSkipTransitionPhase{
			Authority: authority,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrCannotSkipCriticalPhase)
	})

	t.Run("successful skip return_nomination_stakes phase", func(t *testing.T) {
		// REP-S2-6: skips landing on a critical phase (ARCHIVE_REPUTATION /
		// RESET_REPUTATION) are rejected, so we exercise a non-critical
		// transition: RETURN_NOMINATION_STAKES -> SNAPSHOT.
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		state := types.SeasonTransitionState{
			Phase:          types.TransitionPhase_TRANSITION_PHASE_RETURN_NOMINATION_STAKES,
			ProcessedCount: 10,
			LastProcessed:  "something",
		}
		k.SeasonTransitionState.Set(ctx, state)

		_, err := ms.SkipTransitionPhase(ctx, &types.MsgSkipTransitionPhase{
			Authority: authority,
		})

		require.NoError(t, err)

		state, err = k.SeasonTransitionState.Get(ctx)
		require.NoError(t, err)
		require.Equal(t, types.TransitionPhase_TRANSITION_PHASE_SNAPSHOT, state.Phase)
		require.Equal(t, uint64(0), state.ProcessedCount)
		require.Equal(t, "", state.LastProcessed)
	})

	t.Run("skip into critical phase rejected", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		state := types.SeasonTransitionState{
			Phase: types.TransitionPhase_TRANSITION_PHASE_SNAPSHOT,
		}
		k.SeasonTransitionState.Set(ctx, state)

		_, err := ms.SkipTransitionPhase(ctx, &types.MsgSkipTransitionPhase{
			Authority: authority,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrCannotSkipCriticalPhase)
	})

	t.Run("skip clears recovery mode", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		state := types.SeasonTransitionState{
			Phase: types.TransitionPhase_TRANSITION_PHASE_RETURN_NOMINATION_STAKES,
		}
		k.SeasonTransitionState.Set(ctx, state)

		// Set recovery state
		recovery := types.TransitionRecoveryState{
			RecoveryMode: true,
			FailureCount: 5,
		}
		k.TransitionRecoveryState.Set(ctx, recovery)

		_, err := ms.SkipTransitionPhase(ctx, &types.MsgSkipTransitionPhase{
			Authority: authority,
		})

		require.NoError(t, err)

		// Verify recovery mode was cleared
		recovery, err = k.TransitionRecoveryState.Get(ctx)
		require.NoError(t, err)
		require.False(t, recovery.RecoveryMode)
	})

	t.Run("skip reset_xp disables maintenance mode", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		// Set transition state to reset_xp phase with maintenance mode
		state := types.SeasonTransitionState{
			Phase:           types.TransitionPhase_TRANSITION_PHASE_RESET_XP,
			MaintenanceMode: true,
		}
		k.SeasonTransitionState.Set(ctx, state)

		_, err := ms.SkipTransitionPhase(ctx, &types.MsgSkipTransitionPhase{
			Authority: authority,
		})

		require.NoError(t, err)

		// Verify maintenance mode was disabled
		state, err = k.SeasonTransitionState.Get(ctx)
		require.NoError(t, err)
		require.False(t, state.MaintenanceMode)
	})
}
