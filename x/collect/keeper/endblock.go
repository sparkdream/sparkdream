package keeper

import (
	"context"
	"fmt"
	"strconv"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/collect/types"
	commontypes "sparkdream/x/common/types"

	reptypes "sparkdream/x/rep/types"
)

// PruneExpired prunes expired collections, sponsorship requests, unappealed hides,
// timed-out appeals, expired flags, unendorsed collections, releases endorsement stakes,
// and times out unadjudicated curation challenges. Called by the EndBlocker each
// block. These tasks share a single pruned counter capped at params.MaxPrunePerBlock.
//
// Phase 0 (membership-driven auto-promotion) runs first against its own
// independent cap (params.MaxPromotionsPerBlock) so a busy prune cycle never
// starves the promotion queue — and vice versa.
func (k Keeper) PruneExpired(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentBlock := sdkCtx.BlockHeight()

	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	// Phase 0 — drain the membership-driven promotion queue. Zero disables
	// the drain entirely (governance throttle).
	if params.MaxPromotionsPerBlock > 0 {
		k.drainPromotionQueue(ctx, int(params.MaxPromotionsPerBlock))
	}

	pruned := uint32(0)
	cap := params.MaxPrunePerBlock

	// §10.1 — TTL collection pruning
	pruned, err = k.pruneExpiredCollections(ctx, currentBlock, params, pruned, cap)
	if err != nil {
		return err
	}

	// §10.1 continued — Sponsorship request expiry
	pruned, err = k.pruneExpiredSponsorshipRequests(ctx, currentBlock, params, pruned, cap)
	if err != nil {
		return err
	}

	// §10.3 + §10.3a — Unappealed hide expiry and appeal timeout (single walk)
	pruned, err = k.pruneExpiredHideRecords(ctx, currentBlock, params, pruned, cap)
	if err != nil {
		return err
	}

	// §10.4 — Flag expiry
	pruned, err = k.pruneExpiredFlags(ctx, currentBlock, params, pruned, cap)
	if err != nil {
		return err
	}

	// §10.5 — Unendorsed collection pruning
	pruned, err = k.pruneUnendorsedCollections(ctx, currentBlock, params, pruned, cap)
	if err != nil {
		return err
	}

	// §10.6 — Endorsement stake release
	pruned, err = k.releaseExpiredEndorsementStakes(ctx, currentBlock, params, pruned, cap)
	if err != nil {
		return err
	}

	// §10.7 — Curation challenge timeout. The backfill (own fixed cap) indexes
	// challenges filed before the deadline index existed; the sweep then
	// resolves every challenge past its deadline as not adjudicated.
	k.backfillChallengeDeadlines(ctx, params)
	_, err = k.timeoutExpiredChallenges(ctx, currentBlock, params, pruned, cap)
	if err != nil {
		return err
	}

	return nil
}

// maxDeferredExpiryScan bounds how many expired-but-deferred collections (a
// hide appeal in flight) the TTL pruner may step over per block.
const maxDeferredExpiryScan uint32 = 100

