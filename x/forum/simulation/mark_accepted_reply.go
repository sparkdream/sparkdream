package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgMarkAcceptedReply delivers MsgMarkAcceptedReply on one of two
// paths. Mostly the thread author accepts a reply by someone else in its
// visible thread that is neither hidden nor deleted and not already the
// accepted one (a reply by another account is created first when the
// thread has none). Otherwise an eligible CONTENT_SENTINEL proposes such a
// reply on a thread with no open bounty, no accepted or pending reply and
// proposals open; the per-thread proposal cap and reserved-tag gate are left
// to the dry-run.
func SimulateMsgMarkAcceptedReply(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgMarkAcceptedReply{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgMarkAcceptedReply{}
		if e.params.ForumPaused {
			return noOp(msg, "forum is paused")
		}
		if e.r.Intn(3) == 0 {
			m, signer, reason, err := e.newProposalMsg()
			if err != nil {
				return noOpErr(msg, err)
			}
			if reason != "" {
				return noOp(msg, reason)
			}
			return e.submit(signer, m, math.ZeroInt())
		}
		roots, err := e.postsWhere(func(p types.Post) bool {
			_, own := e.account(p.Author)
			return own && p.ParentId == 0 && visible(p)
		})
		if err != nil {
			return noOpErr(msg, err)
		}
		root, ok := pick(e.r, roots)
		if !ok {
			return noOp(msg, "no visible thread by a sim account")
		}
		md, _ := e.metadata(root.PostId)
		keep := func(r types.Post) bool {
			return r.Author != root.Author && r.PostId != md.AcceptedReplyId &&
				r.Status != types.PostStatus_POST_STATUS_DELETED && r.Status != types.PostStatus_POST_STATUS_HIDDEN
		}
		replies, err := e.repliesWhere(root.PostId, keep)
		if err != nil {
			return noOpErr(msg, err)
		}
		if len(replies) == 0 {
			if _, _, err := e.ensureReply(root, notAddr(root.Author)); err != nil {
				return noOpErr(msg, err)
			}
			if replies, err = e.repliesWhere(root.PostId, keep); err != nil {
				return noOpErr(msg, err)
			}
		}
		reply, ok := pick(e.r, replies)
		if !ok {
			return noOp(msg, "no acceptable reply in the thread")
		}
		author, _ := e.account(root.Author)
		msg.Creator, msg.ThreadId, msg.ReplyId = root.Author, root.PostId, reply.PostId
		return e.submit(author, msg, math.ZeroInt())
	})
}

// repliesWhere returns the replies of a thread accepted by keep.
func (e *simEnv) repliesWhere(threadID uint64, keep func(types.Post) bool) ([]types.Post, error) {
	replies, err := e.threadReplies(threadID)
	if err != nil {
		return nil, err
	}
	var out []types.Post
	for _, r := range replies {
		if keep(r) {
			out = append(out, r)
		}
	}
	return out, nil
}

// newProposalMsg builds a sentinel's accepted-reply proposal: an eligible
// CONTENT_SENTINEL (bonded first when no sim account holds the role)
// proposes a reply it did not write, neither hidden nor deleted, on a thread
// it did not write with no open bounty, no accepted or pending reply and
// proposals open.
func (e *simEnv) newProposalMsg() (*types.MsgMarkAcceptedReply, simtypes.Account, string, error) {
	msg := &types.MsgMarkAcceptedReply{}
	sentinels, err := e.ensureSentinels()
	if err != nil {
		return msg, simtypes.Account{}, "", err
	}
	sentinel, ok := pick(e.r, sentinels)
	if !ok {
		return msg, sentinel, "no sim account holds or can bond CONTENT_SENTINEL", nil
	}
	addr := sentinel.Address.String()
	replies, err := e.postsWhere(func(r types.Post) bool {
		if r.ParentId == 0 || r.Author == addr ||
			r.Status == types.PostStatus_POST_STATUS_DELETED || r.Status == types.PostStatus_POST_STATUS_HIDDEN {
			return false
		}
		root, ok := e.post(r.RootId)
		if !ok || root.Author == addr {
			return false
		}
		if _, open := e.openBounty(r.RootId); open {
			return false
		}
		md, _ := e.metadata(r.RootId)
		return md.AcceptedReplyId == 0 && md.ProposedReplyId == 0 && !md.ProposalsLocked
	})
	if err != nil {
		return msg, sentinel, "", err
	}
	reply, ok := pick(e.r, replies)
	if !ok {
		return msg, sentinel, "no reply the sentinel may propose", nil
	}
	msg.Creator, msg.ThreadId, msg.ReplyId = addr, reply.RootId, reply.PostId
	return msg, sentinel, "", nil
}

// pendingProposals returns the threads by live sim accounts with a pending
// accepted-reply proposal, creating one first (a real sentinel
// MsgMarkAcceptedReply) when there is none.
func (e *simEnv) pendingProposals() ([]types.Post, error) {
	list := func() ([]types.Post, error) {
		var out []types.Post
		err := e.k.ThreadMetadata.Walk(e.ctx, nil, func(id uint64, md types.ThreadMetadata) (bool, error) {
			if md.ProposedReplyId == 0 || md.AcceptedReplyId != 0 {
				return false, nil
			}
			root, ok := e.post(id)
			if !ok || root.ParentId != 0 {
				return false, nil
			}
			if _, ok := e.account(root.Author); ok {
				out = append(out, root)
			}
			return false, nil
		})
		return out, err
	}
	out, err := list()
	if err != nil || len(out) > 0 || e.params.ForumPaused {
		return out, err
	}
	msg, signer, reason, err := e.newProposalMsg()
	if err != nil || reason != "" {
		return nil, err
	}
	if ok, err := e.ensure(signer, msg, math.ZeroInt()); err != nil || !ok {
		return nil, err
	}
	return list()
}
