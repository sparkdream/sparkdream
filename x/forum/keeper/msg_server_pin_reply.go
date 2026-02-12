package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// PinReply pins a reply within a thread.
// governance authority can always pin. Sentinels can pin with certain conditions.
func (k msgServer) PinReply(ctx context.Context, msg *types.MsgPinReply) (*types.MsgPinReplyResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	isGov := k.IsCouncilAuthorized(ctx, msg.Creator, "commons", "operations")
	isSentinel := k.GetRepTier(ctx, msg.Creator) >= 3 && k.GetSentinelBond(ctx, msg.Creator).GTE(types.DefaultMinSentinelBond)

	if !isGov && !isSentinel {
		return nil, errorsmod.Wrap(types.ErrNotSentinel, "only operations committee or qualified sentinels can pin replies")
	}

	// Load thread root
	thread, err := k.Post.Get(ctx, msg.ThreadId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("thread %d not found", msg.ThreadId))
	}

	// Verify this is a root post
	if thread.ParentId != 0 {
		return nil, errorsmod.Wrap(types.ErrNotRootPost, "thread_id must be a root post")
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
		return nil, errorsmod.Wrap(types.ErrNotReplyInThread, "cannot pin the thread root as a reply")
	}

	// Check reply is not deleted or hidden
	if reply.Status == types.PostStatus_POST_STATUS_DELETED || reply.Status == types.PostStatus_POST_STATUS_HIDDEN {
		return nil, errorsmod.Wrapf(types.ErrPostStatus, "cannot pin reply with status %s", reply.Status.String())
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

	// Check if already pinned
	for _, pinnedID := range metadata.PinnedReplyIds {
		if pinnedID == msg.ReplyId {
			return nil, errorsmod.Wrap(types.ErrAlreadyPinned, "reply is already pinned")
		}
	}

	// Create pinned record
	record := &types.PinnedReplyRecord{
		PostId:        msg.ReplyId,
		PinnedBy:      msg.Creator,
		PinnedAt:      now,
		IsSentinelPin: !isGov,
		Disputed:      false,
		InitiativeId:  0,
	}

	metadata.PinnedReplyIds = append(metadata.PinnedReplyIds, msg.ReplyId)
	metadata.PinnedRecords = append(metadata.PinnedRecords, record)

	if err := k.ThreadMetadata.Set(ctx, msg.ThreadId, metadata); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update thread metadata")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"reply_pinned",
			sdk.NewAttribute("thread_id", fmt.Sprintf("%d", msg.ThreadId)),
			sdk.NewAttribute("reply_id", fmt.Sprintf("%d", msg.ReplyId)),
			sdk.NewAttribute("pinned_by", msg.Creator),
			sdk.NewAttribute("is_sentinel_pin", fmt.Sprintf("%t", !isGov)),
		),
	)

	return &types.MsgPinReplyResponse{}, nil
}