// pruneExpiredCollections implements §10.1: walk CollectionsByExpiry for entries
// where expiresAt <= currentBlock, then delete via deleteCollectionFull.
func (k Keeper) pruneExpiredCollections(
	ctx context.Context,
	currentBlock int64,
	params types.Params,
	pruned, cap uint32,
) (uint32, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Collect entries to process (cannot modify store during Walk). Entries
	// deferred for an in-flight hide appeal stay at the head of the queue and
	// do not consume the prune budget, so the scan reaches up to
	// maxDeferredExpiryScan entries past the budget; otherwise a run of
	// deferred collections would block every expiry behind it.
	if pruned >= cap {
		return pruned, nil
	}
	scanLimit := (cap - pruned) + maxDeferredExpiryScan
	var entries []collections.Pair[int64, uint64]
	err := k.CollectionsByExpiry.Walk(ctx, nil, func(key collections.Pair[int64, uint64]) (bool, error) {
		if key.K1() > currentBlock || commontypes.Saturate[uint32](len(entries)) >= scanLimit {
			return true, nil // stop — keys are ordered by expiry
		}
		entries = append(entries, key)
		return false, nil
	})
	if err != nil {
		return pruned, err
	}

	for _, entry := range entries {
		if pruned >= cap {
			break
		}

		expiresAt := entry.K1()
		collID := entry.K2()

		// Try to fetch the collection
		coll, err := k.Collection.Get(ctx, collID)
		if err != nil {
			// Collection no longer exists (already deleted by owner) — remove stale index entry
			if err := k.CollectionsByExpiry.Remove(ctx, collections.Join(expiresAt, collID)); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("pruneExpiredCollections: CollectionsByExpiry.Remove failed", "err", err)
			}
			pruned++
			continue
		}

		// §10.1 Conviction-based TTL renewal: anonymous collections (owned by the
		// shield module account, which x/shield signs inner messages with) can be
		// renewed if their conviction score meets the threshold.
		if k.isAnonymous(coll.Owner) && k.late.repKeeper != nil && params.ConvictionRenewalThreshold.IsPositive() {
			// Community conviction stakes sit on the content target (as the
			// CollectionConviction query reads them); x/rep rejects the
			// author-bond target here with ErrNotContentTargetType.
			conviction, convErr := k.late.repKeeper.GetContentConviction(ctx,
				reptypes.StakeTargetType_STAKE_TARGET_COLLECTION_CONTENT, collID)
			if convErr == nil && conviction.GTE(params.ConvictionRenewalThreshold) {
				// Renew: extend TTL by conviction_renewal_period
				renewalPeriod := params.ConvictionRenewalPeriod
				if renewalPeriod <= 0 {
					// Fallback to original TTL if period not set
					renewalPeriod = coll.ExpiresAt - coll.CreatedAt
					if renewalPeriod <= 0 {
						renewalPeriod = params.MaxNonMemberTtlBlocks
					}
				}
				newExpiresAt := currentBlock + renewalPeriod

				// Update expiry index
				if err := k.CollectionsByExpiry.Remove(ctx, collections.Join(expiresAt, collID)); err != nil {
					sdk.UnwrapSDKContext(ctx).Logger().Error("pruneExpiredCollections: CollectionsByExpiry.Remove failed", "err", err)
				}
				if err := k.CollectionsByExpiry.Set(ctx, collections.Join(newExpiresAt, collID)); err != nil {
					sdk.UnwrapSDKContext(ctx).Logger().Error("pruneExpiredCollections: CollectionsByExpiry.Set failed", "err", err)
				}

				// Update collection
				wasSustained := coll.ConvictionSustained
				coll.ExpiresAt = newExpiresAt
				coll.ConvictionSustained = true
				if err := k.Collection.Set(ctx, collID, coll); err != nil {
					sdk.UnwrapSDKContext(ctx).Logger().Error("pruneExpiredCollections: Collection.Set failed", "err", err)
				}

				// Emit appropriate event
				eventType := "collection_renewed"
				if !wasSustained {
					eventType = "collection_conviction_sustained"
				}
				sdkCtx.EventManager().EmitEvent(sdk.NewEvent(eventType,
					sdk.NewAttribute("id", strconv.FormatUint(collID, 10)),
					sdk.NewAttribute("conviction_score", conviction.String()),
					sdk.NewAttribute("new_expires_at", strconv.FormatInt(newExpiresAt, 10)),
				))

				pruned++
				continue
			}
			// Below threshold — clear conviction_sustained flag before deletion
			if coll.ConvictionSustained {
				coll.ConvictionSustained = false
				if err := k.Collection.Set(ctx, collID, coll); err != nil {
					sdk.UnwrapSDKContext(ctx).Logger().Error("pruneExpiredCollections: Collection.Set failed", "err", err)
				}
			}
		}

		// Defer deletion if a hide appeal is in flight. The jury hasn't ruled
		// and we don't want to force a verdict by deletion: an upheld appeal
		// (sentinel wrong) means the endorser was vouching for legitimate
		// content, while a rejected appeal (sentinel right) earns the slash.
		// Leave the collection in CollectionsByExpiry; this loop will retry
		// next block. handleAppealedHideExpiry (§10.3a) is the backstop — if
		// the jury never rules, the appeal_deadline timeout restores status
		// to ACTIVE and a subsequent §10.1 pass deletes via the normal
		// unlock path. The CollectionsByExpiry entry is permitted to outlive
		// expires_at while the appeal sits; this is the only place in §10.1
		// where that happens, and the pruned-counter is intentionally not
		// incremented so the slot stays available for other work this block.
		if coll.Status == types.CollectionStatus_COLLECTION_STATUS_HIDDEN && k.hasInflightHideAppeal(ctx, collID) {
			sdkCtx.EventManager().EmitEvent(sdk.NewEvent("collection_expiry_deferred",
				sdk.NewAttribute("id", strconv.FormatUint(collID, 10)),
				sdk.NewAttribute("reason", "hide_appeal_in_flight"),
			))
			continue
		}

		// Remove initiative link on hard-delete (best effort)
		if coll.InitiativeId > 0 && k.late.repKeeper != nil {
			if linkErr := k.late.repKeeper.RemoveContentInitiativeLink(ctx, coll.InitiativeId, int32(reptypes.StakeTargetType_STAKE_TARGET_COLLECTION_CONTENT), collID); linkErr != nil {
				sdkCtx.Logger().Error("failed to remove initiative link on prune", "collection_id", collID, "error", linkErr)
			}
		}

		// Delete via deleteCollectionFull which handles deposit refunds, sponsorship cleanup,
		// endorsement cleanup, hide records, curation reviews, items, collaborators, flags, etc.
		if err := k.deleteCollectionAtomic(ctx, coll); err != nil {
			sdkCtx.Logger().Error("endblock: failed to delete expired collection",
				"collection_id", collID, "error", err)
			pruned++
			continue
		}

		// Emit collection_expired event (distinct from collection_deleted emitted by deleteCollectionFull)
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("collection_expired",
			sdk.NewAttribute("id", strconv.FormatUint(collID, 10)),
			sdk.NewAttribute("owner", coll.Owner),
			sdk.NewAttribute("item_count", strconv.FormatUint(coll.ItemCount, 10)),
			sdk.NewAttribute("deposit_refunded", coll.DepositAmount.String()),
			sdk.NewAttribute("item_deposit_refunded", coll.ItemDepositTotal.String()),
		))

		// Count each item + the collection itself toward the prune budget
		pruned += commontypes.Saturate[uint32](coll.ItemCount) + 1
	}

	return pruned, nil
}

