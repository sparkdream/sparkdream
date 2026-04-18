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

// SimulateMsgCreateCategory writes a category directly, skipping the
// Commons Operations Committee authorization check that the real handler
// enforces (integration tests cover the authorization path).
func SimulateMsgCreateCategory(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
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

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateCategory{}), "ok (direct keeper call)"), nil, nil
	}
}
