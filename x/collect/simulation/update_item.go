package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

// SimulateMsgUpdateItem delivers MsgUpdateItem from a writer (owner, or a
// member EDITOR/ADMIN) of a mutable, non-hidden collection, rewriting a
// non-hidden item's content.
func SimulateMsgUpdateItem(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgUpdateItem{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgUpdateItem{}
		items, err := e.editableItems(true, func(types.Collection) bool { return true })
		if err != nil {
			return noOpErr(msg, err)
		}
		it, ok := pick(e.r, items)
		if !ok {
			return noOp(msg, "no item a sim account can update")
		}
		c := randomItemContent(e.r, it.coll.Encrypted, e.params)
		msg = &types.MsgUpdateItem{
			Creator:       it.writer.Address.String(),
			Id:            it.item.Id,
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
		return e.submit(it.writer, msg, math.ZeroInt())
	})
}
