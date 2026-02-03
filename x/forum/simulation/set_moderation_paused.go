package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgSetModerationPaused simulates a MsgSetModerationPaused message using direct keeper calls.
// This bypasses the governance authority requirement for simulation purposes.
// Full authority testing should be done in integration tests.
// NOTE: We always set paused=false to avoid breaking other simulations.
func SimulateMsgSetModerationPaused(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// Get current params
		params, err := k.Params.Get(ctx)
		if err != nil {
			params = types.DefaultParams()
		}

		// Always set paused=false to not break other moderation simulations
		params.ModerationPaused = false

		if err := k.Params.Set(ctx, params); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSetModerationPaused{}), "failed to set params"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSetModerationPaused{}), "ok (direct keeper call)"), nil, nil
	}
}
