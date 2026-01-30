package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// RetrySeasonTransition retries the current phase of a failed transition.
// Only governance can retry a transition that's in recovery mode.
func (k msgServer) RetrySeasonTransition(ctx context.Context, msg *types.MsgRetrySeasonTransition) (*types.MsgRetrySeasonTransitionResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Authority); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check authority (governance only)
	if !k.IsGovAuthority(ctx, msg.Authority) {
		return nil, types.ErrNotGovAuthority
	}

	// Get transition state
	state, err := k.SeasonTransitionState.Get(ctx)
	if err != nil {
		return nil, types.ErrNoActiveTransition
	}

	// Check that we're in a transition
	if state.Phase == types.TransitionPhase_TRANSITION_PHASE_COMPLETE ||
		state.Phase == types.TransitionPhase_TRANSITION_PHASE_UNSPECIFIED {
		return nil, types.ErrNoActiveTransition
	}

	// Get recovery state
	recovery, err := k.TransitionRecoveryState.Get(ctx)
	if err != nil {
		return nil, types.ErrNotInRecoveryMode
	}

	// Check we're in recovery mode
	if !recovery.RecoveryMode {
		return nil, types.ErrNotInRecoveryMode
	}

	// Reset processed count for current phase to retry
	state.ProcessedCount = 0
	state.LastProcessed = ""

	if err := k.SeasonTransitionState.Set(ctx, state); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update transition state")
	}

	// Clear recovery mode flag
	recovery.RecoveryMode = false
	if err := k.TransitionRecoveryState.Set(ctx, recovery); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update recovery state")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"season_transition_retry",
			sdk.NewAttribute("phase", state.Phase.String()),
			sdk.NewAttribute("retry_by", msg.Authority),
			sdk.NewAttribute("previous_failures", fmt.Sprintf("%d", recovery.FailureCount)),
		),
	)

	return &types.MsgRetrySeasonTransitionResponse{}, nil
}
