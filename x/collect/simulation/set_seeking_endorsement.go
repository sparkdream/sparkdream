package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

// SimulateMsgSetSeekingEndorsement delivers MsgSetSeekingEndorsement from
// the owner of a PENDING, unendorsed collection, mostly turning seeking on
// (PENDING collections start seeking) and sometimes off.
func SimulateMsgSetSeekingEndorsement(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgSetSeekingEndorsement{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgSetSeekingEndorsement{}
		colls, err := e.collectionsWhere(func(c types.Collection) bool {
			_, ok := getAccountForAddress(c.Owner, e.accs)
			return ok && c.Status == types.CollectionStatus_COLLECTION_STATUS_PENDING && c.EndorsedBy == ""
		})
		if err != nil {
			return noOpErr(msg, err)
		}
		coll, ok := pick(e.r, colls)
		if !ok {
			return noOp(msg, "no PENDING collection owned by a sim account")
		}
		owner, _ := getAccountForAddress(coll.Owner, e.accs)
		msg.Creator, msg.CollectionId, msg.Seeking = coll.Owner, coll.Id, e.r.Intn(4) != 0
		return e.submit(owner, msg, math.ZeroInt())
	})
}
