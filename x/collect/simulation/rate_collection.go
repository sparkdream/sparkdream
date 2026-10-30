package simulation

import (
	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"
	reptypes "sparkdream/x/rep/types"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

// SimulateMsgRateCollection delivers MsgRateCollection from a
// COLLECT_CURATOR bonded-role holder (bonding one first through x/rep's
// MsgBondRole when no sim account holds the role) at min_curator_trust_level
// and under its daily review cap, on a public ACTIVE feedback-enabled
// unexpired collection it neither owns nor collaborates on, has not already
// reviewed, and that is below max_reviews_per_collection active reviews.
func SimulateMsgRateCollection(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgRateCollection{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgRateCollection{}
		p := e.params
		curators, err := e.ensureRoleHolder(reptypes.RoleType_ROLE_TYPE_COLLECT_CURATOR)
		if err != nil {
			return noOpErr(msg, err)
		}
		if len(curators) == 0 {
			return noOp(msg, "no sim account holds or can bond COLLECT_CURATOR")
		}
		curators = accountsWhere(curators, func(a simtypes.Account) bool {
			addr := a.Address.String()
			br, _ := e.bondedRole(reptypes.RoleType_ROLE_TYPE_COLLECT_CURATOR, addr)
			return e.trustAtLeastName(addr, p.MinCuratorTrustLevel) &&
				e.dailyCount(addr, "review") < p.MaxReviewsPerCollection &&
				e.ctx.BlockHeight()-br.RegisteredAt >= p.MinCuratorAgeBlocks
		})
		curator, ok := pick(e.r, curators)
		if !ok {
			return noOp(msg, "no eligible curator")
		}
		addr := curator.Address.String()
		colls, err := e.collectionsWhere(func(c types.Collection) bool {
			if !publicActive(c, true) || e.expired(c) || c.Owner == addr {
				return false
			}
			_, collab := e.collaborator(c.Id, addr)
			return !collab
		})
		if err != nil {
			return noOpErr(msg, err)
		}
		coll, ok := pick(e.r, colls)
		if !ok {
			return noOp(msg, "no collection the curator may rate")
		}
		var active uint32
		reviewed := false
		if err := e.k.CurationReviewsByCollection.Walk(e.ctx, collections.NewPrefixedPairRange[uint64, uint64](coll.Id),
			func(key collections.Pair[uint64, uint64]) (bool, error) {
				rv, err := e.k.CurationReview.Get(e.ctx, key.K2())
				if err != nil {
					return true, err
				}
				if !rv.Overturned {
					active++
					reviewed = reviewed || rv.Curator == addr
				}
				return false, nil
			}); err != nil {
			return noOpErr(msg, err)
		}
		if reviewed {
			return noOp(msg, "curator already reviewed this collection")
		}
		if active >= p.MaxReviewsPerCollection {
			return noOp(msg, "max reviews per collection")
		}
		msg.Creator, msg.CollectionId, msg.Verdict = addr, coll.Id, randomCurationVerdict(e.r)
		msg.Tags = e.randomTags(p.MaxTagsPerReview)
		msg.Comment = randomDescription(e.r)
		if commontypes.Saturate[uint32](len(msg.Comment)) > p.MaxReviewCommentLength {
			msg.Comment = ""
		}
		return e.submit(curator, msg, math.ZeroInt())
	})
}
