package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

// SimulateMsgAddItems delivers MsgAddItems: a batch of 1-5 items (within
// max_batch_size and the room left under max_items_per_collection) from a
// writer of a mutable, non-hidden collection with no pending sponsorship
// request. The writer pays the per-item deposits (and spam tax when not a
// member).
func SimulateMsgAddItems(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgAddItems{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgAddItems{}
		maxItems := uint64(e.params.MaxItemsPerCollection)
		pairs, err := e.writableCollections(true, func(c types.Collection) bool {
			return itemsEditable(c) && !e.hasSponsorshipRequest(c.Id) && c.ItemCount < maxItems
		})
		if err != nil {
			return noOpErr(msg, err)
		}
		w, ok := pick(e.r, pairs)
		if !ok {
			return noOp(msg, "no collection a sim account can add items to")
		}
		n := commontypes.Saturate[uint64](1 + e.r.Intn(5))
		if room := maxItems - w.coll.ItemCount; n > room {
			n = room
		}
		if n > uint64(e.params.MaxBatchSize) {
			n = uint64(e.params.MaxBatchSize)
		}
		if n == 0 {
			return noOp(msg, "max_batch_size is zero")
		}
		msg.Creator, msg.CollectionId = w.writer.Address.String(), w.coll.Id
		for range n {
			c := randomItemContent(e.r, w.coll.Encrypted, e.params)
			msg.Items = append(msg.Items, types.AddItemEntry{
				Title:         c.title,
				Description:   c.description,
				ImageUri:      c.imageURI,
				ReferenceType: c.refType,
				Nft:           c.nft,
				Link:          c.link,
				Custom:        c.custom,
				Attributes:    c.attributes,
				EncryptedData: c.encrypted,
			})
		}
		return e.submit(w.writer, msg, e.itemDepositCost(msg.Creator, commontypes.Saturate[int64](n)))
	})
}
