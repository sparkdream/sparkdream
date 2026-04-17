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

// SimulateMsgAwardFromTagBudget deducts from an existing tag budget directly
// and records a fake award. Post + group membership checks are bypassed.
func SimulateMsgAwardFromTagBudget(
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
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAwardFromTagBudget{}), "failed to get/create tag budget"), nil, nil
		}

		budget, err := k.TagBudget.Get(ctx, budgetID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAwardFromTagBudget{}), "budget not found"), nil, nil
		}

		poolBalance, _ := math.NewIntFromString(budget.PoolBalance)
		if budget.PoolBalance == "" {
			poolBalance = math.ZeroInt()
		}
		if poolBalance.LT(math.NewInt(10)) {
			budget.PoolBalance = "1000"
			_ = k.TagBudget.Set(ctx, budgetID, budget)
			poolBalance = math.NewInt(1000)
		}

		awardVal := 10 + r.Intn(41)
		newBalance := poolBalance.Sub(math.NewInt(int64(awardVal)))
		budget.PoolBalance = fmt.Sprintf("%d", newBalance.Int64())

		if err := k.TagBudget.Set(ctx, budgetID, budget); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAwardFromTagBudget{}), "failed to update budget"), nil, nil
		}

		awardID, err := k.TagBudgetAwardSeq.Next(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAwardFromTagBudget{}), "failed to get award ID"), nil, nil
		}
		award := types.TagBudgetAward{
			Id:        awardID,
			BudgetId:  budgetID,
			PostId:    uint64(r.Intn(1000) + 1),
			Recipient: simAccount.Address.String(),
			Amount:    fmt.Sprintf("%d", awardVal),
			Reason:    "simulation award",
			AwardedAt: ctx.BlockTime().Unix(),
			AwardedBy: simAccount.Address.String(),
		}
		_ = k.TagBudgetAward.Set(ctx, awardID, award)

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAwardFromTagBudget{}), "ok (direct keeper call)"), nil, nil
	}
}
