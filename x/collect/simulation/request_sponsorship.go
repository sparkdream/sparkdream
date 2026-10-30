package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

// SimulateMsgRequestSponsorship delivers MsgRequestSponsorship from a
// non-member owner of a non-hidden, unexpired, unsponsored TTL collection
// whose deposit is still held and that has no request yet. The owner escrows the permanent deposit:
// base_collection_deposit + item_count x per_item_deposit.
func SimulateMsgRequestSponsorship(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgRequestSponsorship{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		owner, msg, escrow, reason, err := e.newSponsorshipRequestMsg()
		if err != nil {
			return noOpErr(msg, err)
		}
		if reason != "" {
			return noOp(msg, reason)
		}
		return e.submit(owner, msg, escrow)
	})
}

// newSponsorshipRequestMsg builds a MsgRequestSponsorship passing the
// handler's local checks, with its signer and escrow.
func (e *simEnv) newSponsorshipRequestMsg() (simtypes.Account, *types.MsgRequestSponsorship, math.Int, string, error) {
	msg := &types.MsgRequestSponsorship{}
	eligible := func(c types.Collection) bool {
		if c.Status == types.CollectionStatus_COLLECTION_STATUS_HIDDEN || c.ExpiresAt == 0 || c.DepositBurned || e.expired(c) || c.SponsoredBy != "" ||
			e.hasSponsorshipRequest(c.Id) || e.isActiveMember(c.Owner) {
			return false
		}
		_, ok := getAccountForAddress(c.Owner, e.accs)
		return ok
	}
	colls, err := e.collectionsWhere(eligible)
	if err != nil {
		return simtypes.Account{}, msg, math.ZeroInt(), "", err
	}
	coll, ok := pick(e.r, colls)
	if !ok {
		return simtypes.Account{}, msg, math.ZeroInt(), "no non-member TTL collection to request sponsorship for", nil
	}
	owner, _ := getAccountForAddress(coll.Owner, e.accs)
	msg.Creator, msg.CollectionId = coll.Owner, coll.Id
	escrow := e.params.BaseCollectionDeposit.Add(e.params.PerItemDeposit.MulRaw(commontypes.Saturate[int64](coll.ItemCount)))
	return owner, msg, escrow, "", nil
}
