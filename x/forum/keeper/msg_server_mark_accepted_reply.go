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

	// Get or create thread metadata
	metadata, err := k.ThreadMetadata.Get(ctx, msg.ThreadId)
	if err != nil {
		// Create new metadata
		metadata = types.ThreadMetadata{
			ThreadId:       msg.ThreadId,
			PinnedReplyIds: []uint64{},
			PinnedRecords:  []*types.PinnedReplyRecord{},
		}
	}

	// Check if there's already an accepted reply
	if metadata.AcceptedReplyId != 0 {
		return nil, errorsmod.Wrapf(types.ErrAlreadyAccepted, "reply %d is already accepted", metadata.AcceptedReplyId)
	}

	// Mark as accepted
	metadata.AcceptedReplyId = msg.ReplyId
	metadata.AcceptedBy = msg.Creator
	metadata.AcceptedAt = now

	if err := k.ThreadMetadata.Set(ctx, msg.ThreadId, metadata); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update thread metadata")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"accepted_reply_marked",
			sdk.NewAttribute("thread_id", fmt.Sprintf("%d", msg.ThreadId)),
			sdk.NewAttribute("reply_id", fmt.Sprintf("%d", msg.ReplyId)),
			sdk.NewAttribute("accepted_by", msg.Creator),
			sdk.NewAttribute("reply_author", reply.Author),
		),
	)

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

	// Caller must be an active (NORMAL/RECOVERY) bonded sentinel. Mirrors the
	// hide/lock/move/pin gating so DEMOTED sentinels cannot propose.
	if k.repKeeper == nil {
		return nil, errorsmod.Wrap(types.ErrNotSentinel, "rep keeper not wired")
	}
	br, err := k.repKeeper.GetBondedRole(ctx, reptypes.RoleType_ROLE_TYPE_FORUM_SENTINEL, msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrNotSentinel, "only the thread author or a qualified sentinel can mark an accepted reply")
	}
	if br.BondStatus != reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL &&
		br.BondStatus != reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_RECOVERY {
		return nil, types.ErrSentinelDemoted
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

	// Record the proposal and enqueue it for auto-confirmation.
	params, perr := k.Params.Get(ctx)
	if perr != nil {
		params = types.DefaultParams()
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
