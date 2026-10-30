package keeper

import (
	"context"
	"fmt"
	commontypes "sparkdream/x/common/types"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// MaxDisputesResolvedPerBlock bounds the BeginBlock work; the rest waits for
// the next block.
const MaxDisputesResolvedPerBlock = 50

// BeginBlocker processes begin block logic for the x/name module.
func (k Keeper) BeginBlocker(ctx context.Context) error {
	return k.processExpiredDisputes(ctx)
}

// processExpiredDisputes handles uncontested disputes whose contest window
// has closed. It walks only the PendingDisputes index (active, uncontested
// disputes by filing height), never resolved ones.
//
//   - If the owner is expired (no x/name activity within
//     expiration_duration) the dispute is upheld: the name transfers to the
//     claimant and the claimant's stake is returned. A claimant who is no
//     longer an active member or is at the name cap gets the stake back and
//     the dispute is voided.
//   - If the owner is still active, silence is not consent (an owner may
//     simply lack the contest stake): the dispute leaves the auto-resolution
//     queue and waits for the Commons Council or its Operations Committee to
//     decide it with MsgResolveDispute (NAM-03).
//
// Each resolution runs in a cache context and commits only on success. A
// failure closes the dispute without a transfer and returns the claimant's
// stake instead of retrying it every block with half-applied writes
// (NAM-04). If the stake cannot be returned either, the dispute stays
// active with its DisputeStakes row (the still-locked DREAM stays
// accounted for) and leaves the queue for MsgResolveDispute.
//
// Contested disputes are never auto-resolved; they wait for MsgResolveDispute.
func (k Keeper) processExpiredDisputes(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentHeight := sdkCtx.BlockHeight()
	timeout := commontypes.Saturate[int64](k.GetParams(ctx).DisputeTimeoutBlocks)

	// Due: currentHeight > filed_at + timeout  <=>  filed_at < currentHeight - timeout.
	rng := new(collections.Range[collections.Pair[int64, string]]).
		EndExclusive(collections.Join(currentHeight-timeout, ""))
	var due []collections.Pair[int64, string]
	if err := k.PendingDisputes.Walk(ctx, rng, func(key collections.Pair[int64, string]) (bool, error) {
		due = append(due, key)
		return len(due) >= MaxDisputesResolvedPerBlock, nil
	}); err != nil {
		return fmt.Errorf("failed to iterate pending disputes: %w", err)
	}

	for _, key := range due {
		name := key.K2()
		dispute, found := k.GetDispute(ctx, name)
		if !found || !dispute.Active || dispute.ContestChallengeId != "" || dispute.FiledAt != key.K1() {
			// Stale index entry.
			if err := k.PendingDisputes.Remove(ctx, key); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("processExpiredDisputes: PendingDisputes.Remove failed", "err", err)
			}
			continue
		}

		owner, ownerFound := k.GetNameOwner(ctx, name)
		if ownerFound && !k.IsOwnerExpired(sdkCtx, owner) {
			// Route to council review: drop from the auto-resolution queue,
			// keep the dispute active. The mark keeps it out of the queue
			// across a genesis import too.
			dispute.AwaitingReview = true
			if err := k.SetDispute(ctx, dispute); err != nil {
				return err
			}
			sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
				"name_dispute_needs_review",
				sdk.NewAttribute("name", name),
				sdk.NewAttribute("claimant", dispute.Claimant),
				sdk.NewAttribute("owner", owner.String()),
			))
			continue
		}

		cacheCtx, write := sdkCtx.CacheContext()
		if err := k.resolveDisputeInternal(cacheCtx, dispute, true); err != nil {
			sdkCtx.Logger().Error("failed to auto-resolve expired dispute; closing it without transfer",
				"name", name, "error", err)
			// Close the dispute so it neither retries every block nor
			// blocks the owner, returning the claimant's stake.
			closeCtx, closeWrite := sdkCtx.CacheContext()
			var unlockErr error
			if !dispute.StakeAmount.IsUint64() {
				unlockErr = fmt.Errorf("dispute stake amount too large for uint64: %s", dispute.StakeAmount)
			} else {
				unlockErr = k.dreamOps.Unlock(closeCtx, dispute.Claimant, dispute.StakeAmount.Uint64())
			}
			if unlockErr != nil {
				// The stake cannot be released either. Never drop the
				// accounting for DREAM that is still locked: the dispute
				// stays active with its DisputeStakes row and leaves the
				// auto-resolution queue, exactly like a dispute routed to
				// council review. MsgResolveDispute settles it (and the
				// stake) once the cause is fixed.
				dispute.AwaitingReview = true
				if rerr := k.SetDispute(ctx, dispute); rerr != nil {
					return rerr
				}
				sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
					"name_dispute_needs_review",
					sdk.NewAttribute("name", name),
					sdk.NewAttribute("claimant", dispute.Claimant),
					sdk.NewAttribute("reason", "auto_resolve_failed"),
					sdk.NewAttribute("error", err.Error()),
					sdk.NewAttribute("unlock_error", unlockErr.Error()),
				))
				continue
			}
			dispute.Active = false
			if serr := k.SetDispute(closeCtx, dispute); serr != nil {
				return serr
			}
			if err := k.DisputeStakes.Remove(closeCtx, fmt.Sprintf("name_dispute:%s:%d", dispute.Name, dispute.FiledAt)); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("processExpiredDisputes: DisputeStakes.Remove failed", "err", err)
			}
			closeWrite()
			sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
				"name_dispute_auto_resolve_failed",
				sdk.NewAttribute("name", name),
				sdk.NewAttribute("claimant", dispute.Claimant),
				sdk.NewAttribute("error", err.Error()),
			))
			continue
		}
		write()

		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				"name_dispute_expired_upheld",
				sdk.NewAttribute("name", name),
				sdk.NewAttribute("claimant", dispute.Claimant),
				sdk.NewAttribute("expired_at_block", fmt.Sprintf("%d", currentHeight)),
			),
		)
	}

	return nil
}
