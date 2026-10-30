package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

// SimulateMsgAddItem delivers MsgAddItem from the owner or a member
// EDITOR/ADMIN of a mutable, non-hidden collection below max_items with no
// pending sponsorship request. The writer pays the per-item deposit (and the
// spam tax when not a member). Items of an encrypted collection carry only
// ciphertext.
func SimulateMsgAddItem(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgAddItem{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgAddItem{}
		pairs, err := e.writableCollections(true, func(c types.Collection) bool {
			return itemsEditable(c) && !e.hasSponsorshipRequest(c.Id) && c.ItemCount < uint64(e.params.MaxItemsPerCollection)
		})
		if err != nil {
			return noOpErr(msg, err)
		}
		w, ok := pick(e.r, pairs)
		if !ok {
			return noOp(msg, "no collection a sim account can add an item to")
		}
		msg = e.newItemMsg(w)
		return e.submit(w.writer, msg, e.itemDepositCost(msg.Creator, 1))
	})
}

// newItemMsg builds a MsgAddItem from w.writer to w.coll at a random
// position (past the end appends).
func (e *simEnv) newItemMsg(w writable) *types.MsgAddItem {
	c := randomItemContent(e.r, w.coll.Encrypted, e.params)
	return &types.MsgAddItem{
		Creator:       w.writer.Address.String(),
		CollectionId:  w.coll.Id,
		Position:      commontypes.Saturate[uint64](e.r.Int63n(commontypes.Saturate[int64](w.coll.ItemCount) + 2)),
		Title:         c.title,
		Description:   c.description,
		ImageUri:      c.imageURI,
		ReferenceType: c.refType,
		Nft:           c.nft,
		Link:          c.link,
		Custom:        c.custom,
		Attributes:    c.attributes,
		EncryptedData: c.encrypted,
	}
}
