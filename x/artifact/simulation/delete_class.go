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

// SimulateMsgDeleteClass deletes a random empty, unhidden class.
func SimulateMsgDeleteClass(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgDeleteClass{}
		c, owner, ok := randomClassWhere(r, ctx, k, accs, func(c types.Class) bool {
			if c.Supply != 0 || c.ReservedSupply != 0 {
				return false
			}
			has, err := k.HideByTarget.Has(ctx, collectionsJoin(c.Id, 0))
			return err == nil && !has
		})
		if !ok {
			return noop(msg, "no empty class")
		}
		msg = &types.MsgDeleteClass{Owner: owner.Address.String(), ClassId: c.Id}
		return deliver(r, app, ctx, ak, bk, txGen, owner, msg, math.ZeroInt())
	}
}
