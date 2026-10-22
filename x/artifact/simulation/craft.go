package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/artifact/keeper"
	"sparkdream/x/artifact/types"
)

// SimulateMsgCraft crafts a recipe whose unique inputs a sim account holds.
func SimulateMsgCraft(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgCraft{}
		var recipe types.CraftRecipe
		found := false
		_ = k.CraftRecipes.Walk(ctx, nil, func(_ keeper.U64Pair, rc types.CraftRecipe) (bool, error) {
			if len(rc.Inputs) == 1 && rc.Inputs[0].EditionId == 0 && r.Intn(2) == 0 {
				recipe, found = rc, true
			}
			return found, nil
		})
		if !found {
			return noop(msg, "no simple recipe")
		}
		in := recipe.Inputs[0]
		held := map[string][]types.TokenRef{}
		var crafter string
		_ = k.Tokens.Walk(ctx, nil, func(key keeper.U64Pair, t types.Token) (bool, error) {
			if t.ClassId != in.ClassId || t.Lock != types.TokenLock_TOKEN_LOCK_NONE {
				return false, nil
			}
			held[t.Owner] = append(held[t.Owner], types.TokenRef{ClassId: t.ClassId, TokenId: t.Id})
			if uint64(len(held[t.Owner])) == in.Quantity {
				crafter = t.Owner
				return true, nil
			}
			return false, nil
		})
		acc, ok := findAccount(accs, crafter)
		if crafter == "" || !ok {
			return noop(msg, "nobody holds the inputs")
		}
		msg = &types.MsgCraft{Crafter: crafter, ClassId: recipe.ClassId, RecipeId: recipe.Id, InputTokens: held[crafter]}
		return deliver(r, app, ctx, ak, bk, txGen, acc, msg, k.GetParams(ctx).TokenDeposit)
	}
}
