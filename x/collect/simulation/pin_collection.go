package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"
	reptypes "sparkdream/x/rep/types"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

// SimulateMsgPinCollection delivers MsgPinCollection from an active member
// at pin_min_trust_level under max_pins_per_day, on an ACTIVE, permanent,
// unpinned collection.
func SimulateMsgPinCollection(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgPinCollection{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgPinCollection{}
		colls, err := e.collectionsWhere(func(c types.Collection) bool {
			return c.Status == types.CollectionStatus_COLLECTION_STATUS_ACTIVE && c.ExpiresAt == 0 && !c.Pinned
		})
		if err != nil {
			return noOpErr(msg, err)
		}
		coll, ok := pick(e.r, colls)
		if !ok {
			return noOp(msg, "no pinnable collection")
		}
		pinner, ok := pick(e.r, e.pinners())
		if !ok {
			return noOp(msg, "no member at pin_min_trust_level under the daily pin cap")
		}
		msg.Creator, msg.CollectionId = pinner.Address.String(), coll.Id
		return e.submit(pinner, msg, math.ZeroInt())
	})
}

// pinners returns the active members at pin_min_trust_level with a free
// slot in the shared daily pin counter.
func (e *simEnv) pinners() []simtypes.Account {
	level := reptypes.TrustLevel(commontypes.Saturate[int32](e.params.PinMinTrustLevel))
	return accountsWhere(e.accs, func(a simtypes.Account) bool {
		addr := a.Address.String()
		return e.trustAtLeast(addr, level) && e.dailyCount(addr, "pin") < e.params.MaxPinsPerDay
	})
}
