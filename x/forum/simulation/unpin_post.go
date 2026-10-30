package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgUnpinPost delivers MsgUnpinPost on a pinned post. Like pinning,
// it needs gov, a Commons council/committee policy or an Operations
// Committee member; the op signs as a sim account that is a committee
// member and is a NoOp when none is.
func SimulateMsgUnpinPost(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgUnpinPost{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgUnpinPost{}
		signer, ok := pick(e.r, e.opsCommittee())
		if !ok {
			return noOp(msg, "no sim account is a Commons Operations Committee member")
		}
		posts, err := e.postsWhere(func(p types.Post) bool { return p.Pinned })
		if err != nil {
			return noOpErr(msg, err)
		}
		post, ok := pick(e.r, posts)
		if !ok {
			return noOp(msg, "no pinned post")
		}
		msg.Creator, msg.PostId = signer.Address.String(), post.PostId
		return e.submit(signer, msg, math.ZeroInt())
	})
}
