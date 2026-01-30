package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) AssignBountyToReply(ctx context.Context, msg *types.MsgAssignBountyToReply) (*types.MsgAssignBountyToReplyResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// Find bounty for thread
	var bounty types.Bounty
	var bountyID uint64
	var found bool

	iter, err := k.Bounty.Iterate(ctx, nil)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to iterate bounties")
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		b, _ := iter.Value()
		if b.ThreadId == msg.ThreadId && b.Status == types.BountyStatus_BOUNTY_STATUS_ACTIVE {
			bounty = b
			bountyID = b.Id
			found = true
			break
		}
	}

	if !found {
		return nil, errorsmod.Wrap(types.ErrBountyNotFound, fmt.Sprintf("no active bounty for thread %d", msg.ThreadId))
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

	// Create award (using full remaining amount for simplicity)
	award := &types.BountyAward{
		PostId:    msg.ReplyId,
		Recipient: reply.Author,
		Amount:    bounty.Amount, // Full amount - would split in production
		Reason:    msg.Reason,
		AwardedAt: now,
		Rank:      uint32(len(bounty.Awards) + 1),
	}

	bounty.Awards = append(bounty.Awards, award)

	// Transfer SPARK from escrow to recipient
	awardAmount, ok := math.NewIntFromString(award.Amount)
	if ok && awardAmount.IsPositive() {
		recipientAddr, _ := sdk.AccAddressFromBech32(reply.Author)
		awardCoins := sdk.NewCoins(sdk.NewCoin(types.DefaultFeeDenom, awardAmount))
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, recipientAddr, awardCoins); err != nil {
			return nil, errorsmod.Wrap(err, "failed to transfer bounty award")
		}
	}

	// Mark bounty as awarded
	bounty.Status = types.BountyStatus_BOUNTY_STATUS_AWARDED

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
			sdk.NewAttribute("amount", bounty.Amount),
			sdk.NewAttribute("reason", msg.Reason),
		),
	)

	return &types.MsgAssignBountyToReplyResponse{}, nil
}
