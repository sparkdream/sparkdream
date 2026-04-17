package simulation

import (
	"fmt"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgCreateTagBudget creates a tag budget directly via the keeper.
// Group-policy and SPARK escrow checks are bypassed; production behavior is
// covered by unit + integration tests.
func SimulateMsgCreateTagBudget(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		tagName := randomTagBudgetTag(r)

		budgetID, err := k.TagBudgetSeq.Next(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateTagBudget{}), "failed to get budget ID"), nil, nil
		}

		poolBalance := fmt.Sprintf("%d", 100+r.Intn(500))
		now := ctx.BlockTime().Unix()

		budget := types.TagBudget{
			Id:           budgetID,
			GroupAccount: simAccount.Address.String(),
			Tag:          tagName,
			PoolBalance:  poolBalance,
			MembersOnly:  r.Intn(2) == 1,
			CreatedAt:    now,
			Active:       true,
		}

		if err := k.TagBudget.Set(ctx, budgetID, budget); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateTagBudget{}), "failed to create tag budget"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateTagBudget{}), "ok (direct keeper call)"), nil, nil
	}
}
