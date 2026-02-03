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

// SimulateMsgWithdrawTagBudget simulates a MsgWithdrawTagBudget message using direct keeper calls.
// This bypasses the x/group membership check for simulation purposes.
// Full group integration testing should be done in integration tests.
func SimulateMsgWithdrawTagBudget(
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
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgWithdrawTagBudget{}), "failed to get/create tag budget"), nil, nil
		}

		// Get the budget
		budget, err := k.TagBudget.Get(ctx, budgetID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgWithdrawTagBudget{}), "budget not found"), nil, nil
		}

		// Use direct keeper calls to withdraw (bypasses group membership check and token transfer)
		// Set pool balance to 0 to simulate withdrawal
		budget.PoolBalance = "0"
		budget.Active = false // Deactivate after withdrawal

		if err := k.TagBudget.Set(ctx, budgetID, budget); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgWithdrawTagBudget{}), "failed to update budget"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgWithdrawTagBudget{}), "ok (direct keeper call)"), nil, nil
	}
}
