package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// AbortSeasonTransition aborts a stuck season transition.
// Authorized: Commons Council policy address or Commons Operations Committee members.
// Can only abort before critical phases (reputation archival/reset).
func (k msgServer) AbortSeasonTransition(ctx context.Context, msg *types.MsgAbortSeasonTransition) (*types.MsgAbortSeasonTransitionResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Authority); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check authority (Commons Council or Operations Committee)
	if !k.IsAuthorizedForGamification(ctx, msg.Authority) {
		return nil, types.ErrNotAuthorized
	}

	// Get transition state
	state, err := k.SeasonTransitionState.Get(ctx)
	if err != nil {
		return nil, types.ErrNoActiveTransition
	}

	// Check that we're in a transition (not complete)
	if state.Phase == types.TransitionPhase_TRANSITION_PHASE_COMPLETE ||
		state.Phase == types.TransitionPhase_TRANSITION_PHASE_UNSPECIFIED {
		return nil, types.ErrNoActiveTransition
	}

	// Cannot abort after critical phases have started (data would be inconsistent)
	if state.Phase > types.TransitionPhase_TRANSITION_PHASE_SNAPSHOT {
		return nil, errorsmod.Wrapf(types.ErrTransitionTooFarToAbort,
			"cannot abort at phase %s, data may be inconsistent", state.Phase.String())
	}

	// Get current season
	season, err := k.Season.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get season")
	}

	// Get params for grace period
	params, _ := k.Params.Get(ctx)

	// Reset season to active with extended end block
	season.Status = types.SeasonStatus_SEASON_STATUS_ACTIVE
	season.EndBlock = sdkCtx.BlockHeight() + int64(params.TransitionGracePeriod)

	if err := k.Season.Set(ctx, season); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update season")
	}

	// Clear transition state
	if err := k.SeasonTransitionState.Remove(ctx); err != nil {
		return nil, errorsmod.Wrap(err, "failed to clear transition state")
	}

	// Clear recovery state if any
	_ = k.TransitionRecoveryState.Remove(ctx)

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"season_transition_aborted",
			sdk.NewAttribute("season_number", fmt.Sprintf("%d", season.Number)),
			sdk.NewAttribute("aborted_at_phase", state.Phase.String()),
			sdk.NewAttribute("new_end_block", fmt.Sprintf("%d", season.EndBlock)),
			sdk.NewAttribute("aborted_by", msg.Authority),
		),
	)

	return &types.MsgAbortSeasonTransitionResponse{}, nil
}
