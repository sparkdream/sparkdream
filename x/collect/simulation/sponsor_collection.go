package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

// SimulateMsgSponsorCollection delivers MsgSponsorCollection from a member
// at min_sponsor_trust_level other than the owner, for an unexpired ACTIVE
// TTL collection with a pending request whose escrowed deposits still match the
// collection's. The sponsor pays sponsor_fee (burned).
func SimulateMsgSponsorCollection(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgSponsorCollection{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgSponsorCollection{}
		p := e.params
		var colls []types.Collection
		if err := e.k.SponsorshipRequest.Walk(e.ctx, nil, func(id uint64, req types.SponsorshipRequest) (bool, error) {
			c, err := e.k.Collection.Get(e.ctx, id)
			if err != nil {
				return true, err
			}
			// A HIDDEN collection cannot be sponsored; a PENDING one is
			// promoted to ACTIVE by the sponsorship.
			if c.ExpiresAt > 0 && !e.expired(c) && c.Status != types.CollectionStatus_COLLECTION_STATUS_HIDDEN &&
				req.CollectionDeposit.Equal(c.DepositAmount) && req.ItemDepositTotal.Equal(c.ItemDepositTotal) {
				colls = append(colls, c)
			}
			return false, nil
		}); err != nil {
			return noOpErr(msg, err)
		}
		coll, ok := pick(e.r, colls)
		if !ok {
			return noOp(msg, "no sponsorable request")
		}
		sponsors := accountsWhere(e.accs, func(a simtypes.Account) bool {
			addr := a.Address.String()
			return addr != coll.Owner && e.trustAtLeastName(addr, p.MinSponsorTrustLevel)
		})
		sponsor, ok := pick(e.r, sponsors)
		if !ok {
			return noOp(msg, "no member at min_sponsor_trust_level")
		}
		msg.Creator, msg.CollectionId = sponsor.Address.String(), coll.Id
		return e.submit(sponsor, msg, p.SponsorFee)
	})
}
