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

// SimulateMsgCreateCategory simulates a MsgCreateCategory message using direct keeper calls.
// This bypasses the operations committee requirement for simulation purposes.
// Full integration testing should be done in integration tests.
func SimulateMsgCreateCategory(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// Use direct keeper calls to create category (bypasses operations committee check)
		categoryID, err := k.CategorySeq.Next(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateCategory{}), "failed to get category ID"), nil, nil
		}

		category := types.Category{
			CategoryId:       categoryID,
			Title:            fmt.Sprintf("Category-%d", r.Intn(10000)),
			Description:      "Simulation generated category",
			MembersOnlyWrite: r.Intn(2) == 0,
			AdminOnlyWrite:   false,
		}

		if err := k.Category.Set(ctx, categoryID, category); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateCategory{}), "failed to create category"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateCategory{}), "ok (direct keeper call)"), nil, nil
	}
}