// pruneExpiredSponsorshipRequests implements §10.1 continued: walk SponsorshipRequestsByExpiry
// for entries where expiresAt <= currentBlock, refund escrowed deposits.
func (k Keeper) pruneExpiredSponsorshipRequests(
	ctx context.Context,
	currentBlock int64,
	_ types.Params,
	pruned, cap uint32,
) (uint32, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	var entries []collections.Pair[int64, uint64]
	err := k.SponsorshipRequestsByExpiry.Walk(ctx, nil, func(key collections.Pair[int64, uint64]) (bool, error) {
		if key.K1() > currentBlock || pruned+commontypes.Saturate[uint32](len(entries)) >= cap {
			return true, nil
		}
		entries = append(entries, key)
		return false, nil
	})
	if err != nil {
		return pruned, err
	}

	for _, entry := range entries {
		if pruned >= cap {
			break
		}

		expiresAt := entry.K1()
		collID := entry.K2()

		req, err := k.SponsorshipRequest.Get(ctx, collID)
		if err != nil {
			// Request no longer exists (already cleaned up) — remove stale index entry
			if err := k.SponsorshipRequestsByExpiry.Remove(ctx, collections.Join(expiresAt, collID)); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("pruneExpiredSponsorshipRequests: SponsorshipRequestsByExpiry.Remove failed", "err", err)
			}
			pruned++
			continue
		}

		// Refund escrowed deposits to requester
		requesterAddr, err := k.addressCodec.StringToBytes(req.Requester)
		if err != nil {
			sdkCtx.Logger().Error("endblock: invalid requester address in sponsorship request",
				"collection_id", collID, "requester", req.Requester, "error", err)
			pruned++
			continue
		}

		refundAmt := req.CollectionDeposit.Add(req.ItemDepositTotal)
		if refundAmt.IsPositive() {
			if err := k.RefundSPARK(ctx, requesterAddr, refundAmt); err != nil {
				sdkCtx.Logger().Error("endblock: failed to refund sponsorship request deposits",
					"collection_id", collID, "error", err)
				pruned++
				continue
			}
		}

		// Delete request and index entry
		if err := k.SponsorshipRequest.Remove(ctx, collID); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("pruneExpiredSponsorshipRequests: SponsorshipRequest.Remove failed", "err", err)
		}
		if err := k.SponsorshipRequestsByExpiry.Remove(ctx, collections.Join(expiresAt, collID)); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("pruneExpiredSponsorshipRequests: SponsorshipRequestsByExpiry.Remove failed", "err", err)
		}

		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("sponsorship_request_expired",
			sdk.NewAttribute("collection_id", strconv.FormatUint(collID, 10)),
			sdk.NewAttribute("requester", req.Requester),
			sdk.NewAttribute("deposit_refunded", refundAmt.String()),
		))

		pruned++
	}

	return pruned, nil
}

