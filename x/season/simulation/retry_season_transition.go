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

// SimulateMsgRetrySeasonTransition simulates a MsgRetrySeasonTransition message using direct keeper calls.
// This bypasses the governance authority requirement for simulation purposes.
// Full authority testing should be done in integration tests.
func SimulateMsgRetrySeasonTransition(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// Get the recovery state to see if there's a failed transition to retry
		recoveryState, err := k.TransitionRecoveryState.Get(ctx)
		if err != nil {
			// Create a recovery state for simulation
			recoveryState = types.TransitionRecoveryState{
				LastAttemptBlock: ctx.BlockHeight() - 10,
				FailedPhase:      types.TransitionPhase_TRANSITION_PHASE_SNAPSHOT,
				FailureCount:     1,
				LastError:        "simulated failure",
				RecoveryMode:     true,
			}
			if err := k.TransitionRecoveryState.Set(ctx, recoveryState); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRetrySeasonTransition{}), "failed to create recovery state"), nil, nil
			}
		}

		// Check if recovery mode is active, if not enable it for simulation
		if !recoveryState.RecoveryMode {
			recoveryState.RecoveryMode = true
			recoveryState.FailedPhase = types.TransitionPhase_TRANSITION_PHASE_SNAPSHOT
			recoveryState.LastError = "simulated failure for retry"
			if err := k.TransitionRecoveryState.Set(ctx, recoveryState); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRetrySeasonTransition{}), "failed to enable recovery mode"), nil, nil
			}
		}

		// Get current transition state
		transitionState, err := k.SeasonTransitionState.Get(ctx)
		if err != nil {
			// Create a new transition state to resume from the failed phase
			transitionState = types.SeasonTransitionState{}
		}

		// Resume transition from the failed phase
		transitionState.Phase = recoveryState.FailedPhase
		transitionState.ProcessedCount = 0
		transitionState.TotalCount = 0
		transitionState.LastProcessed = ""
		transitionState.TransitionStart = ctx.BlockHeight()
		transitionState.MaintenanceMode = false // Don't enable maintenance mode in simulation

		if err := k.SeasonTransitionState.Set(ctx, transitionState); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRetrySeasonTransition{}), "failed to set transition state"), nil, nil
		}

		// Update recovery state
		recoveryState.LastAttemptBlock = ctx.BlockHeight()
		recoveryState.FailureCount++ // Increment attempt count
		recoveryState.RecoveryMode = false // Turn off recovery mode (we're retrying)

		if err := k.TransitionRecoveryState.Set(ctx, recoveryState); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRetrySeasonTransition{}), "failed to update recovery state"), nil, nil
		}

		// Note: Don't set season to maintenance status in simulation to avoid blocking other operations

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRetrySeasonTransition{}), "ok (direct keeper call)"), nil, nil
	}
}
