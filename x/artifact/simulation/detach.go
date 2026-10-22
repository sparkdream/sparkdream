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

// SimulateMsgDetach detaches a random nested token whose tree is unlocked.
func SimulateMsgDetach(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgDetach{}
		child, owner, ok := randomToken(r, ctx, k, accs, func(_ types.Class, t types.Token) bool { return t.IsNested() })
		if !ok {
			return noop(msg, "no nested token")
		}
		msg = &types.MsgDetach{Owner: child.Owner, Child: types.TokenRef{ClassId: child.ClassId, TokenId: child.Id}}
		return deliver(r, app, ctx, ak, bk, txGen, owner, msg, math.ZeroInt())
	}
}