// pruneExpiredHideRecords implements §10.3 + §10.3a in a single walk of HideRecordExpiry.
// For entries where deadline <= currentBlock:
//   - Unappealed + unresolved (§10.3): delete hidden content, release sentinel bond
//   - Appealed + unresolved (§10.3a): restore to ACTIVE, refund 50% appeal fee, burn rest
func (k Keeper) pruneExpiredHideRecords(
	ctx context.Context,
	currentBlock int64,
	params types.Params,
	pruned, cap uint32,
) (uint32, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	var entries []collections.Pair[int64, uint64]
	err := k.HideRecordExpiry.Walk(ctx, nil, func(key collections.Pair[int64, uint64]) (bool, error) {
		if key.K1() > currentBlock || pruned+commontypes.Saturate[uint32](len(entries)) >= cap {
			return true, nil
		}
		entries = append(entries, key)
		return false, nil
	})
	if err != nil {
		return pruned, err
	}

	for _, entry := range entries {
		if pruned >= cap {
			break
		}

		deadline := entry.K1()
		hrID := entry.K2()

		hr, err := k.HideRecord.Get(ctx, hrID)
		if err != nil {
			// Stale index entry — remove
			if err := k.HideRecordExpiry.Remove(ctx, collections.Join(deadline, hrID)); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("pruneExpiredHideRecords: HideRecordExpiry.Remove failed", "err", err)
			}
			pruned++
			continue
		}

		// Skip already resolved records — except self-corrected ones, whose
		// expiry entry was deliberately retained by MsgUnhideContent so the
		// sentinel's committed bond stays reserved until the original
		// appeal_deadline (anti hide/unhide cycling). Release it now.
		if hr.Resolved {
			if hr.SelfCorrected {
				if k.late.repKeeper != nil && hr.CommittedAmount.IsPositive() {
					if err := k.late.repKeeper.ReleaseBond(ctx, reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, hr.Sentinel, hr.CommittedAmount); err != nil {
						sdk.UnwrapSDKContext(ctx).Logger().Error("pruneExpiredHideRecords: repKeeper.ReleaseBond failed", "err", err)
					}
				}
				if err := k.HideRecordExpiry.Remove(ctx, collections.Join(deadline, hrID)); err != nil {
					sdk.UnwrapSDKContext(ctx).Logger().Error("pruneExpiredHideRecords: HideRecordExpiry.Remove failed", "err", err)
				}
				pruned++

				released := hr.CommittedAmount
				// Zero the amount so genesis rebuild and deletion cleanup
				// know the retained bond is no longer held.
				hr.CommittedAmount = math.ZeroInt()
				if err := k.HideRecord.Set(ctx, hr.Id, hr); err != nil {
					sdk.UnwrapSDKContext(ctx).Logger().Error("pruneExpiredHideRecords: HideRecord.Set failed", "err", err)
				}

				sdkCtx.EventManager().EmitEvent(sdk.NewEvent("self_corrected_hide_bond_released",
					sdk.NewAttribute("hide_record_id", strconv.FormatUint(hr.Id, 10)),
					sdk.NewAttribute("sentinel", hr.Sentinel),
					sdk.NewAttribute("released", released.String()),
				))
				continue
			}
			// A settled unappealed hide whose target delete failed in an
			// earlier block re-indexed itself here (COL-201): retry.
			pruned = k.retryUnappealedHideDelete(ctx, sdkCtx, hr, deadline, hrID, params, pruned)
			continue
		}

		if hr.Appealed {
			// Appealed hides are owned by x/rep (deadline, verdict, timeout;
			// see appeal_target.go). MsgAppealHide removes the expiry entry,
			// so this is only a stale index entry.
			if err := k.HideRecordExpiry.Remove(ctx, collections.Join(deadline, hrID)); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("pruneExpiredHideRecords: HideRecordExpiry.Remove failed", "err", err)
			}
			pruned++
			continue
		}
		// §10.3: Unappealed hide — delete content, release bond
		pruned = k.handleUnappealedHideExpiry(ctx, sdkCtx, hr, deadline, hrID, params, pruned)
	}

	return pruned, nil
}

// handleUnappealedHideExpiry processes a single unappealed, unresolved hide record
// that has expired. Deletes the hidden content and releases sentinel bond.
func (k Keeper) handleUnappealedHideExpiry(
	ctx context.Context,
	sdkCtx sdk.Context,
	hr types.HideRecord,
	deadline int64,
	hrID uint64,
	params types.Params,
	pruned uint32,
) uint32 {
	// Release sentinel's reserved bond (no penalty — content was not appealed)
	// and persist Resolved BEFORE deleting the target. The delete walks every
	// hide record on the target (closeHideRecordOnTargetDelete), this one
	// included, and releases any record it finds unresolved; settling first
	// keeps this the only release. A second release would saturate at zero
	// in rep and silently free another action's reservation (COL-101).
	if k.late.repKeeper != nil && hr.CommittedAmount.IsPositive() {
		if err := k.late.repKeeper.ReleaseBond(ctx, reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, hr.Sentinel, hr.CommittedAmount); err != nil {
			sdk.UnwrapSDKContext(sdkCtx).Logger().Error("handleUnappealedHideExpiry: repKeeper.ReleaseBond failed", "err", err)
		}
	}
	hr.Resolved = true
	if err := k.HideRecord.Set(ctx, hr.Id, hr); err != nil {
		sdk.UnwrapSDKContext(sdkCtx).Logger().Error("handleUnappealedHideExpiry: HideRecord.Set failed", "err", err)
	}
	if err := k.HideRecordExpiry.Remove(ctx, collections.Join(deadline, hrID)); err != nil {
		sdk.UnwrapSDKContext(sdkCtx).Logger().Error("handleUnappealedHideExpiry: HideRecordExpiry.Remove failed", "err", err)
	}

	targetDeleted := k.deleteHiddenTarget(ctx, sdkCtx, hr, params)
	if !targetDeleted {
		// The record is settled but the target is still HIDDEN with every
		// exit closed (MsgUnhideContent refuses a resolved record,
		// MsgDeleteCollection refuses HIDDEN). Re-index at the next block so
		// the delete is retried, as the TTL pruner retries (COL-201).
		k.deferHiddenTargetDelete(ctx, sdkCtx, hr)
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("unappealed_hide_expired",
		sdk.NewAttribute("hide_record_id", strconv.FormatUint(hr.Id, 10)),
		sdk.NewAttribute("target_id", strconv.FormatUint(hr.TargetId, 10)),
		sdk.NewAttribute("target_type", fmt.Sprintf("%d", int32(hr.TargetType))),
		sdk.NewAttribute("target_deleted", strconv.FormatBool(targetDeleted)),
	))

	return pruned + 1
}

