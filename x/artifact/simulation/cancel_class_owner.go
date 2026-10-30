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

// SimulateMsgCancelClassOwner withdraws a random pending class-owner proposal.
func SimulateMsgCancelClassOwner(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgCancelClassOwner{}
		po, ok := randomPendingOwner(r, ctx, k, func(types.PendingClassOwner) bool { return true })
		if !ok {
			return noop(msg, "no pending handover")
		}
		c, err := k.Classes.Get(ctx, po.ClassId)
		if err != nil {
			return noop(msg, "class gone")
		}
		owner, ok := findAccount(accs, c.Owner)
		if !ok {
			return noop(msg, "owner is not a sim account")
		}
		msg = &types.MsgCancelClassOwner{Owner: owner.Address.String(), ClassId: c.Id}
		return deliver(r, app, ctx, ak, bk, txGen, owner, msg, math.ZeroInt())
	}
}
