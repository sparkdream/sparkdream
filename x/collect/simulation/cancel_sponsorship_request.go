package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

// SimulateMsgCancelSponsorshipRequest delivers MsgCancelSponsorshipRequest
// from the owner of a collection with a pending sponsorship request (filing
// one first when none exists); the escrowed deposit is refunded.
func SimulateMsgCancelSponsorshipRequest(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgCancelSponsorshipRequest{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgCancelSponsorshipRequest{}
		pending := func(c types.Collection) bool {
			_, ok := getAccountForAddress(c.Owner, e.accs)
			return ok && e.hasSponsorshipRequest(c.Id)
		}
		colls, err := e.collectionsWhere(pending)
		if err != nil {
			return noOpErr(msg, err)
		}
		if len(colls) == 0 {
			// File a request first (a real MsgRequestSponsorship).
			owner, req, escrow, reason, err := e.newSponsorshipRequestMsg()
			if err != nil {
				return noOpErr(msg, err)
			}
			if reason == "" {
				if _, err := e.ensure(owner, req, escrow); err != nil {
					return noOpErr(msg, err)
				}
			}
			if colls, err = e.collectionsWhere(pending); err != nil {
				return noOpErr(msg, err)
			}
		}
		coll, ok := pick(e.r, colls)
		if !ok {
			return noOp(msg, "no pending sponsorship request owned by a sim account")
		}
		owner, _ := getAccountForAddress(coll.Owner, e.accs)
		msg.Creator, msg.CollectionId = coll.Owner, coll.Id
		return e.submit(owner, msg, math.ZeroInt())
	})
}
