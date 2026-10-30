package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

// SimulateMsgReorderItem delivers MsgReorderItem from a writer (owner, or
// an EDITOR/ADMIN collaborator who is a member) of a mutable, non-hidden
// collection, moving a non-hidden item to a
// position in [0, item_count-1].
func SimulateMsgReorderItem(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgReorderItem{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgReorderItem{}
		items, err := e.editableItems(true, func(c types.Collection) bool { return c.ItemCount > 0 })
		if err != nil {
			return noOpErr(msg, err)
		}
		it, ok := pick(e.r, items)
		if !ok {
			return noOp(msg, "no item a sim account can reorder")
		}
		msg.Creator, msg.Id = it.writer.Address.String(), it.item.Id
		msg.NewPosition = commontypes.Saturate[uint64](e.r.Int63n(commontypes.Saturate[int64](it.coll.ItemCount)))
		return e.submit(it.writer, msg, math.ZeroInt())
	})
}