// deferHiddenTargetDelete re-indexes a settled hide record at the next block
// so pruneExpiredHideRecords retries the target delete (COL-201).
func (k Keeper) deferHiddenTargetDelete(ctx context.Context, sdkCtx sdk.Context, hr types.HideRecord) {
	retryAt := sdkCtx.BlockHeight() + 1
	if err := k.HideRecordExpiry.Set(ctx, collections.Join(retryAt, hr.Id)); err != nil {
		sdkCtx.Logger().Error("deferHiddenTargetDelete: HideRecordExpiry.Set failed", "hide_record_id", hr.Id, "err", err)
		return
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("hidden_target_delete_deferred",
		sdk.NewAttribute("hide_record_id", strconv.FormatUint(hr.Id, 10)),
		sdk.NewAttribute("target_id", strconv.FormatUint(hr.TargetId, 10)),
		sdk.NewAttribute("target_type", fmt.Sprintf("%d", int32(hr.TargetType))),
		sdk.NewAttribute("retry_at", strconv.FormatInt(retryAt, 10)),
	))
}

// retryUnappealedHideDelete finishes an unappealed hide whose record is
// already settled (bond released, resolved) but whose target delete failed in
// an earlier block and was re-indexed by deferHiddenTargetDelete (COL-201).
// The bond is never touched again. Only a collection delete can fail
// (deleteCollectionAtomic); the item path is best-effort and always
// completes. The target is deleted only while it is still HIDDEN: a later
// hide on the same target that was overturned has already restored it, and
// a hide appeal in flight on it belongs to x/rep, so both defer or drop the
// retry rather than delete.
func (k Keeper) retryUnappealedHideDelete(
	ctx context.Context,
	sdkCtx sdk.Context,
	hr types.HideRecord,
	deadline int64,
	hrID uint64,
	params types.Params,
	pruned uint32,
) uint32 {
	if err := k.HideRecordExpiry.Remove(ctx, collections.Join(deadline, hrID)); err != nil {
		sdkCtx.Logger().Error("retryUnappealedHideDelete: HideRecordExpiry.Remove failed", "err", err)
	}
	if hr.TargetType != types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION {
		return pruned + 1
	}
	coll, err := k.Collection.Get(ctx, hr.TargetId)
	if err != nil || coll.Status != types.CollectionStatus_COLLECTION_STATUS_HIDDEN {
		return pruned + 1 // gone, or brought back by a later verdict
	}
	if k.hasInflightHideAppeal(ctx, hr.TargetId) || !k.deleteHiddenTarget(ctx, sdkCtx, hr, params) {
		k.deferHiddenTargetDelete(ctx, sdkCtx, hr)
		return pruned + 1
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("unappealed_hide_expired",
		sdk.NewAttribute("hide_record_id", strconv.FormatUint(hr.Id, 10)),
		sdk.NewAttribute("target_id", strconv.FormatUint(hr.TargetId, 10)),
		sdk.NewAttribute("target_type", fmt.Sprintf("%d", int32(hr.TargetType))),
		sdk.NewAttribute("target_deleted", "true"),
	))
	return pruned + 1
}

