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

// SimulateMsgAcceptClassOwner accepts a random unexpired class-owner proposal.
func SimulateMsgAcceptClassOwner(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgAcceptClassOwner{}
		now := ctx.BlockTime().Unix()
		po, ok := randomPendingOwner(r, ctx, k, func(po types.PendingClassOwner) bool { return po.ExpiresAt > now })
		if !ok {
			return noop(msg, "no pending handover")
		}
		to, ok := findAccount(accs, po.ProposedOwner)
		if !ok {
			return noop(msg, "proposed owner is not a sim account")
		}
		msg = &types.MsgAcceptClassOwner{ProposedOwner: to.Address.String(), ClassId: po.ClassId}
		return deliver(r, app, ctx, ak, bk, txGen, to, msg, math.ZeroInt())
	}
}
