package keeper

import (
	"context"
	"strconv"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/collect/types"
	reptypes "sparkdream/x/rep/types"
)

// isChallengePending reports whether a review carries an unresolved curation
// challenge: the challenger's deposit is still locked and the curator's
// committed slash still reserved. Resolution (jury verdict or timeout) either
// overturns the review or clears Challenger, so a settled challenge is never
// paid out twice.
func isChallengePending(review types.CurationReview) bool {
	return review.Challenged && !review.Overturned && review.Challenger != ""
}

// backfillChallengeDeadlines indexes challenges opened before
// CurationChallengeDeadline existed (or imported through genesis, which does
// not carry the index). It scans at most ChallengeBackfillScanPerBlock reviews
// per block from a stored cursor and marks itself done once it reaches the end.
// Pre-index challenges have no recorded filing height, so their deadline is
// the latest possible one: review creation + challenge window + timeout.
func (k Keeper) backfillChallengeDeadlines(ctx context.Context, params types.Params) {
	cursor, err := k.CurationChallengeBackfill.Get(ctx)
	if err != nil {
		cursor = 0
	}
	if cursor == types.ChallengeBackfillDone {
		return
	}

	scanned := 0
	next := types.ChallengeBackfillDone
	err = k.CurationReview.Walk(ctx, new(collections.Range[uint64]).StartInclusive(cursor),
		func(id uint64, review types.CurationReview) (bool, error) {
			if scanned >= types.ChallengeBackfillScanPerBlock {
				next = id
				return true, nil
			}
			scanned++
			if isChallengePending(review) {
				deadline := review.CreatedAt + params.ChallengeWindowBlocks + types.ChallengeResolutionTimeoutBlocks
				k.CurationChallengeDeadline.Set(ctx, collections.Join(deadline, id)) //nolint:errcheck
			}
			return false, nil
		},
	)
	if err != nil {
		return // retry from the same cursor next block
	}
	k.CurationChallengeBackfill.Set(ctx, next) //nolint:errcheck
}

// timeoutExpiredChallenges resolves curation challenges whose deadline has
// passed without a verdict. Shares the EndBlocker prune budget.
func (k Keeper) timeoutExpiredChallenges(
	ctx context.Context,
	currentBlock int64,
	params types.Params,
	pruned, cap uint32,
) (uint32, error) {
	var entries []collections.Pair[int64, uint64]
	err := k.CurationChallengeDeadline.Walk(ctx, nil, func(key collections.Pair[int64, uint64]) (bool, error) {
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
		k.CurationChallengeDeadline.Remove(ctx, entry) //nolint:errcheck
		pruned++

		review, err := k.CurationReview.Get(ctx, entry.K2())
		if err != nil || !isChallengePending(review) {
			// Review deleted with its collection (deposit refunded there) or
			// already resolved — stale entry.
			continue
		}
		k.resolveChallengeNotAdjudicated(ctx, review, params)
	}

	return pruned, nil
}

// resolveChallengeNotAdjudicated settles a challenge nobody ruled on: neither
// side was proven right, so the curator's committed slash is released and the
// challenger's deposit is returned. No curator outcome is recorded. The review
// stays challenged (it cannot be re-challenged) with Challenger cleared,
// matching the state ResolveChallengeResult leaves a settled challenge in.
func (k Keeper) resolveChallengeNotAdjudicated(ctx context.Context, review types.CurationReview, params types.Params) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	committed := review.CommittedSlash
	if committed.IsNil() {
		committed = math.ZeroInt()
	}
	if committed.IsPositive() {
		if err := k.repKeeper.ReleaseBond(ctx, reptypes.RoleType_ROLE_TYPE_COLLECT_CURATOR,
			review.Curator, committed); err != nil {
			sdkCtx.Logger().Warn("curation challenge timeout: bond release failed",
				"review_id", review.Id, "curator", review.Curator, "amount", committed.String(), "error", err)
		}
	}

	challenger := review.Challenger
	refunded := math.ZeroInt()
	if challengerAddr, err := k.addressCodec.StringToBytes(challenger); err == nil {
		if err := k.repKeeper.UnlockDREAM(ctx, challengerAddr, params.ChallengeDeposit); err != nil {
			sdkCtx.Logger().Warn("curation challenge timeout: deposit unlock failed",
				"review_id", review.Id, "challenger", challenger, "error", err)
		} else {
			refunded = params.ChallengeDeposit
		}
	}

	review.Challenger = ""
	review.CommittedSlash = math.ZeroInt()
	if err := k.CurationReview.Set(ctx, review.Id, review); err != nil {
		sdkCtx.Logger().Error("curation challenge timeout: failed to update review",
			"review_id", review.Id, "error", err)
		return
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("challenge_timed_out",
		sdk.NewAttribute("review_id", strconv.FormatUint(review.Id, 10)),
		sdk.NewAttribute("collection_id", strconv.FormatUint(review.CollectionId, 10)),
		sdk.NewAttribute("curator", review.Curator),
		sdk.NewAttribute("challenger", challenger),
		sdk.NewAttribute("bond_released", committed.String()),
		sdk.NewAttribute("challenger_refunded", refunded.String()),
	))
}
