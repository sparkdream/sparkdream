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

// SimulateMsgToggleTagBudget simulates a MsgToggleTagBudget message using direct keeper calls.
// This bypasses operations committee checks for simulation purposes.
// Full integration testing should be done in integration tests.
func SimulateMsgToggleTagBudget(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Get or create a tag budget
		budgetID, err := getOrCreateTagBudget(r, ctx, k, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgToggleTagBudget{}), "failed to get/create tag budget"), nil, nil
		}

		// Use direct keeper calls to toggle tag budget (bypasses operations committee check)
		budget, err := k.TagBudget.Get(ctx, budgetID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgToggleTagBudget{}), "tag budget not found"), nil, nil
		}

		// Toggle the active state
		budget.Active = !budget.Active

		if err := k.TagBudget.Set(ctx, budgetID, budget); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgToggleTagBudget{}), "failed to toggle tag budget"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgToggleTagBudget{}), "ok (direct keeper call)"), nil, nil
	}
}
