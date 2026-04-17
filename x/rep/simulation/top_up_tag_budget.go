package simulation

import (
	"fmt"
	"math/rand"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgTopUpTagBudget increases an existing tag budget balance directly,
// bypassing SPARK escrow.
func SimulateMsgTopUpTagBudget(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		budgetID, err := getOrCreateSimTagBudget(r, ctx, k, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgTopUpTagBudget{}), "failed to get/create tag budget"), nil, nil
		}

		budget, err := k.TagBudget.Get(ctx, budgetID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgTopUpTagBudget{}), "budget not found"), nil, nil
		}

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

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgTopUpTagBudget{}), "ok (direct keeper call)"), nil, nil
	}
}
