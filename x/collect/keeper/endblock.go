package keeper

import (
	"context"
	"fmt"
	"strconv"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/collect/types"
)

// PruneExpired prunes expired collections, sponsorship requests, unappealed hides,
// timed-out appeals, expired flags, unendorsed collections, and releases endorsement stakes.
// Called by the EndBlocker each block. All 7 tasks share a single pruned counter
// capped at params.MaxPrunePerBlock.
func (k Keeper) PruneExpired(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentBlock := sdkCtx.BlockHeight()

	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
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

	// §10.3 — Unappealed hide expiry
	pruned, err = k.pruneUnappealedHides(ctx, currentBlock, params, pruned, cap)
	if err != nil {
		return err
	}

	// §10.3a — Appeal timeout
	pruned, err = k.pruneAppealTimeouts(ctx, currentBlock, params, pruned, cap)
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
	_, err = k.releaseExpiredEndorsementStakes(ctx, currentBlock, params, pruned, cap)
	if err != nil {
		return err
	}

	return nil
}

// pruneExpiredCollections implements §10.1: walk CollectionsByExpiry for entries
// where expiresAt <= currentBlock, then delete via deleteCollectionFull.
func (k Keeper) pruneExpiredCollections(
	ctx context.Context,
	currentBlock int64,
	params types.Params,
	pruned, cap uint32,
) (uint32, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Collect entries to process (cannot modify store during Walk)
	var entries []collections.Pair[int64, uint64]
	err := k.CollectionsByExpiry.Walk(ctx, nil, func(key collections.Pair[int64, uint64]) (bool, error) {
		if key.K1() > currentBlock || pruned+uint32(len(entries)) >= cap {
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
			k.CollectionsByExpiry.Remove(ctx, collections.Join(expiresAt, collID)) //nolint:errcheck
			pruned++
			continue
		}

		// Delete via deleteCollectionFull which handles deposit refunds, sponsorship cleanup,
		// endorsement cleanup, hide records, curation reviews, items, collaborators, flags, etc.
		if err := k.deleteCollectionFull(ctx, coll); err != nil {
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
		pruned += uint32(coll.ItemCount) + 1
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
		if key.K1() > currentBlock || pruned+uint32(len(entries)) >= cap {
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
			k.SponsorshipRequestsByExpiry.Remove(ctx, collections.Join(expiresAt, collID)) //nolint:errcheck
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
		k.SponsorshipRequest.Remove(ctx, collID)                                       //nolint:errcheck
		k.SponsorshipRequestsByExpiry.Remove(ctx, collections.Join(expiresAt, collID)) //nolint:errcheck

		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("sponsorship_request_expired",
			sdk.NewAttribute("collection_id", strconv.FormatUint(collID, 10)),
			sdk.NewAttribute("requester", req.Requester),
			sdk.NewAttribute("deposit_refunded", refundAmt.String()),
		))

		pruned++
	}

	return pruned, nil
}

// pruneUnappealedHides implements §10.3: walk HideRecordExpiry for entries where
// deadline <= currentBlock, filter for appealed=false + resolved=false.
// Deletes the hidden content and releases sentinel bond.
func (k Keeper) pruneUnappealedHides(
	ctx context.Context,
	currentBlock int64,
	params types.Params,
	pruned, cap uint32,
) (uint32, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	var entries []collections.Pair[int64, uint64]
	err := k.HideRecordExpiry.Walk(ctx, nil, func(key collections.Pair[int64, uint64]) (bool, error) {
		if key.K1() > currentBlock || pruned+uint32(len(entries)) >= cap {
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
			k.HideRecordExpiry.Remove(ctx, collections.Join(deadline, hrID)) //nolint:errcheck
			pruned++
			continue
		}

		// §10.3: only process unappealed, unresolved hides
		if hr.Appealed || hr.Resolved {
			continue
		}

		targetDeleted := false

		// Delete the hidden content
		switch hr.TargetType {
		case types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION:
			coll, err := k.Collection.Get(ctx, hr.TargetId)
			if err == nil {
				if err := k.deleteCollectionFull(ctx, coll); err != nil {
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
					ownerAddr, addrErr := k.addressCodec.StringToBytes(coll.Owner)
					if addrErr == nil && params.PerItemDeposit.IsPositive() {
						k.RefundSPARK(ctx, ownerAddr, params.PerItemDeposit) //nolint:errcheck
					}
					// Decrement item_count and item_deposit_total
					if coll.ItemCount > 0 {
						coll.ItemCount--
					}
					coll.ItemDepositTotal = coll.ItemDepositTotal.Sub(params.PerItemDeposit)
					if coll.ItemDepositTotal.IsNegative() {
						coll.ItemDepositTotal = math.ZeroInt() // safety clamp
					}
					k.Collection.Set(ctx, coll.Id, coll) //nolint:errcheck
				}

				// Remove item from indexes
				k.ItemsByCollection.Remove(ctx, collections.Join(item.CollectionId, item.Id)) //nolint:errcheck
				k.ItemsByOwner.Remove(ctx, collections.Join(coll.Owner, item.Id))             //nolint:errcheck
				// Clean up item flags
				flagKey := FlagCompositeKey(types.FlagTargetType_FLAG_TARGET_TYPE_ITEM, item.Id)
				flag, flagErr := k.Flag.Get(ctx, flagKey)
				if flagErr == nil {
					if flag.InReviewQueue {
						k.FlagReviewQueue.Remove(ctx, collections.Join(int32(types.FlagTargetType_FLAG_TARGET_TYPE_ITEM), item.Id)) //nolint:errcheck
					}
					k.FlagExpiry.Remove(ctx, collections.Join(flag.LastFlagAt+params.FlagExpirationBlocks, flagKey)) //nolint:errcheck
					k.Flag.Remove(ctx, flagKey)                                                                      //nolint:errcheck
				}
				// Clean up item hide records (other hide records for this same item)
				k.cleanupItemHideRecords(ctx, item, params)
				// Delete item
				k.Item.Remove(ctx, item.Id) //nolint:errcheck

				// Compact positions for the collection
				if collErr == nil {
					k.CompactPositions(ctx, item.CollectionId) //nolint:errcheck
				}
				targetDeleted = true
			} else {
				targetDeleted = true // already gone
			}
		}

		// Release sentinel's committed bond (no penalty — content was not appealed)
		if k.forumKeeper != nil {
			k.forumKeeper.ReleaseBondCommitment(ctx, hr.Sentinel, hr.CommittedAmount, types.ModuleName, hr.Id) //nolint:errcheck
		}

		// Mark HideRecord resolved
		hr.Resolved = true
		k.HideRecord.Set(ctx, hr.Id, hr)                                 //nolint:errcheck
		k.HideRecordExpiry.Remove(ctx, collections.Join(deadline, hrID)) //nolint:errcheck

		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("unappealed_hide_expired",
			sdk.NewAttribute("hide_record_id", strconv.FormatUint(hr.Id, 10)),
			sdk.NewAttribute("target_id", strconv.FormatUint(hr.TargetId, 10)),
			sdk.NewAttribute("target_type", fmt.Sprintf("%d", int32(hr.TargetType))),
			sdk.NewAttribute("target_deleted", strconv.FormatBool(targetDeleted)),
		))

		pruned++
	}

	return pruned, nil
}

// pruneAppealTimeouts implements §10.3a: walk HideRecordExpiry for entries where
// deadline <= currentBlock, filter for appealed=true + resolved=false.
// Restores hidden content to ACTIVE, refunds 50% appeal fee, burns remaining 50%.
func (k Keeper) pruneAppealTimeouts(
	ctx context.Context,
	currentBlock int64,
	params types.Params,
	pruned, cap uint32,
) (uint32, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// We need to re-walk since pruneUnappealedHides may have consumed some entries
	// but skipped the appealed ones
	var entries []collections.Pair[int64, uint64]
	err := k.HideRecordExpiry.Walk(ctx, nil, func(key collections.Pair[int64, uint64]) (bool, error) {
		if key.K1() > currentBlock || pruned+uint32(len(entries)) >= cap {
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
			k.HideRecordExpiry.Remove(ctx, collections.Join(deadline, hrID)) //nolint:errcheck
			pruned++
			continue
		}

		// §10.3a: only process appealed, unresolved hides
		if !hr.Appealed || hr.Resolved {
			continue
		}

		// Restore hidden content to ACTIVE (favor appellant)
		switch hr.TargetType {
		case types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION:
			coll, collErr := k.Collection.Get(ctx, hr.TargetId)
			if collErr == nil && coll.Status == types.CollectionStatus_COLLECTION_STATUS_HIDDEN {
				// Update status indexes
				k.CollectionsByStatus.Remove(ctx, collections.Join(int32(coll.Status), coll.Id)) //nolint:errcheck
				coll.Status = types.CollectionStatus_COLLECTION_STATUS_ACTIVE
				k.CollectionsByStatus.Set(ctx, collections.Join(int32(coll.Status), coll.Id)) //nolint:errcheck
				k.Collection.Set(ctx, coll.Id, coll)                                          //nolint:errcheck
			}

		case types.FlagTargetType_FLAG_TARGET_TYPE_ITEM:
			item, itemErr := k.Item.Get(ctx, hr.TargetId)
			if itemErr == nil && item.Status == types.ItemStatus_ITEM_STATUS_HIDDEN {
				item.Status = types.ItemStatus_ITEM_STATUS_ACTIVE
				k.Item.Set(ctx, item.Id, item) //nolint:errcheck
			}
		}

		// Resolve the appeal owner (content owner is the appellant) for refund
		appellantRefund := params.AppealFee.Quo(math.NewInt(2)) // 50%

		// Find the content owner to refund the appeal fee to
		var appellantAddr sdk.AccAddress
		switch hr.TargetType {
		case types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION:
			coll, collErr := k.Collection.Get(ctx, hr.TargetId)
			if collErr == nil {
				addr, addrErr := k.addressCodec.StringToBytes(coll.Owner)
				if addrErr == nil {
					appellantAddr = addr
				}
			}
		case types.FlagTargetType_FLAG_TARGET_TYPE_ITEM:
			item, itemErr := k.Item.Get(ctx, hr.TargetId)
			if itemErr == nil {
				coll, collErr := k.Collection.Get(ctx, item.CollectionId)
				if collErr == nil {
					addr, addrErr := k.addressCodec.StringToBytes(coll.Owner)
					if addrErr == nil {
						appellantAddr = addr
					}
				}
			}
		}

		// Refund 50% of appeal_fee to appellant
		if appellantAddr != nil && appellantRefund.IsPositive() {
			k.RefundSPARK(ctx, appellantAddr, appellantRefund) //nolint:errcheck
		}

		// Burn remaining 50% (simplified: spec says 30% jury + 20% burned,
		// but jury pool is not implemented yet)
		burnAmt := params.AppealFee.Sub(appellantRefund)
		if burnAmt.IsPositive() {
			k.BurnSPARK(ctx, burnAmt) //nolint:errcheck
		}

		// Release sentinel's committed bond (no penalty — jury timed out)
		if k.forumKeeper != nil {
			k.forumKeeper.ReleaseBondCommitment(ctx, hr.Sentinel, hr.CommittedAmount, types.ModuleName, hr.Id) //nolint:errcheck
		}

		// Mark HideRecord resolved
		hr.Resolved = true
		k.HideRecord.Set(ctx, hr.Id, hr)                                 //nolint:errcheck
		k.HideRecordExpiry.Remove(ctx, collections.Join(deadline, hrID)) //nolint:errcheck

		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("hide_appeal_timeout",
			sdk.NewAttribute("hide_record_id", strconv.FormatUint(hr.Id, 10)),
			sdk.NewAttribute("target_id", strconv.FormatUint(hr.TargetId, 10)),
			sdk.NewAttribute("target_type", fmt.Sprintf("%d", int32(hr.TargetType))),
			sdk.NewAttribute("appellant_refund", appellantRefund.String()),
		))

		pruned++
	}

	return pruned, nil
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
		if key.K1() > currentBlock || pruned+uint32(len(entries)) >= cap {
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
			k.FlagExpiry.Remove(ctx, collections.Join(expiryBlock, flagKey)) //nolint:errcheck
			pruned++
			continue
		}

		// Remove from review queue if present
		if flag.InReviewQueue {
			k.FlagReviewQueue.Remove(ctx, collections.Join(int32(flag.TargetType), flag.TargetId)) //nolint:errcheck
		}

		// Delete flag record and expiry index
		k.Flag.Remove(ctx, flagKey)                                      //nolint:errcheck
		k.FlagExpiry.Remove(ctx, collections.Join(expiryBlock, flagKey)) //nolint:errcheck

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
		if key.K1() > currentBlock || pruned+uint32(len(entries)) >= cap {
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
		k.EndorsementPending.Remove(ctx, collections.Join(expiryBlock, collID)) //nolint:errcheck

		coll, err := k.Collection.Get(ctx, collID)
		if err != nil {
			// Collection no longer exists (already deleted by TTL or owner) — stale entry removed above
			pruned++
			continue
		}

		// The endorsement creation fee refund (minus burn fraction) is handled by
		// deleteCollectionFull when status == PENDING, so we just need to call it.
		// But deleteCollectionFull will also try to remove from EndorsementPending index
		// using the current block height (not the stored expiry). Since we already removed it
		// above, that's fine — the remove will be a no-op.

		depositRefunded := coll.DepositAmount.Add(coll.ItemDepositTotal)

		if err := k.deleteCollectionFull(ctx, coll); err != nil {
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

		pruned += uint32(coll.ItemCount) + 1
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
		if key.K1() > currentBlock || pruned+uint32(len(entries)) >= cap {
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
		k.EndorsementStakeExpiry.Remove(ctx, collections.Join(releaseAt, collID)) //nolint:errcheck

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

		if err := k.repKeeper.UnlockDREAM(ctx, endorserAddr, endorsement.DreamStake); err != nil {
			sdkCtx.Logger().Error("endblock: failed to unlock endorser DREAM stake",
				"collection_id", collID, "error", err)
			pruned++
			continue
		}

		// Mark stake as released
		endorsement.StakeReleased = true
		k.Endorsement.Set(ctx, collID, endorsement) //nolint:errcheck

		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("endorsement_stake_released",
			sdk.NewAttribute("collection_id", strconv.FormatUint(collID, 10)),
			sdk.NewAttribute("endorser", endorsement.Endorser),
			sdk.NewAttribute("amount", endorsement.DreamStake.String()),
		))

		pruned++
	}

	return pruned, nil
}
