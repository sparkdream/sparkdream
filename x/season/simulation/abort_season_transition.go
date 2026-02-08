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

// SimulateMsgAbortSeasonTransition simulates a MsgAbortSeasonTransition message using direct keeper calls.
// This bypasses the governance authority requirement for simulation purposes.
// Full authority testing should be done in integration tests.
func SimulateMsgAbortSeasonTransition(
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

		// Check if we're in a transition that can be aborted (not unspecified or complete)
		// If not in transition, create one for simulation purposes
		if transitionState.Phase == types.TransitionPhase_TRANSITION_PHASE_UNSPECIFIED ||
			transitionState.Phase == types.TransitionPhase_TRANSITION_PHASE_COMPLETE {
			// Start a new transition for simulation that we can then abort
			transitionState.Phase = types.TransitionPhase_TRANSITION_PHASE_SNAPSHOT
			transitionState.TransitionStart = ctx.BlockHeight()
			transitionState.ProcessedCount = 0
			transitionState.TotalCount = 100        // Simulated total
			transitionState.MaintenanceMode = false // Don't enable maintenance mode in simulation
			if err := k.SeasonTransitionState.Set(ctx, transitionState); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAbortSeasonTransition{}), "failed to create transition"), nil, nil
			}
		}

		// Store recovery information before aborting
		recoveryState := types.TransitionRecoveryState{
			LastAttemptBlock: ctx.BlockHeight(),
			FailedPhase:      transitionState.Phase,
			FailureCount:     1,
			LastError:        "aborted via simulation",
			RecoveryMode:     true,
		}

		if err := k.TransitionRecoveryState.Set(ctx, recoveryState); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAbortSeasonTransition{}), "failed to save recovery state"), nil, nil
		}

		// Reset the transition state (abort)
		transitionState.Phase = types.TransitionPhase_TRANSITION_PHASE_UNSPECIFIED
		transitionState.ProcessedCount = 0
		transitionState.TotalCount = 0
		transitionState.LastProcessed = ""
		transitionState.MaintenanceMode = false

		if err := k.SeasonTransitionState.Set(ctx, transitionState); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAbortSeasonTransition{}), "failed to abort transition"), nil, nil
		}

		// Restore the season to active status
		season, err := k.Season.Get(ctx)
		if err == nil && season.Status == types.SeasonStatus_SEASON_STATUS_MAINTENANCE {
			season.Status = types.SeasonStatus_SEASON_STATUS_ACTIVE
			k.Season.Set(ctx, season)
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAbortSeasonTransition{}), "ok (direct keeper call)"), nil, nil
	}
}
