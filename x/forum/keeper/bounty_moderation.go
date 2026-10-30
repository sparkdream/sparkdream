package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/forum/types"
)

// reconcileThreadBounty keeps a thread's open bounty in step with the
// moderation state of its root post (docs/x-forum-spec.md "Bounty Behavior on
// Thread Moderation"). Every handler that hides, unhides, locks or unlocks a
// root, and every path that settles such an action (expiry, appeal verdict,
// reversal), calls it afterwards with the thread id. It is a no-op for a
// post id that is not a thread with an open bounty.
//
//   - ACTIVE bounty, root hidden or locked with the action still open
//     (hide / lock record present): the timer is suspended
//     (MODERATION_PENDING, remaining time captured, expiry index dropped).
//   - MODERATION_PENDING, root no longer hidden or locked: the timer resumes
//     with the captured remaining time, no fee.
//   - Moderation stood (root tombstoned or gone, or hidden / locked with the
//     record settled): the bounty is cancelled, BountyModerationFeeBps of the
//     escrow is burned and the rest refunded to the creator.
func (k Keeper) reconcileThreadBounty(ctx context.Context, threadID uint64) error {
	bountyID, err := k.ActiveBountyByThread.Get(ctx, threadID)
	if err != nil {
		return nil
	}
	bounty, err := k.Bounty.Get(ctx, bountyID)
	if err != nil {
		return nil
	}
	if bounty.Status != types.BountyStatus_BOUNTY_STATUS_ACTIVE &&
		bounty.Status != types.BountyStatus_BOUNTY_STATUS_MODERATION_PENDING {
		return nil
	}

	root, rootErr := k.Post.Get(ctx, threadID)
	gone := rootErr != nil || root.Status == types.PostStatus_POST_STATUS_DELETED
	hidden := rootErr == nil && root.Status == types.PostStatus_POST_STATUS_HIDDEN
	locked := rootErr == nil && root.Locked
	hideOpen, lockOpen := false, false
	if hidden {
		var getErr error
		hideOpen, getErr = k.HideRecord.Has(ctx, threadID)
		if getErr != nil {
			return getErr
		}
	}
	if locked {
		var getErr error
		lockOpen, getErr = k.ThreadLockRecord.Has(ctx, threadID)
		if getErr != nil {
			return getErr
		}
	}
	// A settled action stands even if the other one is still open: a hide
	// that stood tombstones the root, and a lock that stood keeps the thread
	// closed to answers for good.
	stood := gone || (hidden && !hideOpen) || (locked && !lockOpen)
	open := hideOpen || lockOpen

	switch {
	case stood && bounty.Status == types.BountyStatus_BOUNTY_STATUS_MODERATION_PENDING,
		stood && (hidden || locked):
		return k.cancelModeratedBounty(ctx, &bounty)
	case (hidden || locked) && open && bounty.Status == types.BountyStatus_BOUNTY_STATUS_ACTIVE:
		return k.suspendBounty(ctx, &bounty)
	case !hidden && !locked && !gone && bounty.Status == types.BountyStatus_BOUNTY_STATUS_MODERATION_PENDING:
		return k.resumeBounty(ctx, &bounty)
	}
	return nil
}

// suspendBounty pauses an ACTIVE bounty's timer.
func (k Keeper) suspendBounty(ctx context.Context, bounty *types.Bounty) error {
	now := sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
	remaining := bounty.ExpiresAt - now
	if remaining < 0 {
		remaining = 0
	}
	if err := k.BountiesByExpiry.Remove(ctx, collections.Join(bounty.ExpiresAt, bounty.Id)); err != nil {
		return err
	}
	bounty.Status = types.BountyStatus_BOUNTY_STATUS_MODERATION_PENDING
	bounty.ModerationSuspendedAt = now
	bounty.TimeRemainingAtSuspension = remaining
	if err := k.Bounty.Set(ctx, bounty.Id, *bounty); err != nil {
		return err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("bounty_moderation_suspended",
		sdk.NewAttribute("bounty_id", fmt.Sprintf("%d", bounty.Id)),
		sdk.NewAttribute("thread_id", fmt.Sprintf("%d", bounty.ThreadId)),
		sdk.NewAttribute("time_remaining", fmt.Sprintf("%d", remaining)),
	))
	return nil
}

