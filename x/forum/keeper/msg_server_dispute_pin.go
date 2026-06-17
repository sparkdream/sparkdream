package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"

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

	// Route the dispute through x/rep's GovActionAppeal machinery (ActionType
	// REPLY_PIN, ActionTarget = reply_id) so it resolves through the same
	// audited ResolveGovActionAppeal path as lock/move/hide — charging the
	// appeal bond, slashing/releasing the sentinel's pin bond, updating
	// upheld_pins/overturned_pins, and unpinning on overturn.
	if k.repKeeper == nil {
		return nil, errorsmod.Wrap(types.ErrNotSentinel, "rep keeper not wired")
	}
	creatorAddr, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}
	_, initiativeID, err := k.repKeeper.CreateGovActionAppeal(
		ctx,
		reptypes.GovActionType_GOV_ACTION_TYPE_REPLY_PIN,
		fmt.Sprintf("%d", msg.ReplyId),
		creatorAddr,
		msg.Reason,
	)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to create pin dispute appeal")
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
