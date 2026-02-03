package simulation

import (
	"fmt"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgCreateTagBudget simulates a MsgCreateTagBudget message using direct keeper calls.
// This bypasses the SPARK token requirement for simulation purposes.
// Full token integration testing should be done in integration tests.
func SimulateMsgCreateTagBudget(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Get or create a tag
		tagName, err := getOrCreateTag(r, ctx, k)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateTagBudget{}), "failed to get/create tag"), nil, nil
		}

		// Use direct keeper calls to create tag budget (bypasses token transfer)
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

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateTagBudget{}), "ok (direct keeper call)"), nil, nil
	}
}
