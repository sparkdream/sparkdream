package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// UnpinReply unpins a reply from a thread.
// governance authority can always unpin. Sentinels can only unpin their own pins.
func (k msgServer) UnpinReply(ctx context.Context, msg *types.MsgUnpinReply) (*types.MsgUnpinReplyResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	isGov := k.IsGovAuthority(ctx, msg.Creator)

	// Get thread metadata
	metadata, err := k.ThreadMetadata.Get(ctx, msg.ThreadId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("thread metadata for %d not found", msg.ThreadId))
	}

	// Find the pinned record
	foundIndex := -1
	var foundRecord *types.PinnedReplyRecord
	for i, record := range metadata.PinnedRecords {
		if record.PostId == msg.ReplyId {
			foundIndex = i
			foundRecord = record
			break
		}
	}

	if foundIndex == -1 {
		return nil, errorsmod.Wrap(types.ErrNotPinned, "reply is not pinned")
	}

	// Check authorization
	// Gov can unpin anything
	// Sentinel can only unpin their own pins (and only if not disputed/resolved)
	if !isGov {
		if foundRecord.PinnedBy != msg.Creator {
			return nil, errorsmod.Wrap(types.ErrNotAuthorized, "sentinels can only unpin their own pins")
		}
		if foundRecord.Disputed {
			return nil, errorsmod.Wrap(types.ErrPinDisputed, "cannot unpin disputed pin")
		}
	}

	// Remove from pinned IDs
	newPinnedIds := make([]uint64, 0, len(metadata.PinnedReplyIds)-1)
	for _, id := range metadata.PinnedReplyIds {
		if id != msg.ReplyId {
			newPinnedIds = append(newPinnedIds, id)
		}
	}
	metadata.PinnedReplyIds = newPinnedIds

	// Remove from records
	metadata.PinnedRecords = append(metadata.PinnedRecords[:foundIndex], metadata.PinnedRecords[foundIndex+1:]...)

	if err := k.ThreadMetadata.Set(ctx, msg.ThreadId, metadata); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update thread metadata")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"reply_unpinned",
			sdk.NewAttribute("thread_id", fmt.Sprintf("%d", msg.ThreadId)),
			sdk.NewAttribute("reply_id", fmt.Sprintf("%d", msg.ReplyId)),
			sdk.NewAttribute("unpinned_by", msg.Creator),
		),
	)

	return &types.MsgUnpinReplyResponse{}, nil
}
