package keeper

import (
	"context"
	"errors"
	"fmt"

	"sparkdream/x/season/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// AbortSeasonTransition aborts a stuck season transition.
// Authorized: governance, the Commons Council policy address or the Commons
// Operations Committee policy address (never a single committee member).
// Can only abort before the retro-PGF payout (SEA-01).
func (k msgServer) AbortSeasonTransition(ctx context.Context, msg *types.MsgAbortSeasonTransition) (*types.MsgAbortSeasonTransitionResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Authority); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check authority (gov, Commons Council or Operations Committee policy)
	if !k.IsAuthorizedForTransition(ctx, msg.Authority) {
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

	// Cannot abort once anything irreversible has happened. Use an explicit
	// allowlist so enum renumbering can't silently widen the abort window.
	// SEA-01: RETRO_REWARDS pays out in the block that finishes the phase
	// and the phase advances in that same block, so an abort is possible
	// only while convictions are still being tallied. Aborting any later
	// (RETURN_NOMINATION_STAKES, SNAPSHOT) would re-run the retro payout
	// after the grace period.
	allowedAbortPhases := map[types.TransitionPhase]bool{
		types.TransitionPhase_TRANSITION_PHASE_RETRO_REWARDS: true,
	}
	if !allowedAbortPhases[state.Phase] {
		return nil, errorsmod.Wrapf(types.ErrTransitionTooFarToAbort,
			"cannot abort at phase %s, data may be inconsistent", state.Phase.String())
	}

	// Get current season
	season, err := k.Season.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get season")
	}

	// SEA-14: each abort extends the season by the grace period; cap them so
	// a season stays inside x/rep's alias dedup horizon.
	aborts, err := k.TransitionAborts.Get(ctx, season.Number)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return nil, errorsmod.Wrap(err, "failed to read abort count")
	}
	if aborts >= types.MaxTransitionAbortsPerSeason {
		return nil, errorsmod.Wrapf(types.ErrTransitionTooFarToAbort,
			"season %d transition already aborted %d times (max %d)", season.Number, aborts, types.MaxTransitionAbortsPerSeason)
	}
	if err := k.TransitionAborts.Set(ctx, season.Number, aborts+1); err != nil {
		return nil, errorsmod.Wrap(err, "failed to record abort")
	}

	// Get params for grace period
	params, getErr := k.Params.Get(ctx)
	if getErr != nil && !errors.Is(getErr, collections.ErrNotFound) {
		return nil, getErr
	}

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
	if err := k.TransitionRecoveryState.Remove(ctx); err != nil {
		return nil, err
	}

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