// resumeBounty restarts a suspended bounty's timer with the time it had left.
func (k Keeper) resumeBounty(ctx context.Context, bounty *types.Bounty) error {
	now := sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
	bounty.Status = types.BountyStatus_BOUNTY_STATUS_ACTIVE
	bounty.ExpiresAt = now + bounty.TimeRemainingAtSuspension
	bounty.ModerationSuspendedAt = 0
	bounty.TimeRemainingAtSuspension = 0
	if err := k.Bounty.Set(ctx, bounty.Id, *bounty); err != nil {
		return err
	}
	if err := k.BountiesByExpiry.Set(ctx, collections.Join(bounty.ExpiresAt, bounty.Id)); err != nil {
		return err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("bounty_moderation_resumed",
		sdk.NewAttribute("bounty_id", fmt.Sprintf("%d", bounty.Id)),
		sdk.NewAttribute("thread_id", fmt.Sprintf("%d", bounty.ThreadId)),
		sdk.NewAttribute("expires_at", fmt.Sprintf("%d", bounty.ExpiresAt)),
	))
	return nil
}

// cancelModeratedBounty settles a bounty whose thread moderation stood. Awards
// the creator already assigned are a commitment (an ordinary cancel is
// refused once they exist): they are paid exactly as at expiry, splitting the
// escrow among the still-eligible ones (FRM-34; otherwise a moderation that
// stood, possibly the creator's own sentinel lock, refunded the creator and
// left the answerers unpaid). With no eligible award the moderation fee is
// burned and the rest refunded to the creator.
func (k Keeper) cancelModeratedBounty(ctx context.Context, bounty *types.Bounty) error {
	amount, ok := math.NewIntFromString(bounty.Amount)
	if !ok || amount.IsNegative() {
		return errorsInvalidEscrow(bounty.Id)
	}
	if len(bounty.Awards) > 0 {
		// Pay in a cache context: a failed transfer leaves nothing half-paid
		// and falls back to the refund below, as expiry does.
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		cacheCtx, write := sdkCtx.CacheContext()
		paid, err := k.payBountyAwards(cacheCtx, bounty)
		if err != nil {
			sdkCtx.Logger().Error("failed to pay moderated bounty awards; refunding", "bounty_id", bounty.Id, "error", err)
			paid = 0
		} else if paid > 0 {
			write()
		}
		if paid > 0 {
			if err := k.BountiesByExpiry.Remove(ctx, collections.Join(bounty.ExpiresAt, bounty.Id)); err != nil {
				return err
			}
			bounty.Status = types.BountyStatus_BOUNTY_STATUS_AWARDED
			bounty.ModerationSuspendedAt = 0
			bounty.TimeRemainingAtSuspension = 0
			if err := k.Bounty.Set(ctx, bounty.Id, *bounty); err != nil {
				return err
			}
			k.clearActiveBountyIndex(ctx, *bounty)
			sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("bounty_moderation_awarded",
				sdk.NewAttribute("bounty_id", fmt.Sprintf("%d", bounty.Id)),
				sdk.NewAttribute("thread_id", fmt.Sprintf("%d", bounty.ThreadId)),
				sdk.NewAttribute("awards_paid", fmt.Sprintf("%d", paid)),
			))
			return nil
		}
	}
	fee := amount.Mul(math.NewIntFromUint64(types.BountyModerationFeeBps)).Quo(math.NewInt(10000))
	if err := k.burnAndRefundBounty(ctx, bounty, fee, amount.Sub(fee)); err != nil {
		return err
	}
	if err := k.BountiesByExpiry.Remove(ctx, collections.Join(bounty.ExpiresAt, bounty.Id)); err != nil {
		return err
	}
	bounty.Status = types.BountyStatus_BOUNTY_STATUS_CANCELLED
	bounty.ModerationSuspendedAt = 0
	bounty.TimeRemainingAtSuspension = 0
	if err := k.Bounty.Set(ctx, bounty.Id, *bounty); err != nil {
		return err
	}
	k.clearActiveBountyIndex(ctx, *bounty)
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("bounty_moderation_cancelled",
		sdk.NewAttribute("bounty_id", fmt.Sprintf("%d", bounty.Id)),
		sdk.NewAttribute("thread_id", fmt.Sprintf("%d", bounty.ThreadId)),
		sdk.NewAttribute("fee_burned", fee.String()),
		sdk.NewAttribute("refunded", amount.Sub(fee).String()),
	))
	return nil
}

