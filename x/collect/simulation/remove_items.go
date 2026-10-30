package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

// SimulateMsgRemoveItems delivers MsgRemoveItems: up to three distinct
// non-hidden items (within max_batch_size) of one mutable, non-hidden
// collection with no pending sponsorship request, from a writer of it.
func SimulateMsgRemoveItems(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgRemoveItems{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgRemoveItems{}
		items, err := e.editableItems(true, func(c types.Collection) bool { return !e.hasSponsorshipRequest(c.Id) })
		if err != nil {
			return noOpErr(msg, err)
		}
		first, ok := pick(e.r, items)
		if !ok {
			return noOp(msg, "no items a sim account can remove")
		}
		if e.params.MaxBatchSize == 0 {
			return noOp(msg, "max_batch_size is zero")
		}
		msg.Creator = first.writer.Address.String()
		msg.Ids = []uint64{first.item.Id}
		want := 1 + e.r.Intn(3)
		for _, it := range items {
			if len(msg.Ids) >= want || len(msg.Ids) >= int(e.params.MaxBatchSize) {
				break
			}
			if it.coll.Id == first.coll.Id && it.item.Id != first.item.Id && it.writer.Address.Equals(first.writer.Address) {
				msg.Ids = append(msg.Ids, it.item.Id)
			}
		}
		return e.submit(first.writer, msg, math.ZeroInt())
	})
}
