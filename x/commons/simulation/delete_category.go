package simulation

import (
	"fmt"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

// SimulateMsgDeleteCategory seeds a category and removes it directly via the
// keeper, skipping both the Commons Operations Committee authorization check
// and the forum-keeper "no posts in category" check that the real handler
// enforces (integration tests cover those paths). Mirrors the direct-keeper
// approach used by SimulateMsgCreateCategory.
func SimulateMsgDeleteCategory(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgDeleteCategory{})

		// 1. Seed a fresh category to delete.
		categoryID, err := k.CategorySeq.Next(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get category ID"), nil, nil
		}

		category := types.Category{
			CategoryId:       categoryID,
			Title:            fmt.Sprintf("DoomedCategory-%d", r.Intn(10000)),
			Description:      "Simulation generated category (to be deleted)",
			MembersOnlyWrite: r.Intn(2) == 0,
			AdminOnlyWrite:   false,
		}
		if err := k.Category.Set(ctx, categoryID, category); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to seed category"), nil, nil
		}

		// 2. Delete it directly through the keeper.
		if err := k.Category.Remove(ctx, categoryID); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to remove category"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, msgType, "ok (direct keeper call)"), nil, nil
	}
}