// burnAndRefundBounty burns `burn` of the escrow and refunds `refund` to the
// bounty creator.
func (k Keeper) burnAndRefundBounty(ctx context.Context, bounty *types.Bounty, burn, refund math.Int) error {
	denom := k.BondDenom(ctx)
	if burn.IsPositive() {
		if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin(denom, burn))); err != nil {
			return fmt.Errorf("burn bounty %d fee: %w", bounty.Id, err)
		}
	}
	if refund.IsPositive() {
		creator, err := sdk.AccAddressFromBech32(bounty.Creator)
		if err != nil {
			return fmt.Errorf("bounty %d creator: %w", bounty.Id, err)
		}
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, creator, sdk.NewCoins(sdk.NewCoin(denom, refund))); err != nil {
			return fmt.Errorf("refund bounty %d: %w", bounty.Id, err)
		}
	}
	return nil
}

// topBountyReply returns the most-upvoted eligible reply of the bounty's
// thread: ACTIVE, at least one upvote, not anonymous, and not written by the
// bounty creator (alias-aware). Ties go to the earliest reply. At most
// MaxBountyTopReplyScan replies are read.
func (k Keeper) topBountyReply(ctx context.Context, bounty types.Bounty) (types.Post, bool) {
	ids, err := k.threadReplyIDs(ctx, bounty.ThreadId, types.MaxBountyTopReplyScan)
	if err != nil {
		return types.Post{}, false
	}
	if uint64(len(ids)) > types.MaxBountyTopReplyScan {
		ids = ids[:types.MaxBountyTopReplyScan]
	}
	var best types.Post
	found := false
	for _, id := range ids {
		reply, err := k.Post.Get(ctx, id)
		if err != nil || reply.Status != types.PostStatus_POST_STATUS_ACTIVE || reply.UpvoteCount == 0 {
			continue
		}
		if k.isAnonymous(reply.Author) || k.sameMember(ctx, reply.Author, bounty.Creator) {
			continue
		}
		if !found || reply.UpvoteCount > best.UpvoteCount {
			best, found = reply, true
		}
	}
	return best, found
}

// settleUnawardedBounty closes an expired bounty that has no payable award.
// With an eligible top reply, BountyExpiryBurnBps of the escrow is burned and
// the rest paid to that reply's author (recorded as the bounty's award).
// Without one, the escrow is refunded minus the cancellation fee, so letting
// a bounty lapse is never cheaper than cancelling it. Returns the outcome
// label for the event.
func (k Keeper) settleUnawardedBounty(ctx context.Context, bounty *types.Bounty) (string, error) {
	amount, ok := math.NewIntFromString(bounty.Amount)
	if !ok || amount.IsNegative() {
		return "", errorsInvalidEscrow(bounty.Id)
	}
	if top, ok := k.topBountyReply(ctx, *bounty); ok {
		burn := amount.Mul(math.NewIntFromUint64(types.BountyExpiryBurnBps)).Quo(math.NewInt(10000))
		pay := amount.Sub(burn)
		if burn.IsPositive() {
			if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), burn))); err != nil {
				return "", fmt.Errorf("burn expired bounty %d: %w", bounty.Id, err)
			}
		}
		if pay.IsPositive() {
			recipient, err := sdk.AccAddressFromBech32(top.Author)
			if err != nil {
				return "", fmt.Errorf("bounty %d top reply author: %w", bounty.Id, err)
			}
			if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, recipient, sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), pay))); err != nil {
				return "", fmt.Errorf("pay expired bounty %d to top reply: %w", bounty.Id, err)
			}
		}
		bounty.Awards = append(bounty.Awards, &types.BountyAward{
			PostId:    top.PostId,
			Recipient: top.Author,
			Amount:    pay.String(),
			Reason:    "expiry: top reply",
			AwardedAt: sdk.UnwrapSDKContext(ctx).BlockTime().Unix(),
		})
		return "burned_and_top_reply", nil
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		params = types.DefaultParams()
	}
	feePercent := params.BountyCancellationFeePercent
	if feePercent > 100 {
		feePercent = 100
	}
	fee := amount.Mul(math.NewIntFromUint64(feePercent)).Quo(math.NewInt(100))
	if err := k.burnAndRefundBounty(ctx, bounty, fee, amount.Sub(fee)); err != nil {
		return "", err
	}
	return "refunded", nil
}

// bountyRootEligible reports whether a root post can carry a new bounty: it
// is live (ACTIVE status) and unlocked. Moderation reconciliation suspends a
// bounty whose root stops satisfying this.
func bountyRootEligible(post types.Post) bool {
	return post.Status == types.PostStatus_POST_STATUS_ACTIVE && !post.Locked
}
