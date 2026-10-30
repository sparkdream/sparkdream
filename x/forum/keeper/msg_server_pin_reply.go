package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	reptypes "sparkdream/x/rep/types"
)

// PinReply pins a reply within a thread.
// governance authority can always pin. Sentinels can pin with certain conditions.
func (k msgServer) PinReply(ctx context.Context, msg *types.MsgPinReply) (*types.MsgPinReplyResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}
	if err := k.requireForumNotPaused(ctx); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	isGov := k.isCouncilAuthorized(ctx, msg.Creator, "commons", "operations")

	// Sentinel gating must use the BondedRole record so DEMOTED sentinels
	// (overturn streak / SlashBond) cannot pin via residual general stake.
	// Hide/lock/move use the same check; PinReply was the last bypass.
	isSentinel := false
	if !isGov {
		if _, err := k.eligibleSentinel(ctx, msg.Creator); err != nil {
			return nil, err
		}
		isSentinel = true
	}

	if !isGov && !isSentinel {
		return nil, errorsmod.Wrap(types.ErrNotSentinel, "only operations committee or qualified sentinels can pin replies")
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		params = types.DefaultParams()
	}
	// Sentinel pins are moderation actions backed by a bond reservation, so
	// they get the same operational gates as hide / lock / move.
	if isSentinel {
		if params.ModerationPaused {
			return nil, types.ErrModerationPaused
		}
		if until := k.repKeeper.RoleOverturnCooldownUntil(ctx, reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, msg.Creator); until > now {
			return nil, errorsmod.Wrapf(types.ErrSentinelCooldown, "cooldown until %d", until)
		}
		if k.repKeeper.RoleEpochActionCount(ctx, reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, msg.Creator, reptypes.ActionKindForumPin) >= types.MaxSentinelPinsPerEpoch {
			return nil, errorsmod.Wrapf(types.ErrPinLimitExceeded, "max %d sentinel pins per epoch", types.MaxSentinelPinsPerEpoch)
		}
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

	// Only a live reply can be pinned.
	if reply.Status != types.PostStatus_POST_STATUS_ACTIVE {
		return nil, errorsmod.Wrapf(types.ErrPostStatus, "cannot pin reply with status %s", reply.Status.String())
	}

	// A sentinel pin is a rewarded action: pinning their own reply would be
	// self-dealing.
	if isSentinel && k.sameMember(ctx, reply.Author, msg.Creator) {
		return nil, errorsmod.Wrap(types.ErrCannotPinOwnReply, "sentinels cannot pin their own reply")
	}

	// Pin is display-only and requires the reply to already be permanent. Use
	// MakePostPermanent on the reply id first to lift the TTL, then pin it.
	if reply.ExpirationTime > 0 {
		return nil, errorsmod.Wrapf(types.ErrCannotPinEphemeral, "reply %d is ephemeral; call MakePostPermanent first", msg.ReplyId)
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
	if uint64(len(metadata.PinnedReplyIds)) >= types.DefaultMaxPinnedRepliesPerThread {
		return nil, errorsmod.Wrapf(types.ErrMaxPinnedReplies, "a thread holds at most %d pinned replies", types.DefaultMaxPinnedRepliesPerThread)
	}

	// Reserve the sentinel's slash bond so an overturned pin dispute has funds
	// to slash, mirroring the hide/lock/move reservation path. Released on an
	// upheld dispute via GetActionCommittedAmount. Gov pins reserve nothing.
	committedAmount := ""
	if isSentinel {
		slashAmount := params.SentinelSlashAmountInt()
		if err := k.repKeeper.ReserveBond(ctx, reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, msg.Creator, slashAmount); err != nil {
			return nil, errorsmod.Wrap(err, "insufficient bond to pin")
		}
		committedAmount = slashAmount.String()
	}

	// Create pinned record
	record := &types.PinnedReplyRecord{
		PostId:          msg.ReplyId,
		PinnedBy:        msg.Creator,
		PinnedAt:        now,
		IsSentinelPin:   !isGov,
		Disputed:        false,
		InitiativeId:    0,
		CommittedAmount: committedAmount,
	}

	metadata.PinnedReplyIds = append(metadata.PinnedReplyIds, msg.ReplyId)
	metadata.PinnedRecords = append(metadata.PinnedRecords, record)

	if err := k.ThreadMetadata.Set(ctx, msg.ThreadId, metadata); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update thread metadata")
	}

	// Pin counters for sentinel pins only, on rep's shared RoleActivity
	// record. Gov pins are not sentinel activity and must not count toward
	// epoch rewards. Mirrors the lock/move/hide counter convention.
	if isSentinel {
		if err := k.repKeeper.RecordRoleAction(ctx, reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, msg.Creator, reptypes.ActionKindForumPin); err != nil {
			return nil, err
		}
		if err := k.repKeeper.RecordActivity(ctx, reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, msg.Creator); err != nil {
			return nil, err
		}
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
