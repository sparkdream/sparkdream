package keeper

import (
	"context"
	"encoding/json"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DisputePin allows a thread author to dispute a sentinel's pin.
// Creates an x/rep initiative for jury resolution.
func (k msgServer) DisputePin(ctx context.Context, msg *types.MsgDisputePin) (*types.MsgDisputePinResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// Load thread root to verify thread author
	thread, err := k.Post.Get(ctx, msg.ThreadId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("thread %d not found", msg.ThreadId))
	}

	// Verify this is a root post
	if thread.ParentId != 0 {
		return nil, errorsmod.Wrap(types.ErrNotRootPost, "thread_id must be a root post")
	}

	// Only thread author can dispute pins
	if thread.Author != msg.Creator {
		return nil, errorsmod.Wrap(types.ErrNotThreadAuthor, "only thread author can dispute pins")
	}

	// Get thread metadata
	metadata, err := k.ThreadMetadata.Get(ctx, msg.ThreadId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("thread metadata for %d not found", msg.ThreadId))
	}

	// Find the pinned record
	var foundRecord *types.PinnedReplyRecord
	var foundIndex int
	for i, record := range metadata.PinnedRecords {
		if record.PostId == msg.ReplyId {
			foundRecord = record
			foundIndex = i
			break
		}
	}

	if foundRecord == nil {
		return nil, errorsmod.Wrap(types.ErrNotPinned, "reply is not pinned")
	}

	// Can only dispute sentinel pins (not gov pins)
	if !foundRecord.IsSentinelPin {
		return nil, errorsmod.Wrap(types.ErrCannotDisputeGovPin, "cannot dispute governance authority pins")
	}

	// Check not already disputed
	if foundRecord.Disputed {
		return nil, errorsmod.Wrap(types.ErrAlreadyDisputed, "pin is already disputed")
	}

	// Create appeal initiative
	payload := map[string]interface{}{
		"type":       "pin_dispute",
		"thread_id":  msg.ThreadId,
		"reply_id":   msg.ReplyId,
		"pinned_by":  foundRecord.PinnedBy,
		"pinned_at":  foundRecord.PinnedAt,
		"disputed_by": msg.Creator,
		"reason":     msg.Reason,
	}
	payloadBytes, _ := json.Marshal(payload)

	initiativeID, err := k.CreateAppealInitiative(ctx, "pin_dispute", payloadBytes, now+types.DefaultAppealDeadline)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to create dispute initiative")
	}

	// Mark as disputed
	foundRecord.Disputed = true
	foundRecord.InitiativeId = initiativeID
	metadata.PinnedRecords[foundIndex] = foundRecord

	if err := k.ThreadMetadata.Set(ctx, msg.ThreadId, metadata); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update thread metadata")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"pin_disputed",
			sdk.NewAttribute("thread_id", fmt.Sprintf("%d", msg.ThreadId)),
			sdk.NewAttribute("reply_id", fmt.Sprintf("%d", msg.ReplyId)),
			sdk.NewAttribute("disputed_by", msg.Creator),
			sdk.NewAttribute("pinned_by", foundRecord.PinnedBy),
			sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiativeID)),
			sdk.NewAttribute("reason", msg.Reason),
		),
	)

	return &types.MsgDisputePinResponse{}, nil
}
