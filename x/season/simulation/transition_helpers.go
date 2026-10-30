package simulation

import (
	"errors"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// activeTransition returns the in-flight season transition, or a reason
// why there is none. The sim genesis season ends far beyond any run, so the
// transition-control ops normally NoOp here: they are delivered only when a
// transition really is in flight, never by forcing one (a transition
// archives and resets every member's reputation, which the other modules'
// ops depend on).
func activeTransition(ctx sdk.Context, k keeper.Keeper) (types.SeasonTransitionState, string, error) {
	state, err := k.SeasonTransitionState.Get(ctx)
	if errors.Is(err, collections.ErrNotFound) {
		return state, "no season transition in progress", nil
	}
	if err != nil {
		return state, "", err
	}
	if state.Phase == types.TransitionPhase_TRANSITION_PHASE_COMPLETE ||
		state.Phase == types.TransitionPhase_TRANSITION_PHASE_UNSPECIFIED {
		return state, "no season transition in progress", nil
	}
	return state, "", nil
}

// inRecovery reports whether the transition is in recovery mode.
func inRecovery(ctx sdk.Context, k keeper.Keeper) (bool, error) {
	rec, err := k.TransitionRecoveryState.Get(ctx)
	if errors.Is(err, collections.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return rec.RecoveryMode, nil
}
