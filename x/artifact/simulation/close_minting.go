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

// SimulateMsgCloseMinting permanently closes minting on a random open class (rarely, so most classes keep minting).
func SimulateMsgCloseMinting(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgCloseMinting{}
		if r.Intn(10) != 0 {
			return noop(msg, "skipped to keep classes open")
		}
		c, owner, ok := randomOpenClass(r, ctx, k, accs)
		if !ok {
			return noop(msg, "no open class owned by a sim account")
		}
		msg = &types.MsgCloseMinting{Owner: owner.Address.String(), ClassId: c.Id}
		return deliver(r, app, ctx, ak, bk, txGen, owner, msg, math.ZeroInt())
	}
}
