package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ConfirmProposedReply confirms a proposed reply as the accepted answer.
// This is used when a sentinel proposes a reply and the thread author confirms.
func (k msgServer) ConfirmProposedReply(ctx context.Context, msg *types.MsgConfirmProposedReply) (*types.MsgConfirmProposedReplyResponse, error) {
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

	// Only thread author can confirm proposed reply
	if thread.Author != msg.Creator {
		return nil, errorsmod.Wrap(types.ErrNotThreadAuthor, "only thread author can confirm proposed reply")
	}

	// Get thread metadata
	metadata, err := k.ThreadMetadata.Get(ctx, msg.ThreadId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("thread metadata for %d not found", msg.ThreadId))
	}

	// Check there is a proposed reply
	if metadata.ProposedReplyId == 0 {
		return nil, errorsmod.Wrap(types.ErrNoProposedReply, "no proposed reply to confirm")
	}

	// Check no existing accepted reply
	if metadata.AcceptedReplyId != 0 {
		return nil, errorsmod.Wrapf(types.ErrAlreadyAccepted, "reply %d is already accepted", metadata.AcceptedReplyId)
	}

	// Promote the proposal: credit + reward the proposing sentinel, bump the
	// curation counters, and drain the auto-confirm queue (shared with the
	// EndBlocker auto-confirm path).
	if err := k.confirmCurationProposal(ctx, &metadata, now, false); err != nil {
		return nil, err
	}
	if err := k.ThreadMetadata.Set(ctx, msg.ThreadId, metadata); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update thread metadata")
	}

	return &types.MsgConfirmProposedReplyResponse{}, nil
}
