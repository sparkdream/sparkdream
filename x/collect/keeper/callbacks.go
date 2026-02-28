package keeper

import (
	"context"
	"strconv"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/collect/types"
)

// OnMembershipGranted is called by x/rep when a non-member becomes a member.
// It transitions all PENDING collections to ACTIVE, lifts immutability, and clears seeking_endorsement.
// This is idempotent — calling it on an address with no PENDING collections is a no-op.
func (k Keeper) OnMembershipGranted(ctx context.Context, address string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Walk all collections owned by this address
	var collectionIDs []uint64
	err := k.CollectionsByOwner.Walk(ctx,
		collections.NewPrefixedPairRange[string, uint64](address),
		func(key collections.Pair[string, uint64]) (bool, error) {
			collectionIDs = append(collectionIDs, key.K2())
			return false, nil
		},
	)
	if err != nil {
		return errorsmod.Wrap(err, "failed to walk collections by owner")
	}

	for _, collID := range collectionIDs {
		coll, err := k.Collection.Get(ctx, collID)
		if err != nil {
			continue
		}

		changed := false

		// §14.20.1: PENDING → ACTIVE
		if coll.Status == types.CollectionStatus_COLLECTION_STATUS_PENDING {
			// Update status index
			k.CollectionsByStatus.Remove(ctx, collections.Join(int32(coll.Status), coll.Id)) //nolint:errcheck
			coll.Status = types.CollectionStatus_COLLECTION_STATUS_ACTIVE
			k.CollectionsByStatus.Set(ctx, collections.Join(int32(coll.Status), coll.Id)) //nolint:errcheck

			// Remove from EndorsementPending index
			k.EndorsementPending.Walk(ctx, nil, func(key collections.Pair[int64, uint64]) (bool, error) {
				if key.K2() == coll.Id {
					k.EndorsementPending.Remove(ctx, key) //nolint:errcheck
					return true, nil
				}
				return false, nil
			}) //nolint:errcheck

			changed = true
		}

		// §14.20.2: Lift immutability
		if coll.Immutable {
			coll.Immutable = false
			changed = true
		}

		// §14.20.3: Clear seeking_endorsement
		if coll.SeekingEndorsement {
			coll.SeekingEndorsement = false
			changed = true
		}

		if changed {
			if err := k.Collection.Set(ctx, coll.Id, coll); err != nil {
				return errorsmod.Wrapf(err, "failed to update collection %d", coll.Id)
			}
			sdkCtx.EventManager().EmitEvent(sdk.NewEvent("collection_membership_upgraded",
				sdk.NewAttribute("collection_id", strconv.FormatUint(coll.Id, 10)),
				sdk.NewAttribute("owner", address),
				sdk.NewAttribute("new_status", coll.Status.String()),
			))
		}
	}

	return nil
}

