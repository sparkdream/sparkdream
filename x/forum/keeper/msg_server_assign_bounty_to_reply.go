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

	// Calculate per-winner share: total bounty divided equally among max_winners.
	// This is a winner-take-equal-share model: each assignment receives the same
	// fraction (1/max_winners) of the total bounty.
	totalAmount, ok := math.NewIntFromString(bounty.Amount)
	if !ok {
		return nil, errorsmod.Wrap(types.ErrInvalidAmount, "invalid bounty amount")
	}
	perWinnerAmount := totalAmount.Quo(math.NewInt(int64(types.DefaultMaxBountyWinners)))
	if !perWinnerAmount.IsPositive() {
		return nil, errorsmod.Wrap(types.ErrInvalidAmount, "bounty amount too small to split among winners")
	}

	// Verify remaining funds cover another award
	assignedAmount := math.ZeroInt()
	for _, a := range bounty.Awards {
		awardAmt, ok := math.NewIntFromString(a.Amount)
		if ok {
			assignedAmount = assignedAmount.Add(awardAmt)
		}
	}
	remainingAmount := totalAmount.Sub(assignedAmount)
	if remainingAmount.LT(perWinnerAmount) {
		return nil, errorsmod.Wrap(types.ErrInvalidAmount, "no remaining bounty funds to assign")
	}

	// Create award with per-winner share
	award := &types.BountyAward{
		PostId:    msg.ReplyId,
		Recipient: reply.Author,
		Amount:    perWinnerAmount.String(),
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
			sdk.NewAttribute("amount", award.Amount),
			sdk.NewAttribute("reason", msg.Reason),
		),
	)

	return &types.MsgAssignBountyToReplyResponse{}, nil
}
