package keeper

import (
	"context"
	"errors"
	"sort"
	"strconv"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/collect/types"
	reptypes "sparkdream/x/rep/types"
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
			// Update status index: PENDING → ACTIVE (pinned unchanged).
			oldStatus := coll.Status
			coll.Status = types.CollectionStatus_COLLECTION_STATUS_ACTIVE
			if err := k.MoveCollectionStatusIndex(ctx, oldStatus, coll.Pinned, coll.Status, coll.Pinned, coll.Id); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("OnMembershipGranted: k.MoveCollectionStatusIndex failed", "err", err)
			}

			// Remove from EndorsementPending (stored deadline) and refund the
			// escrowed endorsement fee: the endorsement stage is over and
			// nothing else would ever release it.
			k.clearEndorsementPending(ctx, &coll, true)
			if fee := escrowedEndorsementFee(coll); fee.IsPositive() && k.chargesDeposits(coll) {
				if ownerAddr, aerr := k.addressCodec.StringToBytes(coll.Owner); aerr == nil {
					if rerr := k.RefundSPARK(ctx, ownerAddr, fee); rerr != nil {
						sdkCtx.Logger().Error("failed to refund endorsement fee on membership grant",
							"collection_id", coll.Id, "error", rerr)
					} else {
						coll.EndorsementFeeEscrowed = math.ZeroInt()
					}
				}
			}

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

