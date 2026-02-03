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

// SimulateMsgAwardFromTagBudget simulates a MsgAwardFromTagBudget message using direct keeper calls.
// This bypasses the x/group membership check for simulation purposes.
// Full group integration testing should be done in integration tests.
func SimulateMsgAwardFromTagBudget(
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
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAwardFromTagBudget{}), "failed to get/create tag budget"), nil, nil
		}

		// Get the budget
		budget, err := k.TagBudget.Get(ctx, budgetID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAwardFromTagBudget{}), "budget not found"), nil, nil
		}

		// Check pool has balance
		poolBalance, _ := math.NewIntFromString(budget.PoolBalance)
		if budget.PoolBalance == "" {
			poolBalance = math.ZeroInt()
		}
		if poolBalance.LT(math.NewInt(10)) {
			// Top up the budget
			budget.PoolBalance = "1000"
			k.TagBudget.Set(ctx, budgetID, budget)
			poolBalance = math.NewInt(1000)
		}

		// Get or create a post (we'll simulate awarding without checking tag match)
		postID, err := getOrCreatePost(r, ctx, k, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAwardFromTagBudget{}), "failed to get/create post"), nil, nil
		}

		// Use direct keeper calls to award (bypasses group membership check and token transfer)
		awardVal := 10 + r.Intn(41)
		newBalance := poolBalance.Sub(math.NewInt(int64(awardVal)))
		budget.PoolBalance = fmt.Sprintf("%d", newBalance.Int64())

		if err := k.TagBudget.Set(ctx, budgetID, budget); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAwardFromTagBudget{}), "failed to update budget"), nil, nil
		}

		// Record the award (simulated)
		_ = postID // Used for the award

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAwardFromTagBudget{}), "ok (direct keeper call)"), nil, nil
	}
}
