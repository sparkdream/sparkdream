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

// SimulateMsgAddCraftRecipe adds a "two tokens of a class -> one token"
// recipe to a random unlocked class.
func SimulateMsgAddCraftRecipe(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgAddCraftRecipe{}
		target, owner, ok := randomOpenClass(r, ctx, k, accs)
		if !ok || target.CraftingLocked || target.IsEditions() || target.Reveal.Reveals() {
			return noop(msg, "no craftable target class")
		}
		var input types.Class
		found := false
		_ = k.Classes.Walk(ctx, nil, func(_ uint64, c types.Class) (bool, error) {
			if !c.IsEditions() && c.Flags.HolderMayBurn() && c.Supply >= 2 && r.Intn(2) == 0 {
				input, found = c, true
			}
			return found, nil
		})
		if !found {
			return noop(msg, "no input class")
		}
		msg = &types.MsgAddCraftRecipe{Owner: owner.Address.String(), ClassId: target.Id,
			Inputs: []types.CraftInput{{ClassId: input.Id, Quantity: 2}}, OutputMetadata: randomMetadata(r),
			AcceptedContentLicense: types.ContentLicense}
		return deliver(r, app, ctx, ak, bk, txGen, owner, msg, math.ZeroInt())
	}
}
