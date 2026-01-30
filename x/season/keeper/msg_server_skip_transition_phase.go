package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// SkipTransitionPhase skips the current phase of a transition.
// Emergency action - cannot skip critical phases (reputation archival/reset).
// Only governance can skip a phase.
func (k msgServer) SkipTransitionPhase(ctx context.Context, msg *types.MsgSkipTransitionPhase) (*types.MsgSkipTransitionPhaseResponse, error) {
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

	// Cannot skip critical phases - this would leave state inconsistent
	if state.Phase == types.TransitionPhase_TRANSITION_PHASE_ARCHIVE_REPUTATION ||
		state.Phase == types.TransitionPhase_TRANSITION_PHASE_RESET_REPUTATION {
		return nil, errorsmod.Wrapf(types.ErrCannotSkipCriticalPhase,
			"cannot skip critical phase %s", state.Phase.String())
	}

	skippedPhase := state.Phase

	// Advance to next phase
	state.Phase++
	state.ProcessedCount = 0
	state.LastProcessed = ""

	// If we're exiting maintenance mode phases, disable maintenance mode
	if skippedPhase == types.TransitionPhase_TRANSITION_PHASE_RESET_XP {
		state.MaintenanceMode = false
	}

	if err := k.SeasonTransitionState.Set(ctx, state); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update transition state")
	}

	// Clear recovery mode if active
	recovery, err := k.TransitionRecoveryState.Get(ctx)
	if err == nil && recovery.RecoveryMode {
		recovery.RecoveryMode = false
		_ = k.TransitionRecoveryState.Set(ctx, recovery)
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"season_transition_phase_skipped",
			sdk.NewAttribute("skipped_phase", skippedPhase.String()),
			sdk.NewAttribute("new_phase", state.Phase.String()),
			sdk.NewAttribute("skipped_by", msg.Authority),
			sdk.NewAttribute("reason", fmt.Sprintf("emergency skip by governance")),
		),
	)

	return &types.MsgSkipTransitionPhaseResponse{}, nil
}
