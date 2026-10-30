package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

// SimulateMsgDeleteCollection delivers MsgDeleteCollection from the owner of
// a collection that is not HIDDEN (a standing hide blocks owner deletes).
// Deleting refunds held deposits and settles any escrowed endorsement fee.
func SimulateMsgDeleteCollection(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgDeleteCollection{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgDeleteCollection{}
		colls, err := e.collectionsWhere(func(c types.Collection) bool {
			_, ok := getAccountForAddress(c.Owner, e.accs)
			return ok && c.Status != types.CollectionStatus_COLLECTION_STATUS_HIDDEN
		})
		if err != nil {
			return noOpErr(msg, err)
		}
		coll, ok := pick(e.r, colls)
		if !ok {
			return noOp(msg, "no deletable collection owned by a sim account")
		}
		owner, _ := getAccountForAddress(coll.Owner, e.accs)
		msg.Creator, msg.Id = coll.Owner, coll.Id
		return e.submit(owner, msg, math.ZeroInt())
	})
}
