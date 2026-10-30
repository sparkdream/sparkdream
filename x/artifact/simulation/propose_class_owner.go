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

// SimulateMsgProposeClassOwner proposes handing a random class to another sim account.
func SimulateMsgProposeClassOwner(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgProposeClassOwner{}
		c, owner, ok := randomClassWhere(r, ctx, k, accs, func(c types.Class) bool { return true })
		if !ok {
			return noop(msg, "no class owned by a sim account")
		}
		to, ok := otherAccount(r, accs, owner.Address)
		if !ok {
			return noop(msg, "no other account")
		}
		msg = &types.MsgProposeClassOwner{Owner: owner.Address.String(), ClassId: c.Id, ProposedOwner: to.Address.String()}
		return deliver(r, app, ctx, ak, bk, txGen, owner, msg, math.ZeroInt())
	}
}
