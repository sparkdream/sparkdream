package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

// SimulateMsgChallengeReview delivers MsgChallengeReview against an
// unchallenged, non-overturned review inside challenge_window_blocks whose
// collection still exists and has not expired. The challenger is an active
// member other than the curator holding challenge_deposit unlocked DREAM;
// the curator's bond must cover the slash budget reservation (dry-run).
func SimulateMsgChallengeReview(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgChallengeReview{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgChallengeReview{}
		p := e.params
		height := e.ctx.BlockHeight()
		var reviews []types.CurationReview
		if err := e.k.CurationReview.Walk(e.ctx, nil, func(_ uint64, rv types.CurationReview) (bool, error) {
			if rv.Challenged || rv.Overturned || height-rv.CreatedAt > p.ChallengeWindowBlocks {
				return false, nil
			}
			coll, err := e.k.Collection.Get(e.ctx, rv.CollectionId)
			if err == nil && !e.expired(coll) {
				reviews = append(reviews, rv)
			}
			return false, nil
		}); err != nil {
			return noOpErr(msg, err)
		}
		rv, ok := pick(e.r, reviews)
		if !ok {
			return noOp(msg, "no challengeable review")
		}
		challengers := accountsWhere(e.accs, func(a simtypes.Account) bool {
			addr := a.Address.String()
			return addr != rv.Curator && e.isActiveMember(addr) && e.unlockedDream(addr).GTE(p.ChallengeDeposit)
		})
		challenger, ok := pick(e.r, challengers)
		if !ok {
			return noOp(msg, "no member with the challenge deposit")
		}
		msg.Creator, msg.ReviewId, msg.Reason = challenger.Address.String(), rv.Id, "simulation challenge: "+randomDescription(e.r)
		if commontypes.Saturate[uint32](len(msg.Reason)) > p.MaxChallengeReasonLength {
			msg.Reason = ""
		}
		return e.submit(challenger, msg, math.ZeroInt())
	})
}
