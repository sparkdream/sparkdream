package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/artifact/keeper"
	"sparkdream/x/artifact/types"
	commontypes "sparkdream/x/common/types"
)

// SimulateMsgTransferUnits moves free units of a transferable edition.
func SimulateMsgTransferUnits(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgTransferUnits{}
		c, b, holder, ok := randomBalance(r, ctx, k, accs, func(c types.Class, b types.Balance) bool {
			return c.Flags.Transferable && b.Amount > 0
		})
		if !ok {
			return noop(msg, "no transferable units")
		}
		to, ok := otherAccount(r, accs, holder.Address)
		if !ok {
			return noop(msg, "picked self as recipient")
		}
		p := k.GetParams(ctx)
		msg = &types.MsgTransferUnits{Sender: holder.Address.String(), Entries: []types.UnitTransferEntry{{
			ClassId: c.Id, EditionId: b.EditionId, Recipient: to.Address.String(), Amount: commontypes.Saturate[uint64](1 + r.Intn(commontypes.Saturate[int](b.Amount)))}}}
		return deliver(r, app, ctx, ak, bk, txGen, holder, msg, p.TokenDeposit.Add(p.InboxFee))
	}
}
