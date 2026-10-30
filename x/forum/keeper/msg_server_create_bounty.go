package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) CreateBounty(ctx context.Context, msg *types.MsgCreateBounty) (*types.MsgCreateBountyResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}
	if err := k.requireForumNotPaused(ctx); err != nil {
		return nil, err
	}

	// A bounty escrows real SPARK from its creator. From the shield address
	// that would be the communal gas reserve, and every anonymous member
	// shares that address, so any of them could cancel or award it.
	if k.isAnonymous(msg.Creator) {
		return nil, types.ErrAnonymousBounty
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// Check bounties_enabled param
	params, err := k.Params.Get(ctx)
	if err != nil {
		params = types.DefaultParams()
	}
	if !params.BountiesEnabled {
		return nil, types.ErrBountiesDisabled
	}

	// Load thread (root post)
	post, err := k.Post.Get(ctx, msg.ThreadId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("thread %d not found", msg.ThreadId))
	}

	// Check this is a root post
	if post.ParentId != 0 {
		return nil, types.ErrNotRootPost
	}

	// Verify creator is the thread author
	if !k.canActAs(ctx, msg.Creator, post.Author) {
		return nil, errorsmod.Wrap(types.ErrNotThreadAuthor, "only the thread author can create a bounty")
	}
	// Spec: the creator must be a member (FRM-29).
	if !k.IsActiveMember(ctx, msg.Creator) {
		return nil, errorsmod.Wrap(types.ErrNotMember, "only active members can create bounties")
	}
	// A bounty on a hidden, locked, deleted or archived root starts life
	// suspended or unreachable (FRM-29, FRM-25).
	if !bountyRootEligible(post) {
		return nil, errorsmod.Wrapf(types.ErrBountyRootIneligible, "thread %d is %s (locked=%t)", msg.ThreadId, post.Status, post.Locked)
	}

	// At most one open bounty per thread (O(1) lookup via secondary index).
	// A suspended (MODERATION_PENDING) bounty is still open: replacing it
	// would drop its only index entry and strand its escrow (FRM-25).
	if existingBountyID, err := k.ActiveBountyByThread.Get(ctx, msg.ThreadId); err == nil {
		if existingBounty, err := k.Bounty.Get(ctx, existingBountyID); err == nil && existingBounty.IsOpen() {
			return nil, types.ErrBountyAlreadyExists
		}
		// Stale index entry (the bounty is settled or gone) — clean it up
		if err := k.ActiveBountyByThread.Remove(ctx, msg.ThreadId); err != nil {
			return nil, err
		}
	}

	// Parse and validate amount
	amount, ok := math.NewIntFromString(msg.Amount)
	if !ok || amount.IsNegative() || amount.IsZero() {
		return nil, errorsmod.Wrap(types.ErrInvalidAmount, "invalid bounty amount")
	}

	minBountyAmount := math.NewInt(types.MinBountyAmount)
	if amount.LT(minBountyAmount) {
		return nil, errorsmod.Wrapf(types.ErrBountyAmountTooSmall,
			"minimum bounty is %s (bond denom base units)", minBountyAmount.String())
	}

	// Validate duration
	duration := msg.Duration
	if duration <= 0 {
		duration = types.DefaultBountyDuration
	}
	if duration > types.DefaultMaxBountyDuration {
		return nil, errorsmod.Wrapf(types.ErrInvalidBountyDuration,
			"max duration is %d seconds", types.DefaultMaxBountyDuration)
	}

	// Transfer SPARK from creator to module (escrow)
	creatorAddr, _ := sdk.AccAddressFromBech32(msg.Creator)
	escrowCoins := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), amount))
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, creatorAddr, types.ModuleName, escrowCoins); err != nil {
		return nil, errorsmod.Wrap(err, "failed to escrow bounty funds")
	}

	// Generate bounty ID
	bountyID, err := k.BountySeq.Next(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to generate bounty ID")
	}

	// Create bounty
	bounty := types.Bounty{
		Id:        bountyID,
		Creator:   msg.Creator,
		ThreadId:  msg.ThreadId,
		Amount:    msg.Amount,
		CreatedAt: now,
		ExpiresAt: now + duration,
		Status:    types.BountyStatus_BOUNTY_STATUS_ACTIVE,
		Awards:    []*types.BountyAward{},
	}

	if err := k.Bounty.Set(ctx, bountyID, bounty); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store bounty")
	}

	// Update secondary index for O(1) thread-to-bounty lookup
	if err := k.ActiveBountyByThread.Set(ctx, msg.ThreadId, bountyID); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update bounty thread index")
	}
	// FORUM-S2-8: indexes for paginated UserBounties / BountyExpiringSoon.
	if err := k.BountiesByCreator.Set(ctx, collections.Join(msg.Creator, bountyID)); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update bounties-by-creator index")
	}
	if err := k.BountiesByExpiry.Set(ctx, collections.Join(bounty.ExpiresAt, bountyID)); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update bounties-by-expiry index")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"bounty_created",
			sdk.NewAttribute("bounty_id", fmt.Sprintf("%d", bountyID)),
			sdk.NewAttribute("thread_id", fmt.Sprintf("%d", msg.ThreadId)),
			sdk.NewAttribute("creator", msg.Creator),
			sdk.NewAttribute("amount", msg.Amount),
			sdk.NewAttribute("expires_at", fmt.Sprintf("%d", bounty.ExpiresAt)),
		),
	)

	return &types.MsgCreateBountyResponse{}, nil
}
