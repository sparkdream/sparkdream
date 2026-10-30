package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgPinPost delivers MsgPinPost on a permanent, unpinned thread
// that is neither hidden nor deleted. Only gov, the Commons Council or
// Operations Committee policy, or an Operations Committee member may pin;
// the op signs as a sim account that is a committee member and is a NoOp
// when none is (sim accounts are never made group or committee policies).
func SimulateMsgPinPost(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgPinPost{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgPinPost{}
		signer, ok := pick(e.r, e.opsCommittee())
		if !ok {
			return noOp(msg, "no sim account is a Commons Operations Committee member")
		}
		roots, err := e.postsWhere(func(p types.Post) bool {
			return p.ParentId == 0 && !p.Pinned && p.ExpirationTime == 0 &&
				p.Status != types.PostStatus_POST_STATUS_HIDDEN && p.Status != types.PostStatus_POST_STATUS_DELETED
		})
		if err != nil {
			return noOpErr(msg, err)
		}
		root, ok := pick(e.r, roots)
		if !ok {
			return noOp(msg, "no pinnable thread")
		}
		msg.Creator, msg.PostId = signer.Address.String(), root.PostId
		msg.Priority = commontypes.Saturate[uint64](e.r.Intn(10))
		return e.submit(signer, msg, math.ZeroInt())
	})
}