// ResolveChallengeResult resolves a curation challenge. Production caller:
// MsgResolveCurationChallenge (gov, Commons Council or Commons Operations
// Committee policy), which also drops the CurationChallengeDeadline entry.
// If upheld (challenger wins): review overturned, curator's committed bond
// slashed (via BondedRole), challenger rewarded.
// If rejected (curator wins): review stands, committed bond released back to
// the curator, challenge deposit burned.
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
	if !isChallengePending(review) {
		// Already settled (verdict delivered or timed out, see
		// challenge_timeout.go) — never pay out a challenge twice.
		return errorsmod.Wrap(types.ErrReviewNotFound, "challenge already resolved")
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return errorsmod.Wrap(err, "failed to get params")
	}

	challengerAddr, err := k.addressCodec.StringToBytes(review.Challenger)
	if err != nil {
		return errorsmod.Wrap(err, "invalid challenger address")
	}

	// Load per-module counters; start from a zero record if first time.
	activity, getErr := k.CuratorActivity.Get(ctx, review.Curator)
	if getErr != nil && !errors.Is(getErr, collections.ErrNotFound) {
		return getErr
	}
	if activity.Address == "" {
		activity.Address = review.Curator
	}

	committed := review.CommittedSlash
	if committed.IsNil() {
		committed = math.ZeroInt()
	}
	deposit := challengeDepositOf(review, params)

	if upheld {
		// Challenger wins: review overturned, committed bond is slashed.
		review.Overturned = true

		slashAmount := committed
		rewardAmount := params.ChallengeRewardFraction.MulInt(slashAmount).TruncateInt()
		burnAmount := slashAmount.Sub(rewardAmount)

		if slashAmount.IsPositive() {
			if err := k.late.repKeeper.SlashBond(ctx, reptypes.RoleType_ROLE_TYPE_COLLECT_CURATOR,
				review.Curator, slashAmount, "curation_overturned"); err != nil {
				sdkCtx.Logger().Warn("curation slash failed",
					"curator", review.Curator, "amount", slashAmount.String(), "error", err)
			}
		}

		// Reward the challenger with their share of the slashed commit.
		// SlashBond burns the whole commit, so the reward is minted as fresh
		// DREAM (net supply change: -burnAmount). Unlocking would only free
		// DREAM the challenger already owned.
		if rewardAmount.IsPositive() {
			if err := k.late.repKeeper.MintDREAM(ctx, challengerAddr, rewardAmount); err != nil {
				sdkCtx.Logger().Warn("curation challenge reward mint failed",
					"challenger", review.Challenger, "amount", rewardAmount.String(), "error", err)
			}
		}

		// Counter updates.
		activity.OverturnedReviews++
		activity.ConsecutiveOverturns++
		activity.ConsecutiveUpheld = 0

		// Report the outcome into x/rep's shared RoleActivity as well. The
		// local CuratorActivity above drives collect's demotion streak; the
		// shared record is what the curator SPARK pool reads for windowed
		// accuracy. Best-effort: an accounting failure must not roll back the
		// jury's verdict.
		if err := k.late.repKeeper.RecordRoleOutcome(ctx, reptypes.RoleType_ROLE_TYPE_COLLECT_CURATOR,
			review.Curator, reptypes.ActionKindCollectCuration, false); err != nil {
			sdkCtx.Logger().Warn("curator outcome (overturned) not recorded on RoleActivity",
				"curator", review.Curator, "error", err)
		}

		// Refund the challenge deposit actually locked.
		if err := k.late.repKeeper.UnlockDREAM(ctx, challengerAddr, deposit); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("ResolveChallengeResult: repKeeper.UnlockDREAM failed", "err", err)
		}

		// Update curation summary (recalculate with overturned review excluded).
		k.recalculateSummary(ctx, review.CollectionId)

		// Clear committed_slash and the deposit on the review since both have
		// been settled.
		review.CommittedSlash = math.ZeroInt()
		review.ChallengeDeposit = math.ZeroInt()
		if err := k.CurationReview.Set(ctx, reviewID, review); err != nil {
			return errorsmod.Wrap(err, "failed to update review")
		}

		// Consecutive-overturn demotion.
		demoted := false
		if params.CuratorOverturnDemotionStreak > 0 &&
			activity.ConsecutiveOverturns >= params.CuratorOverturnDemotionStreak {
			cooldownUntil := sdkCtx.BlockTime().Unix() + params.CuratorDemotionCooldown
			if err := k.late.repKeeper.SetBondStatus(ctx, reptypes.RoleType_ROLE_TYPE_COLLECT_CURATOR,
				review.Curator, reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED, cooldownUntil); err != nil {
				sdkCtx.Logger().Warn("curator demotion failed",
					"curator", review.Curator, "error", err)
			} else {
				demoted = true
			}
		}

		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("challenge_resolved",
			sdk.NewAttribute("review_id", strconv.FormatUint(reviewID, 10)),
			sdk.NewAttribute("upheld", "true"),
			sdk.NewAttribute("curator", review.Curator),
			sdk.NewAttribute("slash_amount", slashAmount.String()),
			sdk.NewAttribute("reward_amount", rewardAmount.String()),
			sdk.NewAttribute("burn_amount", burnAmount.String()),
			sdk.NewAttribute("curator_demoted", strconv.FormatBool(demoted)),
			sdk.NewAttribute("challenger_refunded", deposit.String()),
		))
	} else {
		// Curator wins: review stands. Release the reserved commit back to
		// the curator's available bond.
		if committed.IsPositive() {
			if err := k.late.repKeeper.ReleaseBond(ctx, reptypes.RoleType_ROLE_TYPE_COLLECT_CURATOR,
				review.Curator, committed); err != nil {
				sdkCtx.Logger().Warn("curation release failed",
					"curator", review.Curator, "amount", committed.String(), "error", err)
			}
		}

		// Counter updates.
		activity.UpheldReviews++
		activity.ConsecutiveUpheld++
		activity.ConsecutiveOverturns = 0

		// Mirror into x/rep's shared RoleActivity — the curator SPARK pool
		// reads windowed accuracy from there, and it needs the upheld side as
		// much as the overturned one or every curator looks 0% accurate.
		if err := k.late.repKeeper.RecordRoleOutcome(ctx, reptypes.RoleType_ROLE_TYPE_COLLECT_CURATOR,
			review.Curator, reptypes.ActionKindCollectCuration, true); err != nil {
			sdkCtx.Logger().Warn("curator outcome (upheld) not recorded on RoleActivity",
				"curator", review.Curator, "error", err)
		}

		// Burn the challenge deposit actually locked, out of the lock (the
		// locked and total balances drop together, as the endorser slash in
		// deleteCollectionFull does).
		if err := k.late.repKeeper.BurnLockedDREAM(ctx, challengerAddr, deposit); err != nil {
			sdkCtx.Logger().Error("failed to burn challenge deposit",
				"review_id", reviewID, "challenger", review.Challenger, "error", err)
		}

		// Clear committed_slash on the review, and the challenger: the deposit
		// is settled (burned), so collection deletion and the timeout sweep
		// must not refund it again.
		review.CommittedSlash = math.ZeroInt()
		review.ChallengeDeposit = math.ZeroInt()
		review.Challenger = ""
		if err := k.CurationReview.Set(ctx, reviewID, review); err != nil {
			return errorsmod.Wrap(err, "failed to update review")
		}

		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("challenge_resolved",
			sdk.NewAttribute("review_id", strconv.FormatUint(reviewID, 10)),
			sdk.NewAttribute("upheld", "false"),
			sdk.NewAttribute("curator", review.Curator),
			sdk.NewAttribute("slash_amount", "0"),
			sdk.NewAttribute("reward_amount", "0"),
			sdk.NewAttribute("burn_amount", deposit.String()),
			sdk.NewAttribute("curator_demoted", "false"),
			sdk.NewAttribute("challenger_refunded", "0"),
		))
	}

	if err := k.CuratorActivity.Set(ctx, review.Curator, activity); err != nil {
		return errorsmod.Wrap(err, "failed to update curator activity")
	}

	return nil
}

// recalculateSummary recomputes the CurationSummary for a collection from scratch.
func (k Keeper) recalculateSummary(ctx context.Context, collectionID uint64) {
	var upCount, downCount uint32
	tagCounts := make(map[string]uint32)
	var lastReviewedAt int64

	if err := k.CurationReviewsByCollection.Walk(ctx,
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
	); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("recalculateSummary: CurationReviewsByCollection.Walk failed", "err", err)
	}

	topTags := make([]types.TagCount, 0, len(tagCounts))
	for tag, count := range tagCounts {
		topTags = append(topTags, types.TagCount{Tag: tag, Count: count})
	}
	// Map order differs between nodes: sort (count desc, then tag) so the
	// stored summary is identical everywhere.
	sort.Slice(topTags, func(i, j int) bool {
		if topTags[i].Count != topTags[j].Count {
			return topTags[i].Count > topTags[j].Count
		}
		return topTags[i].Tag < topTags[j].Tag
	})

	summary := types.CurationSummary{
		CollectionId:   collectionID,
		UpCount:        upCount,
		DownCount:      downCount,
		TopTags:        topTags,
		LastReviewedAt: lastReviewedAt,
	}
	if err := k.CurationSummary.Set(ctx, collectionID, summary); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("recalculateSummary: CurationSummary.Set failed", "err", err)
	}
}
