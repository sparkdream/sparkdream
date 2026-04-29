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
	if post.Author != msg.Creator {
		return nil, errorsmod.Wrap(types.ErrNotThreadAuthor, "only the thread author can create a bounty")
	}

	// Check no existing active bounty for this thread (O(1) lookup via secondary index)
	if existingBountyID, err := k.ActiveBountyByThread.Get(ctx, msg.ThreadId); err == nil {
		// Verify the bounty is still active (defensive check)
		if existingBounty, err := k.Bounty.Get(ctx, existingBountyID); err == nil {
			if existingBounty.Status == types.BountyStatus_BOUNTY_STATUS_ACTIVE {
				return nil, types.ErrBountyAlreadyExists
			}
		}
		// Stale index entry — clean it up
		_ = k.ActiveBountyByThread.Remove(ctx, msg.ThreadId)
	}

	// Parse and validate amount
	amount, ok := math.NewIntFromString(msg.Amount)
	if !ok || amount.IsNegative() || amount.IsZero() {
		return nil, errorsmod.Wrap(types.ErrInvalidAmount, "invalid bounty amount")
	}

	minBountyAmount := math.NewInt(50) // DefaultMinBountyAmount
	if amount.LT(minBountyAmount) {
		return nil, errorsmod.Wrapf(types.ErrBountyAmountTooSmall,
			"minimum bounty is %s SPARK", minBountyAmount.String())
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
	escrowCoins := sdk.NewCoins(sdk.NewCoin(types.DefaultFeeDenom, amount))
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
