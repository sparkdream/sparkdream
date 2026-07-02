package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/collect/types"
)

// EnqueueMemberForPromotion adds `addr` to the promotion queue, stamping the
// current block height. Idempotent — re-enqueuing overwrites the height.
// No-op for empty strings or module-account addresses (which cannot become
// members and so should never be queued).
func (k Keeper) EnqueueMemberForPromotion(ctx context.Context, addr string) error {
	if addr == "" {
		return nil
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	return k.PromotionQueue.Set(ctx, addr, sdkCtx.BlockHeight())
}

// IsMemberQueuedForPromotion reports whether `addr` is currently in the
// promotion queue. Exposed for tests.
func (k Keeper) IsMemberQueuedForPromotion(ctx context.Context, addr string) bool {
	has, _ := k.PromotionQueue.Has(ctx, addr)
	return has
}

// drainPromotionQueue processes up to `budget` units of work across all
// queued addresses. A unit of work is either:
//   - releasing one inviter's locked DREAM (and clearing the stake on the
//     collaborator record), for a collaborator record where the queued
//     address is the collaborator, or
//   - flipping one ephemeral collection owned by the queued address to
//     permanent.
//
// Addresses whose pending work is fully drained are removed from the queue.
// Partial progress is preserved per-address: if the budget is exhausted
// mid-address the address stays in the queue and the next block resumes
// where this one stopped. Returns the count of work units actually completed.
func (k Keeper) drainPromotionQueue(ctx context.Context, budget int) int {
	if budget <= 0 {
		return 0
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Snapshot queue keys so we can safely mutate the map mid-walk.
	var addrs []string
	_ = k.PromotionQueue.Walk(ctx, nil, func(addr string, _ int64) (bool, error) {
		addrs = append(addrs, addr)
		return false, nil
	})

	done := 0
	for _, addr := range addrs {
		if done >= budget {
			return done
		}
		drained := k.drainPromotionForAddress(ctx, sdkCtx, addr, &done, budget)
		if drained {
			_ = k.PromotionQueue.Remove(ctx, addr)
		}
	}
	return done
}

// drainPromotionForAddress runs the two passes for a single address in order:
// inviter-stake refunds first, then owned-collection promotions. Returns true
// iff *both* passes completed without hitting the budget — i.e. the address
// has no remaining promotion work and can be removed from the queue.
func (k Keeper) drainPromotionForAddress(
	ctx context.Context,
	sdkCtx sdk.Context,
	addr string,
	done *int,
	budget int,
) bool {
	stakeDrained := k.releaseCollabStakesForNewMember(ctx, sdkCtx, addr, done, budget)
	if !stakeDrained {
		return false
	}
	return k.promoteOwnedEphemerals(ctx, sdkCtx, addr, done, budget)
}

// releaseCollabStakesForNewMember walks CollaboratorReverse prefixed by `addr`
// and, for every collaborator record where the new member carries a non-zero
// inviter stake, unlocks the inviter's DREAM (full refund — admission isn't
// misconduct), clears the inviter / dream_stake fields, and decrements the
// collection's non-member counter. Returns true iff the pass completed
// without hitting the budget.
func (k Keeper) releaseCollabStakesForNewMember(
	ctx context.Context,
	sdkCtx sdk.Context,
	addr string,
	done *int,
	budget int,
) bool {
	// Snapshot (addr, collectionID) entries so we don't mutate while walking.
	var collectionIDs []uint64
	rng := collections.NewPrefixedPairRange[string, uint64](addr)
	_ = k.CollaboratorReverse.Walk(ctx, rng, func(key collections.Pair[string, uint64]) (bool, error) {
		collectionIDs = append(collectionIDs, key.K2())
		return false, nil
	})

	for _, collID := range collectionIDs {
		if *done >= budget {
			return false
		}
		k.releaseOneCollabStake(ctx, sdkCtx, addr, collID, done)
	}
	return true
}

// releaseOneCollabStake settles a single collaborator record's locked stake
// and clears the bookkeeping. Best-effort: missing records, missing
// collections, and rep-keeper failures are all logged-and-skipped so the
// drain pass never aborts mid-flight.
func (k Keeper) releaseOneCollabStake(
	ctx context.Context,
	sdkCtx sdk.Context,
	addr string,
	collID uint64,
	done *int,
) {
	compositeKey := CollaboratorCompositeKey(collID, addr)
	collab, err := k.Collaborator.Get(ctx, compositeKey)
	if err != nil {
		return
	}
	// Member-collaborator (no stake) — nothing to settle.
	if collab.DreamStake.IsNil() || !collab.DreamStake.IsPositive() {
		return
	}
	if collab.Inviter == "" {
		return
	}
	inviterAddr, addrErr := k.addressCodec.StringToBytes(collab.Inviter)
	if addrErr != nil {
		return
	}

	stake := collab.DreamStake
	if unlockErr := k.repKeeper.UnlockDREAM(ctx, inviterAddr, stake); unlockErr != nil {
		sdkCtx.Logger().Error("failed to unlock inviter DREAM on member admission",
			"collection_id", collID, "inviter", collab.Inviter, "address", addr, "error", unlockErr)
		// Don't advance bookkeeping — leave the record so retry / manual
		// reconciliation is possible.
		return
	}

	// Clear stake bookkeeping on the collaborator record. The address remains
	// a collaborator (now as a normal member); the inviter's "training
	// wheels" link is gone.
	collab.Inviter = ""
	collab.DreamStake = collab.DreamStake.Sub(stake) // -> zero
	if setErr := k.Collaborator.Set(ctx, compositeKey, collab); setErr != nil {
		sdkCtx.Logger().Error("failed to clear collaborator stake after refund",
			"collection_id", collID, "address", addr, "error", setErr)
		return
	}

	// Decrement the collection's non-member counter.
	if coll, getErr := k.Collection.Get(ctx, collID); getErr == nil {
		if coll.NonMemberCollaboratorCount > 0 {
			coll.NonMemberCollaboratorCount--
			_ = k.Collection.Set(ctx, collID, coll)
		}
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("collect.collab_stake.released",
		sdk.NewAttribute("collection_id", fmt.Sprintf("%d", collID)),
		sdk.NewAttribute("address", addr),
		sdk.NewAttribute("inviter", collab.Inviter),
		sdk.NewAttribute("dream_refunded", stake.String()),
		sdk.NewAttribute("via", "member_admitted"),
	))
	*done++
}

// promoteOwnedEphemerals walks CollectionsByOwner prefixed by `addr` and
// flips each ephemeral collection (PENDING or ACTIVE+TTL) to permanent. The
// HIDDEN-at-admission skip rule applies — those wait for the moderation
// cycle to resolve. Returns true iff the pass completed without hitting the
// budget.
func (k Keeper) promoteOwnedEphemerals(
	ctx context.Context,
	sdkCtx sdk.Context,
	addr string,
	done *int,
	budget int,
) bool {
	var collectionIDs []uint64
	rng := collections.NewPrefixedPairRange[string, uint64](addr)
	_ = k.CollectionsByOwner.Walk(ctx, rng, func(key collections.Pair[string, uint64]) (bool, error) {
		collectionIDs = append(collectionIDs, key.K2())
		return false, nil
	})

	for _, collID := range collectionIDs {
		if *done >= budget {
			return false
		}
		k.promoteOneOwnedEphemeral(ctx, sdkCtx, addr, collID, done)
	}
	return true
}

// promoteOneOwnedEphemeral promotes a single ephemeral collection to
// permanent. Encapsulates all four cases (PENDING+TTL, ACTIVE+TTL,
// ACTIVE+TTL+immutable-endorsed, already-permanent skip, HIDDEN skip).
// Best-effort: failures are logged and the work unit is not counted.
func (k Keeper) promoteOneOwnedEphemeral(
	ctx context.Context,
	sdkCtx sdk.Context,
	addr string,
	collID uint64,
	done *int,
) {
	coll, err := k.Collection.Get(ctx, collID)
	if err != nil {
		return
	}
	if coll.Owner != addr {
		// Stale index entry — drop it.
		_ = k.CollectionsByOwner.Remove(ctx, collections.Join(addr, collID))
		return
	}
	// Already permanent → nothing to do; this is the lazy idempotent path.
	if coll.ExpiresAt == 0 {
		return
	}
	// HIDDEN at admission — defer until moderation resolves so we don't
	// accidentally rehabilitate a hidden collection by flipping it to
	// permanent.
	if coll.Status == types.CollectionStatus_COLLECTION_STATUS_HIDDEN {
		return
	}

	params, paramsErr := k.Params.Get(ctx)
	if paramsErr != nil {
		return
	}

	// Snapshot state we need for post-update cleanup.
	oldExpiresAt := coll.ExpiresAt
	wasPending := coll.Status == types.CollectionStatus_COLLECTION_STATUS_PENDING
	wasEndorsed := coll.EndorsedBy != ""

	// Burn the escrowed collection deposit + per-item deposits, matching
	// MsgMakeCollectionPermanent. If the collection already had its
	// collection-deposit burned (rare on ephemerals but possible), only the
	// item deposits remain to burn.
	var depositBurn = coll.ItemDepositTotal
	if !coll.DepositBurned {
		depositBurn = depositBurn.Add(coll.DepositAmount)
	}
	if depositBurn.IsPositive() {
		if burnErr := k.BurnSPARK(ctx, depositBurn); burnErr != nil {
			sdkCtx.Logger().Error("failed to burn deposits on auto-promotion",
				"collection_id", collID, "amount", depositBurn.String(), "error", burnErr)
			return
		}
	}

	// Flip status / TTL / deposit-burned. Endorsed (immutable) collections
	// keep the immutable flag — promotion doesn't reopen edits, just
	// preserves the collection.
	coll.Status = types.CollectionStatus_COLLECTION_STATUS_ACTIVE
	coll.ExpiresAt = 0
	coll.DepositBurned = true
	coll.SeekingEndorsement = false
	if coll.ConvictionSustained {
		coll.ConvictionSustained = false
	}
	if setErr := k.Collection.Set(ctx, collID, coll); setErr != nil {
		sdkCtx.Logger().Error("failed to set promoted collection",
			"collection_id", collID, "error", setErr)
		return
	}

	// CollectionsByExpiry index — remove the entry now that TTL is cleared.
	_ = k.CollectionsByExpiry.Remove(ctx, collections.Join(oldExpiresAt, collID))

	// CollectionsByStatus index — if we just transitioned PENDING→ACTIVE,
	// swap the index entry.
	if wasPending {
		// A pending collection is never pinned, so rank is unchanged (false).
		_ = k.MoveCollectionStatusIndex(ctx,
			types.CollectionStatus_COLLECTION_STATUS_PENDING, false,
			types.CollectionStatus_COLLECTION_STATUS_ACTIVE, false, collID)
	}

	// PENDING-specific cleanup: refund the endorsement creation fee (the
	// endorsement vouch is no longer needed) and remove from the
	// EndorsementPending index.
	if wasPending {
		ownerAddr, ownerErr := k.addressCodec.StringToBytes(addr)
		if ownerErr == nil && params.EndorsementCreationFee.IsPositive() {
			if refundErr := k.RefundSPARK(ctx, ownerAddr, params.EndorsementCreationFee); refundErr != nil {
				sdkCtx.Logger().Error("failed to refund endorsement creation fee on auto-promotion",
					"collection_id", collID, "owner", addr, "error", refundErr)
			}
		}
		// Compute the pending key directly using CreatedAt + expiry blocks.
		expiryBlock := coll.CreatedAt + params.EndorsementExpiryBlocks
		_ = k.EndorsementPending.Remove(ctx, collections.Join(expiryBlock, collID))
	}

	// Endorsed collections: release the endorser's locked stake. The
	// endorsement was insurance for the owner's pre-member status; that
	// status is gone, so the endorser's lock is too.
	if wasEndorsed {
		if endorsement, endErr := k.Endorsement.Get(ctx, collID); endErr == nil && !endorsement.StakeReleased {
			endorserAddr, addrErr := k.addressCodec.StringToBytes(endorsement.Endorser)
			if addrErr == nil && endorsement.DreamStake.IsPositive() {
				if unlockErr := k.repKeeper.UnlockDREAM(ctx, endorserAddr, endorsement.DreamStake); unlockErr != nil {
					sdkCtx.Logger().Error("failed to unlock endorser DREAM on auto-promotion",
						"collection_id", collID, "endorser", endorsement.Endorser, "error", unlockErr)
				}
			}
			endorsement.StakeReleased = true
			_ = k.Endorsement.Set(ctx, collID, endorsement)
			_ = k.EndorsementStakeExpiry.Remove(ctx, collections.Join(endorsement.StakeReleaseAt, collID))
		}
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("collect.collection.upgraded",
		sdk.NewAttribute("collection_id", fmt.Sprintf("%d", collID)),
		sdk.NewAttribute("owner", addr),
		sdk.NewAttribute("via", "promotion_queue"),
		sdk.NewAttribute("deposit_burned", depositBurn.String()),
	))
	*done++
}
