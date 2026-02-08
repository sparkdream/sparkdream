package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// SimulateMsgSkipTransitionPhase simulates a MsgSkipTransitionPhase message using direct keeper calls.
// This bypasses the governance authority requirement for simulation purposes.
// Full authority testing should be done in integration tests.
func SimulateMsgSkipTransitionPhase(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// Get the current transition state
		transitionState, err := k.SeasonTransitionState.Get(ctx)
		if err != nil {
			// Create a transition state for simulation
			transitionState = types.SeasonTransitionState{
				Phase:           types.TransitionPhase_TRANSITION_PHASE_UNSPECIFIED,
				TransitionStart: ctx.BlockHeight(),
			}
		}

		// Check if we're in a transition (not unspecified or complete)
		// If not in transition, create one for simulation purposes
		if transitionState.Phase == types.TransitionPhase_TRANSITION_PHASE_UNSPECIFIED ||
			transitionState.Phase == types.TransitionPhase_TRANSITION_PHASE_COMPLETE {
			// Start a new transition for simulation
			transitionState.Phase = types.TransitionPhase_TRANSITION_PHASE_SNAPSHOT
			transitionState.TransitionStart = ctx.BlockHeight()
			transitionState.ProcessedCount = 0
			transitionState.TotalCount = 100        // Simulated total
			transitionState.MaintenanceMode = false // Don't enable maintenance mode in simulation
			if err := k.SeasonTransitionState.Set(ctx, transitionState); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSkipTransitionPhase{}), "failed to create transition"), nil, nil
			}
		}

		// Advance to the next phase
		currentPhase := transitionState.Phase
		nextPhase := currentPhase + 1
		if nextPhase > types.TransitionPhase_TRANSITION_PHASE_COMPLETE {
			nextPhase = types.TransitionPhase_TRANSITION_PHASE_COMPLETE
		}

		// Update the transition state
		transitionState.Phase = nextPhase
		transitionState.ProcessedCount = 0 // Reset for new phase
		transitionState.TotalCount = 0
		transitionState.LastProcessed = ""
		transitionState.MaintenanceMode = false // Always reset maintenance mode in simulation

		if err := k.SeasonTransitionState.Set(ctx, transitionState); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSkipTransitionPhase{}), "failed to skip phase"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSkipTransitionPhase{}), "ok (direct keeper call)"), nil, nil
	}
}
