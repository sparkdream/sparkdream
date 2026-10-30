package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgFlagPost delivers MsgFlagPost from a random account on a
// visible post it did not write and has not flagged, below the flagger cap.
// A non-member pays the flag spam tax; the daily flag cap is left to the
// dry-run.
func SimulateMsgFlagPost(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgFlagPost{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgFlagPost{}
		if e.params.ForumPaused {
			return noOp(msg, "forum is paused")
		}
		flagger, ok := pick(e.r, e.live())
		if !ok {
			return noOp(msg, "no live signer")
		}
		posts, err := e.flaggable(flagger.Address.String())
		if err != nil {
			return noOpErr(msg, err)
		}
		post, ok := pick(e.r, posts)
		if !ok {
			return noOp(msg, "no visible post the signer may flag")
		}
		msg = e.newFlagMsg(flagger, post.PostId)
		return e.submit(flagger, msg, e.flagCost(flagger))
	})
}

// flaggable returns the visible posts addr did not write and has not
// flagged, below the flagger cap.
func (e *simEnv) flaggable(addr string) ([]types.Post, error) {
	return e.postsWhere(func(p types.Post) bool {
		if !visible(p) || p.Author == addr {
			return false
		}
		pf, err := e.k.PostFlag.Get(e.ctx, p.PostId)
		if err != nil {
			return true
		}
		if uint64(len(pf.Flaggers)) >= types.DefaultMaxPostFlaggers {
			return false
		}
		for _, f := range pf.Flaggers {
			if f == addr {
				return false
			}
		}
		return true
	})
}

// newFlagMsg builds a flag on postID with a valid reason category (free
// text only for OTHER, where it is required).
func (e *simEnv) newFlagMsg(flagger simtypes.Account, postID uint64) *types.MsgFlagPost {
	reason := randomModerationReason(e.r)
	msg := &types.MsgFlagPost{
		Creator:  flagger.Address.String(),
		PostId:   postID,
		Category: commontypes.Saturate[uint64](int32(reason)),
	}
	if reason == commontypes.ModerationReason_MODERATION_REASON_OTHER || e.r.Intn(2) == 0 {
		msg.Reason = randomReason(e.r)
	}
	return msg
}

// flagCost is the flag spam tax a non-member pays.
func (e *simEnv) flagCost(flagger simtypes.Account) math.Int {
	if !e.member(flagger) && e.params.FlagSpamTaxAmount.IsPositive() {
		return e.params.FlagSpamTaxAmount
	}
	return math.ZeroInt()
}
