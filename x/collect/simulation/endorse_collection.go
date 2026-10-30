package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

// SimulateMsgEndorseCollection delivers MsgEndorseCollection from a member
// at min_sponsor_trust_level holding endorsement_dream_stake unlocked DREAM,
// for a PENDING, seeking, unendorsed collection of a non-member owner other
// than the endorser. The stake is locked in x/rep; the escrowed creation fee
// is split between the endorser and a burn.
func SimulateMsgEndorseCollection(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgEndorseCollection{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgEndorseCollection{}
		p := e.params
		colls, err := e.collectionsWhere(func(c types.Collection) bool {
			return c.Status == types.CollectionStatus_COLLECTION_STATUS_PENDING && c.SeekingEndorsement &&
				c.EndorsedBy == "" && !e.isActiveMember(c.Owner)
		})
		if err != nil {
			return noOpErr(msg, err)
		}
		coll, ok := pick(e.r, colls)
		if !ok {
			return noOp(msg, "no collection seeking endorsement")
		}
		endorsers := accountsWhere(e.accs, func(a simtypes.Account) bool {
			addr := a.Address.String()
			return addr != coll.Owner && e.trustAtLeastName(addr, p.MinSponsorTrustLevel) &&
				e.unlockedDream(addr).GTE(p.EndorsementDreamStake)
		})
		endorser, ok := pick(e.r, endorsers)
		if !ok {
			return noOp(msg, "no member able to endorse")
		}
		msg.Creator, msg.CollectionId = endorser.Address.String(), coll.Id
		return e.submit(endorser, msg, math.ZeroInt())
	})
}
