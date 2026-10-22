package simulation

import (
	"math/rand"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/artifact/keeper"
	"sparkdream/x/artifact/types"
)

// SimulateMsgRemoveCraftRecipe removes a random recipe.
func SimulateMsgRemoveCraftRecipe(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgRemoveCraftRecipe{}
		var recipe types.CraftRecipe
		found := false
		_ = k.CraftRecipes.Walk(ctx, nil, func(_ keeper.U64Pair, rc types.CraftRecipe) (bool, error) {
			if r.Intn(2) == 0 {
				recipe, found = rc, true
			}
			return found, nil
		})
		if !found {
			return noop(msg, "no recipe")
		}
		c, err := k.Classes.Get(ctx, recipe.ClassId)
		if err != nil {
			return noop(msg, "recipe class gone")
		}
		owner, ok := findAccount(accs, c.Owner)
		if !ok {
			return noop(msg, "class owner is not a sim account")
		}
		msg = &types.MsgRemoveCraftRecipe{Owner: owner.Address.String(), ClassId: c.Id, RecipeId: recipe.Id}
		return deliver(r, app, ctx, ak, bk, txGen, owner, msg, math.ZeroInt())
	}
}