// pruneExpiredFlags implements §10.4: walk FlagExpiry for entries where
// expiry <= currentBlock. Deletes CollectionFlag record and all index entries.
func (k Keeper) pruneExpiredFlags(
	ctx context.Context,
	currentBlock int64,
	_ types.Params,
	pruned, cap uint32,
) (uint32, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	var entries []collections.Pair[int64, string]
	err := k.FlagExpiry.Walk(ctx, nil, func(key collections.Pair[int64, string]) (bool, error) {
		if key.K1() > currentBlock || pruned+commontypes.Saturate[uint32](len(entries)) >= cap {
			return true, nil
		}
		entries = append(entries, key)
		return false, nil
	})
	if err != nil {
		return pruned, err
	}

	for _, entry := range entries {
		if pruned >= cap {
			break
		}

		expiryBlock := entry.K1()
		flagKey := entry.K2()

		// Get the flag record for event emission and cleanup
		flag, err := k.Flag.Get(ctx, flagKey)
		if err != nil {
			// Flag no longer exists — remove stale expiry index entry
			if err := k.FlagExpiry.Remove(ctx, collections.Join(expiryBlock, flagKey)); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("pruneExpiredFlags: FlagExpiry.Remove failed", "err", err)
			}
			pruned++
			continue
		}

		// A stale entry (left by an older key derivation) must never delete
		// a flag whose stored expiry is later: drop the entry only.
		if flag.ExpiresAt > 0 && flag.ExpiresAt != expiryBlock {
			if err := k.FlagExpiry.Remove(ctx, collections.Join(expiryBlock, flagKey)); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("pruneExpiredFlags: FlagExpiry.Remove failed", "err", err)
			}
			pruned++
			continue
		}

		// Remove from review queue if present
		if flag.InReviewQueue {
			if err := k.FlagReviewQueue.Remove(ctx, collections.Join(int32(flag.TargetType), flag.TargetId)); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("pruneExpiredFlags: FlagReviewQueue.Remove failed", "err", err)
			}
		}

		// Delete flag record and expiry index
		if err := k.Flag.Remove(ctx, flagKey); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("pruneExpiredFlags: Flag.Remove failed", "err", err)
		}
		if err := k.FlagExpiry.Remove(ctx, collections.Join(expiryBlock, flagKey)); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("pruneExpiredFlags: FlagExpiry.Remove failed", "err", err)
		}

		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("flags_expired",
			sdk.NewAttribute("target_id", strconv.FormatUint(flag.TargetId, 10)),
			sdk.NewAttribute("target_type", fmt.Sprintf("%d", int32(flag.TargetType))),
		))

		pruned++
	}

	return pruned, nil
}

// pruneUnendorsedCollections implements §10.5: walk EndorsementPending for entries
// where expiry <= currentBlock. Refunds endorsement_creation_fee (minus burn fraction)
// and deletes the collection via deleteCollectionFull.
func (k Keeper) pruneUnendorsedCollections(
	ctx context.Context,
	currentBlock int64,
	params types.Params,
	pruned, cap uint32,
) (uint32, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	var entries []collections.Pair[int64, uint64]
	err := k.EndorsementPending.Walk(ctx, nil, func(key collections.Pair[int64, uint64]) (bool, error) {
		if key.K1() > currentBlock || pruned+commontypes.Saturate[uint32](len(entries)) >= cap {
			return true, nil
		}
		entries = append(entries, key)
		return false, nil
	})
	if err != nil {
		return pruned, err
	}

	for _, entry := range entries {
		if pruned >= cap {
			break
		}

		expiryBlock := entry.K1()
		collID := entry.K2()

		// Remove the pending index entry regardless of outcome
		if err := k.EndorsementPending.Remove(ctx, collections.Join(expiryBlock, collID)); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("pruneUnendorsedCollections: EndorsementPending.Remove failed", "err", err)
		}

		coll, err := k.Collection.Get(ctx, collID)
		if err != nil {
			// Collection no longer exists (already deleted by TTL or owner) — stale entry removed above
			pruned++
			continue
		}

		// Only an unendorsed collection still in the endorsement stage, whose
		// stored deadline is this entry, is pruned. Anything else (endorsed,
		// promoted to ACTIVE or permanent, hidden, or a stale entry whose
		// deadline no longer matches) only loses the stale index entry.
		if coll.Status != types.CollectionStatus_COLLECTION_STATUS_PENDING || coll.EndorsedBy != "" ||
			(coll.EndorsementPendingUntil != 0 && coll.EndorsementPendingUntil != expiryBlock) {
			if coll.EndorsementPendingUntil == expiryBlock {
				coll.EndorsementPendingUntil = 0
				if err := k.Collection.Set(ctx, collID, coll); err != nil {
					sdk.UnwrapSDKContext(ctx).Logger().Error("pruneUnendorsedCollections: Collection.Set failed", "err", err)
				}
			}
			pruned++
			continue
		}
		// The endorsement creation fee refund (minus burn fraction) is handled
		// by deleteCollectionFull from the fee escrowed at creation.

		depositRefunded := coll.DepositAmount.Add(coll.ItemDepositTotal)

		if err := k.deleteCollectionAtomic(ctx, coll); err != nil {
			sdkCtx.Logger().Error("endblock: failed to delete unendorsed collection",
				"collection_id", collID, "error", err)
			pruned++
			continue
		}

		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("unendorsed_collection_pruned",
			sdk.NewAttribute("collection_id", strconv.FormatUint(collID, 10)),
			sdk.NewAttribute("owner", coll.Owner),
			sdk.NewAttribute("deposit_refunded", depositRefunded.String()),
		))

		pruned += commontypes.Saturate[uint32](coll.ItemCount) + 1
	}

	return pruned, nil
}

