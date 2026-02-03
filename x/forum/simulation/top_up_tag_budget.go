package simulation

import (
	"fmt"
	"math/rand"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgTopUpTagBudget simulates a MsgTopUpTagBudget message using direct keeper calls.
// This bypasses the SPARK token requirement for simulation purposes.
// Full token integration testing should be done in integration tests.
func SimulateMsgTopUpTagBudget(
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
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgTopUpTagBudget{}), "failed to get/create tag budget"), nil, nil
		}

		// Get the budget
		budget, err := k.TagBudget.Get(ctx, budgetID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgTopUpTagBudget{}), "budget not found"), nil, nil
		}

		// Use direct keeper calls to top up budget (bypasses token transfer)
		topUpVal := 50 + r.Intn(200)
		currentBalance, _ := math.NewIntFromString(budget.PoolBalance)
		if budget.PoolBalance == "" {
			currentBalance = math.ZeroInt()
		}
		newBalance := currentBalance.Add(math.NewInt(int64(topUpVal)))
		budget.PoolBalance = fmt.Sprintf("%d", newBalance.Int64())

		if err := k.TagBudget.Set(ctx, budgetID, budget); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgTopUpTagBudget{}), "failed to update budget"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgTopUpTagBudget{}), "ok (direct keeper call)"), nil, nil
	}
}
