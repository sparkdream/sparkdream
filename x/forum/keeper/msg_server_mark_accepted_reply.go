package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// MarkAcceptedReply allows thread author to mark a reply as the accepted answer.
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

	// Only thread author can mark accepted reply
	if thread.Author != msg.Creator {
		return nil, errorsmod.Wrap(types.ErrNotThreadAuthor, "only thread author can mark accepted reply")
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