// releaseExpiredEndorsementStakes implements §10.6: walk EndorsementStakeExpiry for
// entries where releaseAt <= currentBlock. Releases DREAM stake to endorser.
func (k Keeper) releaseExpiredEndorsementStakes(
	ctx context.Context,
	currentBlock int64,
	_ types.Params,
	pruned, cap uint32,
) (uint32, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	var entries []collections.Pair[int64, uint64]
	err := k.EndorsementStakeExpiry.Walk(ctx, nil, func(key collections.Pair[int64, uint64]) (bool, error) {
		if key.K1() > currentBlock || pruned+commontypes.Saturate[uint32](len(entries)) >= cap {
			return true, nil
		}
		entries = append(entries, key)
		return false, nil
	})
	if err != nil {
		return pruned, err
	}

	for _, entry := range entries {
		if pruned >= cap {
			break
		}

		releaseAt := entry.K1()
		collID := entry.K2()

		// Remove index entry regardless
		if err := k.EndorsementStakeExpiry.Remove(ctx, collections.Join(releaseAt, collID)); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("releaseExpiredEndorsementStakes: EndorsementStakeExpiry.Remove failed", "err", err)
		}

		endorsement, err := k.Endorsement.Get(ctx, collID)
		if err != nil {
			// Endorsement no longer exists — stale entry removed above
			pruned++
			continue
		}

		if endorsement.StakeReleased {
			// Already released (e.g., by deleteCollectionFull) — skip
			pruned++
			continue
		}

		// Release DREAM stake to endorser
		endorserAddr, addrErr := k.addressCodec.StringToBytes(endorsement.Endorser)
		if addrErr != nil {
			sdkCtx.Logger().Error("endblock: invalid endorser address",
				"collection_id", collID, "endorser", endorsement.Endorser, "error", addrErr)
			pruned++
			continue
		}

		if err := k.late.repKeeper.UnlockDREAM(ctx, endorserAddr, endorsement.DreamStake); err != nil {
			sdkCtx.Logger().Error("endblock: failed to unlock endorser DREAM stake",
				"collection_id", collID, "error", err)
			pruned++
			continue
		}

		// Mark stake as released
		endorsement.StakeReleased = true
		if err := k.Endorsement.Set(ctx, collID, endorsement); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("releaseExpiredEndorsementStakes: Endorsement.Set failed", "err", err)
		}

		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("endorsement_stake_released",
			sdk.NewAttribute("collection_id", strconv.FormatUint(collID, 10)),
			sdk.NewAttribute("endorser", endorsement.Endorser),
			sdk.NewAttribute("amount", endorsement.DreamStake.String()),
		))

		pruned++
	}

	return pruned, nil
}

