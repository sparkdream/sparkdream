package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

// SimulateMsgUnpinCollection delivers MsgUnpinCollection from an active
// member at pin_min_trust_level under max_pins_per_day (the counter is shared
// with pins) on a pinned collection.
func SimulateMsgUnpinCollection(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgUnpinCollection{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgUnpinCollection{}
		colls, err := e.collectionsWhere(func(c types.Collection) bool { return c.Pinned })
		if err != nil {
			return noOpErr(msg, err)
		}
		coll, ok := pick(e.r, colls)
		if !ok {
			return noOp(msg, "no pinned collection")
		}
		pinner, ok := pick(e.r, e.pinners())
		if !ok {
			return noOp(msg, "no member at pin_min_trust_level under the daily pin cap")
		}
		msg.Creator, msg.CollectionId = pinner.Address.String(), coll.Id
		return e.submit(pinner, msg, math.ZeroInt())
	})
}