// ResolveChallengeResult is called by the x/rep jury to resolve a curation challenge.
// If upheld (challenger wins): review overturned, curator bond slashed, challenger rewarded.
// If rejected (curator wins): review stands, challenge deposit burned.
func (k Keeper) ResolveChallengeResult(ctx context.Context, reviewID uint64, upheld bool) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	review, err := k.CurationReview.Get(ctx, reviewID)
	if err != nil {
		// Review missing (collection deleted during deliberation) — no-op per spec §5.16
		return nil
	}

	if !review.Challenged {
		return errorsmod.Wrap(types.ErrReviewNotFound, "review is not challenged")
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return errorsmod.Wrap(err, "failed to get params")
	}

	curator, err := k.Curator.Get(ctx, review.Curator)
	if err != nil {
		return errorsmod.Wrap(err, "curator not found")
	}

	challengerAddr, err := k.addressCodec.StringToBytes(review.Challenger)
	if err != nil {
		return errorsmod.Wrap(err, "invalid challenger address")
	}

	curatorAddr, err := k.addressCodec.StringToBytes(review.Curator)
	if err != nil {
		return errorsmod.Wrap(err, "invalid curator address")
	}

	if upheld {
		// Challenger wins: review overturned
		review.Overturned = true

		// Slash curator bond
		slashAmount := params.CuratorSlashFraction.MulInt(curator.BondAmount).TruncateInt()
		rewardAmount := params.ChallengeRewardFraction.MulInt(slashAmount).TruncateInt()
		burnAmount := slashAmount.Sub(rewardAmount)

		// Slash the curator's bond via x/rep
		if slashAmount.IsPositive() {
			k.repKeeper.BurnDREAM(ctx, curatorAddr, slashAmount) //nolint:errcheck
		}

		// Reward challenger from slashed amount
		if rewardAmount.IsPositive() {
			// Mint/transfer reward to challenger (via rep keeper since this is DREAM)
			k.repKeeper.UnlockDREAM(ctx, challengerAddr, rewardAmount) //nolint:errcheck
		}

		// Update curator bond
		curator.BondAmount = curator.BondAmount.Sub(slashAmount)
		curator.ChallengedReviews++
		curator.PendingChallenges--

		// Auto-deactivate if bond drops below minimum
		curatorDeactivated := false
		if curator.BondAmount.LT(params.MinCuratorBond) {
			curator.Active = false
			curatorDeactivated = true
		}

		if err := k.Curator.Set(ctx, review.Curator, curator); err != nil {
			return errorsmod.Wrap(err, "failed to update curator")
		}

		// Refund challenge deposit to challenger
		k.repKeeper.UnlockDREAM(ctx, challengerAddr, params.ChallengeDeposit) //nolint:errcheck

		// Update curation summary (recalculate with overturned review excluded)
		k.recalculateSummary(ctx, review.CollectionId)

		// Store updated review
		if err := k.CurationReview.Set(ctx, reviewID, review); err != nil {
			return errorsmod.Wrap(err, "failed to update review")
		}

		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("challenge_resolved",
			sdk.NewAttribute("review_id", strconv.FormatUint(reviewID, 10)),
			sdk.NewAttribute("upheld", "true"),
			sdk.NewAttribute("curator", review.Curator),
			sdk.NewAttribute("slash_amount", slashAmount.String()),
			sdk.NewAttribute("reward_amount", rewardAmount.String()),
			sdk.NewAttribute("burn_amount", burnAmount.String()),
			sdk.NewAttribute("curator_deactivated", strconv.FormatBool(curatorDeactivated)),
			sdk.NewAttribute("challenger_refunded", params.ChallengeDeposit.String()),
		))
	} else {
		// Curator wins: review stands
		curator.PendingChallenges--
		if err := k.Curator.Set(ctx, review.Curator, curator); err != nil {
			return errorsmod.Wrap(err, "failed to update curator")
		}

		// Burn challenge deposit
		k.repKeeper.BurnDREAM(ctx, challengerAddr, params.ChallengeDeposit) //nolint:errcheck

		// Store updated review (challenger field remains for history)
		if err := k.CurationReview.Set(ctx, reviewID, review); err != nil {
			return errorsmod.Wrap(err, "failed to update review")
		}

		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("challenge_resolved",
			sdk.NewAttribute("review_id", strconv.FormatUint(reviewID, 10)),
			sdk.NewAttribute("upheld", "false"),
			sdk.NewAttribute("curator", review.Curator),
			sdk.NewAttribute("slash_amount", "0"),
			sdk.NewAttribute("reward_amount", "0"),
			sdk.NewAttribute("burn_amount", params.ChallengeDeposit.String()),
			sdk.NewAttribute("curator_deactivated", "false"),
			sdk.NewAttribute("challenger_refunded", "0"),
		))
	}

	return nil
}

// recalculateSummary recomputes the CurationSummary for a collection from scratch.
func (k Keeper) recalculateSummary(ctx context.Context, collectionID uint64) {
	var upCount, downCount uint32
	tagCounts := make(map[string]uint32)
	var lastReviewedAt int64

	k.CurationReviewsByCollection.Walk(ctx,
		collections.NewPrefixedPairRange[uint64, uint64](collectionID),
		func(key collections.Pair[uint64, uint64]) (bool, error) {
			review, err := k.CurationReview.Get(ctx, key.K2())
			if err != nil || review.Overturned {
				return false, nil
			}
			if review.Verdict == types.CurationVerdict_CURATION_VERDICT_UP {
				upCount++
			} else if review.Verdict == types.CurationVerdict_CURATION_VERDICT_DOWN {
				downCount++
			}
			for _, tag := range review.Tags {
				tagCounts[tag]++
			}
			if review.CreatedAt > lastReviewedAt {
				lastReviewedAt = review.CreatedAt
			}
			return false, nil
		},
	)

	topTags := make([]types.TagCount, 0, len(tagCounts))
	for tag, count := range tagCounts {
		topTags = append(topTags, types.TagCount{Tag: tag, Count: count})
	}

	summary := types.CurationSummary{
		CollectionId:   collectionID,
		UpCount:        upCount,
		DownCount:      downCount,
		TopTags:        topTags,
		LastReviewedAt: lastReviewedAt,
	}
	k.CurationSummary.Set(ctx, collectionID, summary) //nolint:errcheck
}