// deleteHiddenTarget deletes the content behind a hide that stands — an
// unappealed hide that expired (§10.3) or one UPHELD on appeal. Returns true
// once the target is gone (including when it already was).
func (k Keeper) deleteHiddenTarget(ctx context.Context, sdkCtx sdk.Context, hr types.HideRecord, params types.Params) bool {
	targetDeleted := false

	// Delete the hidden content
	switch hr.TargetType {
	case types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION:
		coll, err := k.Collection.Get(ctx, hr.TargetId)
		if err == nil {
			// coll.Status is HIDDEN here (set by HideContent, not flipped back
			// because no appeal was filed) — deleteCollectionFull's endorser
			// cleanup branches on that to BURN the endorser's stake instead of
			// unlocking it. Also the UPHELD-appeal path (appeal_target.go).
			if err := k.deleteCollectionAtomic(ctx, coll); err != nil {
				sdkCtx.Logger().Error("endblock: failed to delete hidden collection",
					"collection_id", hr.TargetId, "error", err)
			} else {
				targetDeleted = true
			}
		} else {
			targetDeleted = true // already gone
		}

	case types.FlagTargetType_FLAG_TARGET_TYPE_ITEM:
		item, err := k.Item.Get(ctx, hr.TargetId)
		if err == nil {
			// Refund per_item_deposit to collection owner if TTL collection
			coll, collErr := k.Collection.Get(ctx, item.CollectionId)
			if collErr == nil && !coll.DepositBurned {
				// Refund capped at the deposit actually held; anonymous
				// collections hold none, so the shield account is never
				// paid out of other owners' escrow.
				refund := math.ZeroInt()
				if k.chargesDeposits(coll) {
					refund = heldItemDeposit(coll, params.PerItemDeposit, 1)
				}
				ownerAddr, addrErr := k.addressCodec.StringToBytes(coll.Owner)
				if addrErr == nil && refund.IsPositive() {
					if err := k.RefundSPARK(ctx, ownerAddr, refund); err != nil {
						sdk.UnwrapSDKContext(sdkCtx).Logger().Error("deleteHiddenTarget: k.RefundSPARK failed", "err", err)
					}
				}
				// Decrement item_count and item_deposit_total
				k.decrementItemCount(&coll, 1)
				if refund.IsPositive() {
					coll.ItemDepositTotal = coll.ItemDepositTotal.Sub(refund)
					if coll.ItemDepositTotal.IsNegative() {
						coll.ItemDepositTotal = math.ZeroInt() // safety clamp
					}
				}
				if err := k.Collection.Set(ctx, coll.Id, coll); err != nil {
					sdk.UnwrapSDKContext(sdkCtx).Logger().Error("deleteHiddenTarget: Collection.Set failed", "err", err)
				}
			}

			// Remove item from indexes
			if err := k.ItemsByCollection.Remove(ctx, collections.Join(item.CollectionId, item.Id)); err != nil {
				sdk.UnwrapSDKContext(sdkCtx).Logger().Error("deleteHiddenTarget: ItemsByCollection.Remove failed", "err", err)
			}
			if collErr == nil {
				if err := k.ItemsByOwner.Remove(ctx, collections.Join(coll.Owner, item.Id)); err != nil {
					sdk.UnwrapSDKContext(sdkCtx).Logger().Error("deleteHiddenTarget: ItemsByOwner.Remove failed", "err", err)
				}
			}
			// Clean up item flags
			flagKey := FlagCompositeKey(types.FlagTargetType_FLAG_TARGET_TYPE_ITEM, item.Id)
			flag, flagErr := k.Flag.Get(ctx, flagKey)
			if flagErr == nil {
				if flag.InReviewQueue {
					if err := k.FlagReviewQueue.Remove(ctx, collections.Join(int32(types.FlagTargetType_FLAG_TARGET_TYPE_ITEM), item.Id)); err != nil {
						sdk.UnwrapSDKContext(sdkCtx).Logger().Error("deleteHiddenTarget: FlagReviewQueue.Remove failed", "err", err)
					}
				}
				if err := k.FlagExpiry.Remove(ctx, collections.Join(flagExpiryBlock(flag, params), flagKey)); err != nil {
					sdk.UnwrapSDKContext(sdkCtx).Logger().Error("deleteHiddenTarget: FlagExpiry.Remove failed", "err", err)
				}
				if err := k.Flag.Remove(ctx, flagKey); err != nil {
					sdk.UnwrapSDKContext(sdkCtx).Logger().Error("deleteHiddenTarget: Flag.Remove failed", "err", err)
				}
			}
			// Clean up item hide records (other hide records for this same item)
			k.cleanupItemHideRecords(ctx, item, params)
			// Drop reaction dedup markers and the on-chain ref index entry.
			k.deleteTargetReactions(ctx, types.FlagTargetType_FLAG_TARGET_TYPE_ITEM, item.Id)
			k.removeItemOnChainRef(ctx, item)
			// Delete item
			if err := k.Item.Remove(ctx, item.Id); err != nil {
				sdk.UnwrapSDKContext(sdkCtx).Logger().Error("deleteHiddenTarget: Item.Remove failed", "err", err)
			}

			// Positions are allowed to be sparse (no auto-compaction on removal).
			targetDeleted = true
		} else {
			targetDeleted = true // already gone
		}
	}

	return targetDeleted
}

// restoreHiddenTarget sets a hidden collection or item back to ACTIVE (appeal
// OVERTURNED or timed out).
func (k Keeper) restoreHiddenTarget(ctx context.Context, hr types.HideRecord) {
	switch hr.TargetType {
	case types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION:
		coll, collErr := k.Collection.Get(ctx, hr.TargetId)
		if collErr == nil && coll.Status == types.CollectionStatus_COLLECTION_STATUS_HIDDEN {
			// Update status index: HIDDEN → ACTIVE (pinned unchanged).
			oldStatus := coll.Status
			coll.Status = types.CollectionStatus_COLLECTION_STATUS_ACTIVE
			if err := k.MoveCollectionStatusIndex(ctx, oldStatus, coll.Pinned, coll.Status, coll.Pinned, coll.Id); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("restoreHiddenTarget: k.MoveCollectionStatusIndex failed", "err", err)
			}
			if err := k.Collection.Set(ctx, coll.Id, coll); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("restoreHiddenTarget: Collection.Set failed", "err", err)
			}
		}
	case types.FlagTargetType_FLAG_TARGET_TYPE_ITEM:
		item, itemErr := k.Item.Get(ctx, hr.TargetId)
		if itemErr == nil && item.Status == types.ItemStatus_ITEM_STATUS_HIDDEN {
			item.Status = types.ItemStatus_ITEM_STATUS_ACTIVE
			if err := k.Item.Set(ctx, item.Id, item); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("restoreHiddenTarget: Item.Set failed", "err", err)
			}
		}
	}
}
