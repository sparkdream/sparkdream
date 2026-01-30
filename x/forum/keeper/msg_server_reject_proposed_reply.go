package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// RejectProposedReply allows thread author to reject a proposed reply.
func (k msgServer) RejectProposedReply(ctx context.Context, msg *types.MsgRejectProposedReply) (*types.MsgRejectProposedReplyResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Load thread root
	thread, err := k.Post.Get(ctx, msg.ThreadId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("thread %d not found", msg.ThreadId))
	}

	// Verify this is a root post
	if thread.ParentId != 0 {
		return nil, errorsmod.Wrap(types.ErrNotRootPost, "thread_id must be a root post")
	}

	// Only thread author can reject proposed reply
	if thread.Author != msg.Creator {
		return nil, errorsmod.Wrap(types.ErrNotThreadAuthor, "only thread author can reject proposed reply")
	}

	// Get thread metadata
	metadata, err := k.ThreadMetadata.Get(ctx, msg.ThreadId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("thread metadata for %d not found", msg.ThreadId))
	}

	// Check there is a proposed reply
	if metadata.ProposedReplyId == 0 {
		return nil, errorsmod.Wrap(types.ErrNoProposedReply, "no proposed reply to reject")
	}

	rejectedReplyId := metadata.ProposedReplyId
	proposedBy := metadata.ProposedBy

	// Clear proposed
	metadata.ProposedReplyId = 0
	metadata.ProposedBy = ""
	metadata.ProposedAt = 0

	if err := k.ThreadMetadata.Set(ctx, msg.ThreadId, metadata); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update thread metadata")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"proposed_reply_rejected",
			sdk.NewAttribute("thread_id", fmt.Sprintf("%d", msg.ThreadId)),
			sdk.NewAttribute("reply_id", fmt.Sprintf("%d", rejectedReplyId)),
			sdk.NewAttribute("rejected_by", msg.Creator),
			sdk.NewAttribute("proposed_by", proposedBy),
			sdk.NewAttribute("reason", msg.Reason),
		),
	)

	return &types.MsgRejectProposedReplyResponse{}, nil
}
