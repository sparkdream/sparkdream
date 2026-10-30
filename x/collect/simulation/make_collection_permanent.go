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

// SimulateMsgMakeCollectionPermanent delivers MsgMakeCollectionPermanent
// from an active member at make_permanent_min_trust_level under
// max_make_permanent_per_day, on an ACTIVE, unexpired TTL collection. The
// owner burns its own escrow; anyone else burns the same amount from its
// account and the escrow goes back to the owner.
func SimulateMsgMakeCollectionPermanent(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgMakeCollectionPermanent{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgMakeCollectionPermanent{}
		p := e.params
		activeTTL := func(c types.Collection) bool {
			return c.Status == types.CollectionStatus_COLLECTION_STATUS_ACTIVE && c.ExpiresAt > 0 && !e.expired(c)
		}
		colls, err := e.collectionsWhere(activeTTL)
		if err != nil {
			return noOpErr(msg, err)
		}
		if len(colls) == 0 {
			// A member first creates an ACTIVE TTL collection (a real MsgCreateCollection).
			if _, err := e.ensureMemberCollection(true); err != nil {
				return noOpErr(msg, err)
			}
			if colls, err = e.collectionsWhere(activeTTL); err != nil {
				return noOpErr(msg, err)
			}
		}
		coll, ok := pick(e.r, colls)
		if !ok {
			return noOp(msg, "no ACTIVE TTL collection")
		}
		level := reptypes.TrustLevel(commontypes.Saturate[int32](p.MakePermanentMinTrustLevel))
		callers := accountsWhere(e.accs, func(a simtypes.Account) bool {
			addr := a.Address.String()
			return e.trustAtLeast(addr, level) && e.dailyCount(addr, "make_permanent") < p.MaxMakePermanentPerDay
		})
		caller, ok := pick(e.r, callers)
		if !ok {
			return noOp(msg, "no member at make_permanent_min_trust_level under the daily cap")
		}
		msg.Creator, msg.CollectionId = caller.Address.String(), coll.Id
		spent := math.ZeroInt()
		if msg.Creator != coll.Owner {
			spent = coll.DepositAmount.Add(coll.ItemDepositTotal)
		}
		return e.submit(caller, msg, spent)
	})
}
