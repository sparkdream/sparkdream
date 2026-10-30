package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

// SimulateMsgRemoveItem delivers MsgRemoveItem from a writer of a mutable,
// non-hidden collection with no pending sponsorship request, removing one of
// its non-hidden items (a TTL collection refunds the held item deposit to
// the owner).
func SimulateMsgRemoveItem(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgRemoveItem{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgRemoveItem{}
		items, err := e.editableItems(true, func(c types.Collection) bool { return !e.hasSponsorshipRequest(c.Id) })
		if err != nil {
			return noOpErr(msg, err)
		}
		it, ok := pick(e.r, items)
		if !ok {
			return noOp(msg, "no item a sim account can remove")
		}
		msg.Creator, msg.Id = it.writer.Address.String(), it.item.Id
		return e.submit(it.writer, msg, math.ZeroInt())
	})
}
