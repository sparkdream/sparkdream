package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgAssignBountyToReply delivers MsgAssignBountyToReply from the
// creator of an unexpired ACTIVE bounty with a free winner slot, naming an
// ACTIVE reply in the bounty's thread by someone else that holds no award
// yet. Missing prerequisites are built with real messages first: a bounty
// (MsgCreateBounty) and a reply by another account (MsgCreatePost).
func SimulateMsgAssignBountyToReply(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgAssignBountyToReply{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgAssignBountyToReply{}
		bounties, err := e.assignableBounties()
		if err != nil {
			return noOpErr(msg, err)
		}
		bounty, ok := pick(e.r, bounties)
		if !ok {
			return noOp(msg, "no assignable bounty held by a sim account")
		}
		m, reason, err := e.newAssignMsg(bounty)
		if err != nil {
			return noOpErr(msg, err)
		}
		if reason != "" {
			return noOp(msg, reason)
		}
		creator, _ := e.account(bounty.Creator)
		return e.submit(creator, m, math.ZeroInt())
	})
}

// assignableBounties returns the unexpired ACTIVE bounties of live sim
// accounts with a free winner slot, opening one first when there is none.
func (e *simEnv) assignableBounties() ([]types.Bounty, error) {
	return e.activeSimBounties(func(b types.Bounty) bool {
		return e.now() <= b.ExpiresAt && uint64(len(b.Awards)) < types.DefaultMaxBountyWinners
	})
}

// newAssignMsg names an eligible reply in the bounty's thread, creating one
// by another account first when there is none.
func (e *simEnv) newAssignMsg(b types.Bounty) (*types.MsgAssignBountyToReply, string, error) {
	msg := &types.MsgAssignBountyToReply{Creator: b.Creator, ThreadId: b.ThreadId, Reason: "phoenix: the most helpful answer"}
	awarded := map[uint64]bool{}
	for _, a := range b.Awards {
		awarded[a.PostId] = true
	}
	eligible := func() ([]types.Post, error) {
		replies, err := e.threadReplies(b.ThreadId)
		if err != nil {
			return nil, err
		}
		var out []types.Post
		for _, r := range replies {
			if r.Status == types.PostStatus_POST_STATUS_ACTIVE && r.Author != b.Creator && !awarded[r.PostId] {
				out = append(out, r)
			}
		}
		return out, nil
	}
	replies, err := eligible()
	if err != nil {
		return msg, "", err
	}
	if len(replies) == 0 {
		root, ok := e.post(b.ThreadId)
		if !ok {
			return msg, "bounty thread is gone", nil
		}
		if _, _, err := e.ensureReply(root, notAddr(b.Creator)); err != nil {
			return msg, "", err
		}
		if replies, err = eligible(); err != nil {
			return msg, "", err
		}
	}
	reply, ok := pick(e.r, replies)
	if !ok {
		return msg, "no eligible reply in the bounty thread", nil
	}
	msg.ReplyId = reply.PostId
	return msg, "", nil
}
