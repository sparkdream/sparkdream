package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// MarkAcceptedReply marks a reply as the accepted answer. For the thread author
// this is immediate. For a sentinel (any other qualified address) it creates a
// proposal that the author confirms/rejects, or that auto-confirms after the
// accept_proposal_timeout.
func (k msgServer) MarkAcceptedReply(ctx context.Context, msg *types.MsgMarkAcceptedReply) (*types.MsgMarkAcceptedReplyResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// Load thread root
	thread, err := k.Post.Get(ctx, msg.ThreadId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("thread %d not found", msg.ThreadId))
	}

	// Verify this is a root post
	if thread.ParentId != 0 {
		return nil, errorsmod.Wrap(types.ErrNotRootPost, "thread_id must be a root post")
	}

	// Non-authors take the sentinel-proposal path; the author takes the
	// immediate-accept path below.
	if thread.Author != msg.Creator {
		return k.proposeAcceptedReply(ctx, msg, thread, now)
	}

	// Get or create thread metadata.
	metadata, err := k.ThreadMetadata.Get(ctx, msg.ThreadId)
	if err != nil {
		metadata = types.ThreadMetadata{
			ThreadId:       msg.ThreadId,
			PinnedReplyIds: []uint64{},
			PinnedRecords:  []*types.PinnedReplyRecord{},
		}
	}

	// reply_id == 0 is the clear path: the author removes the accepted reply.
	if msg.ReplyId == 0 {
		if metadata.AcceptedReplyId == 0 {
			return nil, errorsmod.Wrap(types.ErrNoAcceptedReply, "no accepted reply to clear")
		}
		prev := metadata.AcceptedReplyId
		// The author's choice supersedes any pending sentinel proposal.
		superseded := k.clearPendingProposal(ctx, &metadata)
		metadata.AcceptedReplyId = 0
		metadata.AcceptedBy = ""
		metadata.AcceptedAt = 0
		if err := k.ThreadMetadata.Set(ctx, msg.ThreadId, metadata); err != nil {
			return nil, errorsmod.Wrap(err, "failed to update thread metadata")
		}
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				"accepted_reply_cleared",
				sdk.NewAttribute("thread_id", fmt.Sprintf("%d", msg.ThreadId)),
				sdk.NewAttribute("previous_reply_id", fmt.Sprintf("%d", prev)),
				sdk.NewAttribute("cleared_by", msg.Creator),
			),
		)
		k.emitProposalSuperseded(ctx, msg.ThreadId, superseded, &metadata)
		return &types.MsgMarkAcceptedReplyResponse{}, nil
	}

	// Load reply
	reply, err := k.Post.Get(ctx, msg.ReplyId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("reply %d not found", msg.ReplyId))
	}

	// Verify reply is in the thread
	if reply.RootId != msg.ThreadId && reply.PostId != msg.ThreadId {
		return nil, errorsmod.Wrap(types.ErrNotReplyInThread, "reply is not in the specified thread")
	}

	// Verify reply is not the root
	if reply.ParentId == 0 {
		return nil, errorsmod.Wrap(types.ErrNotReplyInThread, "cannot accept the thread root as a reply")
	}

	// Check reply is not deleted or hidden
	if reply.Status == types.PostStatus_POST_STATUS_DELETED || reply.Status == types.PostStatus_POST_STATUS_HIDDEN {
		return nil, errorsmod.Wrapf(types.ErrPostStatus, "cannot accept reply with status %s", reply.Status.String())
	}

	// Re-submitting the already-accepted reply is a no-op error; a different
	// reply replaces the current acceptance (the author may change their mind).
	if metadata.AcceptedReplyId == msg.ReplyId {
		return nil, errorsmod.Wrapf(types.ErrAlreadyAccepted, "reply %d is already accepted", msg.ReplyId)
	}

	prev := metadata.AcceptedReplyId
	// The author's choice supersedes any pending sentinel proposal.
	superseded := k.clearPendingProposal(ctx, &metadata)

	// Mark as accepted
	metadata.AcceptedReplyId = msg.ReplyId
	metadata.AcceptedBy = msg.Creator
	metadata.AcceptedAt = now

	if err := k.ThreadMetadata.Set(ctx, msg.ThreadId, metadata); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update thread metadata")
	}

	// New acceptance emits accepted_reply_marked; replacing an existing one emits
	// accepted_reply_changed (carrying the previous reply id).
	eventType := "accepted_reply_marked"
	attrs := []sdk.Attribute{
		sdk.NewAttribute("thread_id", fmt.Sprintf("%d", msg.ThreadId)),
		sdk.NewAttribute("reply_id", fmt.Sprintf("%d", msg.ReplyId)),
		sdk.NewAttribute("accepted_by", msg.Creator),
		sdk.NewAttribute("reply_author", reply.Author),
	}
	if prev != 0 {
		eventType = "accepted_reply_changed"
		attrs = append(attrs, sdk.NewAttribute("previous_reply_id", fmt.Sprintf("%d", prev)))
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(eventType, attrs...))
	k.emitProposalSuperseded(ctx, msg.ThreadId, superseded, &metadata)

	return &types.MsgMarkAcceptedReplyResponse{}, nil
}