// ResolveHideAppeal is called by the x/rep jury to resolve a hide appeal.
// If upheld (appellant wins — sentinel was wrong): content restored, sentinel slashed.
// If rejected (sentinel wins): content deleted, sentinel vindicated.
func (k Keeper) ResolveHideAppeal(ctx context.Context, hideRecordID uint64, upheld bool) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	hr, err := k.HideRecord.Get(ctx, hideRecordID)
	if err != nil {
		return types.ErrHideRecordNotFound
	}

	if hr.Resolved {
		return types.ErrHideRecordResolved
	}
	if !hr.Appealed {
		return errorsmod.Wrap(types.ErrHideRecordNotFound, "hide record not appealed")
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return errorsmod.Wrap(err, "failed to get params")
	}

	if upheld {
		// Appellant wins — sentinel was wrong

		// Restore content to ACTIVE
		switch hr.TargetType {
		case types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION:
			coll, collErr := k.Collection.Get(ctx, hr.TargetId)
			if collErr == nil && coll.Status == types.CollectionStatus_COLLECTION_STATUS_HIDDEN {
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

		// Find content owner (appellant) for appeal fee refund
		var appellantAddr sdk.AccAddress
		appellantAddr = k.resolveContentOwnerAddr(ctx, hr.TargetType, hr.TargetId)

		// Refund 80% of appeal_fee to appellant
		appellantRefund := params.AppealFee.MulRaw(80).Quo(math.NewInt(100))
		if appellantAddr != nil && appellantRefund.IsPositive() {
			k.RefundSPARK(ctx, appellantAddr, appellantRefund) //nolint:errcheck
		}

		// Burn remaining 20% (jurors compensated via x/rep DREAM minting, no SPARK jury pool)
		burnAmount := params.AppealFee.Sub(appellantRefund)
		if burnAmount.IsPositive() {
			k.BurnSPARK(ctx, burnAmount) //nolint:errcheck
		}

		// Slash sentinel's committed bond
		if k.forumKeeper != nil {
			k.forumKeeper.SlashBondCommitment(ctx, hr.Sentinel, hr.CommittedAmount, types.ModuleName, hr.Id) //nolint:errcheck
		}

		hr.Resolved = true
		k.HideRecord.Set(ctx, hr.Id, hr)                                           //nolint:errcheck
		k.HideRecordExpiry.Remove(ctx, collections.Join(hr.AppealDeadline, hr.Id)) //nolint:errcheck

		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("hide_appeal_upheld",
			sdk.NewAttribute("hide_record_id", strconv.FormatUint(hr.Id, 10)),
			sdk.NewAttribute("target_id", strconv.FormatUint(hr.TargetId, 10)),
			sdk.NewAttribute("target_type", hr.TargetType.String()),
			sdk.NewAttribute("sentinel_slashed", hr.CommittedAmount.String()),
			sdk.NewAttribute("appellant_refund", appellantRefund.String()),
		))
	} else {
		// Sentinel wins — content should be deleted

		// Delete target
		switch hr.TargetType {
		case types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION:
			coll, collErr := k.Collection.Get(ctx, hr.TargetId)
			if collErr == nil {
				// Check if endorsed — slash endorser stake
				if coll.EndorsedBy != "" {
					endorsement, endErr := k.Endorsement.Get(ctx, coll.Id)
					if endErr == nil && !endorsement.StakeReleased {
						endorserAddr, addrErr := k.addressCodec.StringToBytes(endorsement.Endorser)
						if addrErr == nil {
							k.repKeeper.BurnDREAM(ctx, endorserAddr, endorsement.DreamStake) //nolint:errcheck
						}
						endorsement.StakeReleased = true
						k.Endorsement.Set(ctx, coll.Id, endorsement) //nolint:errcheck

						sdkCtx.EventManager().EmitEvent(sdk.NewEvent("endorsement_stake_slashed",
							sdk.NewAttribute("collection_id", strconv.FormatUint(coll.Id, 10)),
							sdk.NewAttribute("endorser", endorsement.Endorser),
							sdk.NewAttribute("amount", endorsement.DreamStake.String()),
						))
					}
				}
				k.deleteCollectionFull(ctx, coll) //nolint:errcheck
			}
		case types.FlagTargetType_FLAG_TARGET_TYPE_ITEM:
			item, itemErr := k.Item.Get(ctx, hr.TargetId)
			if itemErr == nil {
				coll, collErr := k.Collection.Get(ctx, item.CollectionId)
				if collErr == nil {
					// Remove item
					k.Item.Remove(ctx, item.Id)                                         //nolint:errcheck
					k.ItemsByCollection.Remove(ctx, collections.Join(coll.Id, item.Id)) //nolint:errcheck
					k.ItemsByOwner.Remove(ctx, collections.Join(coll.Owner, item.Id))   //nolint:errcheck
					coll.ItemCount--
					coll.UpdatedAt = sdkCtx.BlockHeight()
					k.Collection.Set(ctx, coll.Id, coll) //nolint:errcheck
					k.CompactPositions(ctx, coll.Id)     //nolint:errcheck

					// Refund item deposit if TTL
					if isTTLCollection(coll) {
						ownerAddr, addrErr := k.addressCodec.StringToBytes(coll.Owner)
						if addrErr == nil {
							k.RefundSPARK(ctx, ownerAddr, params.PerItemDeposit) //nolint:errcheck
						}
					}
				}
			}
		}

		// Distribute appeal fee: 50% to sentinel, 50% burned
		sentinelReward := params.AppealFee.MulRaw(50).Quo(math.NewInt(100))
		burnAmount := params.AppealFee.Sub(sentinelReward)

		// Send sentinel reward
		if k.forumKeeper != nil && sentinelReward.IsPositive() {
			// Release the sentinel's bond commitment (no slash)
			k.forumKeeper.ReleaseBondCommitment(ctx, hr.Sentinel, hr.CommittedAmount, types.ModuleName, hr.Id) //nolint:errcheck
			// Reward sentinel from escrowed appeal fee
			sentinelAddr, addrErr := k.addressCodec.StringToBytes(hr.Sentinel)
			if addrErr == nil {
				k.RefundSPARK(ctx, sentinelAddr, sentinelReward) //nolint:errcheck
			}
		}

		// Burn remaining 50% (jurors compensated via x/rep DREAM minting, no SPARK jury pool)
		if burnAmount.IsPositive() {
			k.BurnSPARK(ctx, burnAmount) //nolint:errcheck
		}

		hr.Resolved = true
		k.HideRecord.Set(ctx, hr.Id, hr)                                           //nolint:errcheck
		k.HideRecordExpiry.Remove(ctx, collections.Join(hr.AppealDeadline, hr.Id)) //nolint:errcheck

		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("hide_appeal_rejected",
			sdk.NewAttribute("hide_record_id", strconv.FormatUint(hr.Id, 10)),
			sdk.NewAttribute("target_id", strconv.FormatUint(hr.TargetId, 10)),
			sdk.NewAttribute("target_type", hr.TargetType.String()),
			sdk.NewAttribute("sentinel_reward", sentinelReward.String()),
			sdk.NewAttribute("target_deleted", "true"),
		))
	}

	return nil
}

// resolveContentOwnerAddr returns the owner address for a target (collection or item's parent collection owner).
func (k Keeper) resolveContentOwnerAddr(ctx context.Context, targetType types.FlagTargetType, targetID uint64) sdk.AccAddress {
	switch targetType {
	case types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION:
		coll, err := k.Collection.Get(ctx, targetID)
		if err == nil {
			addr, err := k.addressCodec.StringToBytes(coll.Owner)
			if err == nil {
				return addr
			}
		}
	case types.FlagTargetType_FLAG_TARGET_TYPE_ITEM:
		item, err := k.Item.Get(ctx, targetID)
		if err == nil {
			coll, err := k.Collection.Get(ctx, item.CollectionId)
			if err == nil {
				addr, err := k.addressCodec.StringToBytes(coll.Owner)
				if err == nil {
					return addr
				}
			}
		}
	}
	return nil
}
