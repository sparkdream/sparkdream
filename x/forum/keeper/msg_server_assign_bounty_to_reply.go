package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) AssignBountyToReply(ctx context.Context, msg *types.MsgAssignBountyToReply) (*types.MsgAssignBountyToReplyResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// Lookup active bounty via the by-thread index (O(1)) instead of scanning
	// the full bounty table.
	bountyID, err := k.ActiveBountyByThread.Get(ctx, msg.ThreadId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrBountyNotFound, fmt.Sprintf("no active bounty for thread %d", msg.ThreadId))
	}
	bounty, err := k.Bounty.Get(ctx, bountyID)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrBountyNotFound, fmt.Sprintf("bounty %d not found", bountyID))
	}
	if bounty.Status != types.BountyStatus_BOUNTY_STATUS_ACTIVE {
		return nil, errorsmod.Wrapf(types.ErrBountyNotActive, "bounty status is %s", bounty.Status.String())
	}

	// Verify creator is the bounty creator
	if bounty.Creator != msg.Creator {
		return nil, errorsmod.Wrap(types.ErrNotBountyCreator, "only the bounty creator can assign awards")
	}

	// Check bounty not expired
	if now > bounty.ExpiresAt {
		return nil, types.ErrBountyExpired
	}

	// Check bounty not in moderation
	if bounty.Status == types.BountyStatus_BOUNTY_STATUS_MODERATION_PENDING {
		return nil, types.ErrBountyInModeration
	}

	// Check max winners
	if uint64(len(bounty.Awards)) >= types.DefaultMaxBountyWinners {
		return nil, types.ErrMaxBountyWinners
	}

	// Prevent duplicate awards for the same reply post.
	for _, existing := range bounty.Awards {
		if existing.PostId == msg.ReplyId {
			return nil, errorsmod.Wrap(types.ErrBountyAlreadyAwarded, "reply already received a bounty award")
		}
	}

	// Load reply post
	reply, err := k.Post.Get(ctx, msg.ReplyId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("reply %d not found", msg.ReplyId))
	}

	// Verify reply is in the bounty thread
	if reply.RootId != msg.ThreadId && reply.PostId != msg.ThreadId {
		return nil, types.ErrNotReplyInThread
	}

	// Check reply is not the root post
	if reply.ParentId == 0 {
		return nil, errorsmod.Wrap(types.ErrNotReplyInThread, "cannot award bounty to thread root post")
	}

	// Creators cannot pay the escrow back to themselves through their own reply
	if reply.Author == bounty.Creator {
		return nil, errorsmod.Wrap(types.ErrCannotAwardSelf, "bounty creator cannot accept their own reply")
	}

	// Shares are not fixed here. The escrow is divided equally among all
	// accepted replies when the creator finalizes via AwardBounty, so an
	// assignment only records who was accepted; Amount is filled in at payout.
	award := &types.BountyAward{
		PostId:    msg.ReplyId,
		Recipient: reply.Author,
		Reason:    msg.Reason,
		AwardedAt: now,
		Rank:      uint32(len(bounty.Awards) + 1),
	}

	bounty.Awards = append(bounty.Awards, award)

	// Note: Funds are NOT transferred here - they remain in escrow until AwardBounty is called
	// This allows the bounty creator to assign multiple awards before finalizing
	// The bounty remains ACTIVE so more awards can be assigned (up to max winners)

	if err := k.Bounty.Set(ctx, bountyID, bounty); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update bounty")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"bounty_assigned",
			sdk.NewAttribute("bounty_id", fmt.Sprintf("%d", bountyID)),
			sdk.NewAttribute("thread_id", fmt.Sprintf("%d", msg.ThreadId)),
			sdk.NewAttribute("reply_id", fmt.Sprintf("%d", msg.ReplyId)),
			sdk.NewAttribute("recipient", reply.Author),
			sdk.NewAttribute("reason", msg.Reason),
		),
	)

	return &types.MsgAssignBountyToReplyResponse{}, nil
}