// proposeAcceptedReply handles MarkAcceptedReply when the caller is not the
// thread author: a qualified sentinel proposes a reply as the accepted answer,
// awaiting author confirmation (or auto-confirmation after the timeout).
func (k msgServer) proposeAcceptedReply(ctx context.Context, msg *types.MsgMarkAcceptedReply, thread types.Post, now int64) (*types.MsgMarkAcceptedReplyResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Sentinels may not clear an accepted reply — only the author can.
	if msg.ReplyId == 0 {
		return nil, errorsmod.Wrap(types.ErrSentinelCannotClearAccepted, "use the thread author to clear an accepted reply")
	}

	// Caller must be an eligible bonded sentinel. Mirrors the hide/lock/move/pin
	// gating so DEMOTED sentinels cannot propose; an UNBONDING sentinel whose
	// staying bond still covers the floor may. MarkAcceptedReply reserves no
	// slash, so only the floor check applies (no ReserveBond below).
	if _, err := k.eligibleSentinel(ctx, msg.Creator); err != nil {
		return nil, err
	}

	// Bounty threads pay out via MsgAwardBounty; a sentinel must not pre-empt
	// the creator's choice by curating an "accepted" answer.
	if has, _ := k.ActiveBountyByThread.Has(ctx, msg.ThreadId); has {
		return nil, errorsmod.Wrap(types.ErrCannotMarkBountyThread, "thread has an active bounty")
	}

	// Threads carrying a members-restricted reserved tag are governance space;
	// sentinels do not curate them.
	for _, tag := range thread.Tags {
		reserved, _ := k.repKeeper.IsReservedTag(ctx, tag)
		if !reserved {
			continue
		}
		rt, rerr := k.repKeeper.GetReservedTag(ctx, tag)
		if rerr == nil && !rt.MembersCanUse {
			return nil, errorsmod.Wrapf(types.ErrCannotMarkRestrictedTag, "tag %q is members-restricted", tag)
		}
	}

	// Validate the reply (same checks as the author path).
	reply, err := k.Post.Get(ctx, msg.ReplyId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("reply %d not found", msg.ReplyId))
	}
	if reply.RootId != msg.ThreadId && reply.PostId != msg.ThreadId {
		return nil, errorsmod.Wrap(types.ErrNotReplyInThread, "reply is not in the specified thread")
	}
	if reply.ParentId == 0 {
		return nil, errorsmod.Wrap(types.ErrNotReplyInThread, "cannot accept the thread root as a reply")
	}
	if reply.Status == types.PostStatus_POST_STATUS_DELETED || reply.Status == types.PostStatus_POST_STATUS_HIDDEN {
		return nil, errorsmod.Wrapf(types.ErrPostStatus, "cannot accept reply with status %s", reply.Status.String())
	}

	// Get or create thread metadata.
	metadata, err := k.ThreadMetadata.Get(ctx, msg.ThreadId)
	if err != nil {
		metadata = types.ThreadMetadata{
			ThreadId:       msg.ThreadId,
			PinnedReplyIds: []uint64{},
			PinnedRecords:  []*types.PinnedReplyRecord{},
		}
	}
	if metadata.AcceptedReplyId != 0 {
		return nil, errorsmod.Wrapf(types.ErrAlreadyAccepted, "reply %d is already accepted", metadata.AcceptedReplyId)
	}
	if metadata.ProposedReplyId != 0 {
		return nil, errorsmod.Wrap(types.ErrProposalAlreadyPending, "a proposal is already pending for this thread")
	}
	// The author may close a thread to curation entirely (e.g. open discussion,
	// or no good answer) so they are not forced into an irreversible acceptance.
	if metadata.ProposalsLocked {
		return nil, errorsmod.Wrap(types.ErrThreadProposalsLocked, "thread is closed to accepted-reply proposals")
	}

	// Record the proposal and enqueue it for auto-confirmation.
	params, perr := k.Params.Get(ctx)
	if perr != nil {
		params = types.DefaultParams()
	}

	// Per-sentinel-per-thread cap. Counting all proposals (confirmed or rejected),
	// a sentinel gets at most max_accept_proposals_per_sentinel_per_thread shots
	// on a thread — so a rejected sentinel cannot re-propose indefinitely. 0
	// disables the cap. Per-sentinel (not thread-global) so one griefer cannot
	// exhaust the quota and lock out honest curators.
	if cap := params.MaxAcceptProposalsPerSentinelPerThread; cap > 0 {
		if k.proposalCount(ctx, msg.ThreadId, msg.Creator) >= uint64(cap) {
			return nil, errorsmod.Wrapf(types.ErrMaxProposalsReached,
				"sentinel has reached the %d-proposal cap on thread %d", cap, msg.ThreadId)
		}
	}

	fireAt := now + params.AcceptProposalTimeout

	metadata.ProposedReplyId = msg.ReplyId
	metadata.ProposedBy = msg.Creator
	metadata.ProposedAt = now
	metadata.ProposalExtended = false
	if err := k.enqueueProposalAutoConfirm(ctx, &metadata, fireAt); err != nil {
		return nil, errorsmod.Wrap(err, "failed to enqueue proposal")
	}
	if err := k.ThreadMetadata.Set(ctx, msg.ThreadId, metadata); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update thread metadata")
	}

	// Count this proposal against the per-sentinel-per-thread cap (confirmed or
	// rejected, every proposal counts).
	if err := k.incrProposalCount(ctx, msg.ThreadId, msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update proposal count")
	}

	// total_proposals is a lifetime counter; epoch_curations is only bumped on
	// confirmation so unconfirmed proposals cannot farm rewards.
	local, err := k.SentinelActivity.Get(ctx, msg.Creator)
	if err != nil {
		local = types.SentinelActivity{Address: msg.Creator}
	}
	local.TotalProposals++
	if err := k.SentinelActivity.Set(ctx, msg.Creator, local); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update sentinel activity")
	}
	_ = k.repKeeper.RecordActivity(ctx, reptypes.RoleType_ROLE_TYPE_FORUM_SENTINEL, msg.Creator)

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"accept_proposal_created",
			sdk.NewAttribute("thread_id", fmt.Sprintf("%d", msg.ThreadId)),
			sdk.NewAttribute("reply_id", fmt.Sprintf("%d", msg.ReplyId)),
			sdk.NewAttribute("proposed_by", msg.Creator),
			sdk.NewAttribute("fire_at", fmt.Sprintf("%d", fireAt)),
		),
	)

	return &types.MsgMarkAcceptedReplyResponse{}, nil
}
