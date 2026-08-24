package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// The SPARK half of reviewer pay.
//
// The DREAM fee (see initiative_review.go) pays for the *act* of reviewing and
// is deliberately outcome-blind — paid per verdict filed, never per approval.
// This pool pays for reviewing *well*: it is gated on windowed accuracy from
// challenge outcomes, so a reviewer who files plausible verdicts without reading
// the deliverable earns the fee and nothing else.
//
// Held and tuned separately from the sentinel pool. The roles are separate
// because the liability differs by orders of magnitude — a wrong approval mints
// DREAM that cannot be clawed back — and sharing a pool or an accuracy bar
// would undo that.
//
// The pool is filled automatically from the community pool each block (see
// role_reward_funding.go). It is also an ordinary bank sub-address, so a council
// can top it up with a plain send; no bespoke funding message is needed either
// way, and an empty pool distributes nothing rather than erroring.

// GetReviewerRewardPool returns the pool's current uspark balance.
func (k Keeper) GetReviewerRewardPool(ctx context.Context) math.Int {
	return k.bankKeeper.GetBalance(ctx, ReviewerRewardPoolAddress(), k.BondDenom(ctx)).Amount
}

func (k Keeper) reviewerRewardEpochBlocks(params types.Params) uint64 {
	if params.ReviewerRewardEpochBlocks == 0 {
		// Stored params predating this field: fall back to the sentinel cadence
		// rather than dividing by zero below.
		if params.SentinelRewardEpochBlocks > 0 {
			return params.SentinelRewardEpochBlocks
		}
		return 14400
	}
	return params.ReviewerRewardEpochBlocks
}

// IsReviewerRewardEpoch reports whether this block closes a reward epoch.
func (k Keeper) IsReviewerRewardEpoch(ctx context.Context) bool {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return false
	}
	blocks := k.reviewerRewardEpochBlocks(params)
	h := sdk.UnwrapSDKContext(ctx).BlockHeight()
	return h > 0 && uint64(h)%blocks == 0
}

// CurrentReviewerRewardEpoch is the epoch number this block falls in.
func (k Keeper) CurrentReviewerRewardEpoch(ctx context.Context) uint64 {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0
	}
	return uint64(sdk.UnwrapSDKContext(ctx).BlockHeight()) / k.reviewerRewardEpochBlocks(params)
}

type reviewerRewardCandidate struct {
	addr         string
	score        math.LegacyDec
	accuracyRate math.LegacyDec
}

// DistributeReviewerRewards splits the pool across reviewers whose windowed
// accuracy clears min_reviewer_accuracy, weighted by accuracy, then resets the
// per-epoch counters on every reviewer regardless of eligibility.
//
// Idempotency: a double invocation on the same block would distribute twice;
// the EndBlocker guarantees a single call per boundary.
func (k Keeper) DistributeReviewerRewards(ctx context.Context) error {
	if !k.IsReviewerRewardEpoch(ctx) {
		return nil
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("load params: %w", err)
	}
	epochNum := k.CurrentReviewerRewardEpoch(ctx)

	var (
		eligible     []reviewerRewardCandidate
		allReviewers []string
		totalScore   = math.LegacyZeroDec()
	)

	prefix := collections.NewPrefixedPairRange[int32, string](int32(types.RoleType_ROLE_TYPE_INITIATIVE_REVIEWER))
	if err := k.BondedRoles.Walk(ctx, prefix, func(key collections.Pair[int32, string], br types.BondedRole) (bool, error) {
		addr := key.K2()
		allReviewers = append(allReviewers, addr)

		// Only a role actively backing liability earns from the pool. RECOVERY,
		// UNBONDING and DEMOTED all mean the bond is not standing behind new
		// verdicts.
		if br.BondStatus != types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL {
			return false, nil
		}

		if _, raErr := k.GetRoleActivity(ctx, types.RoleType_ROLE_TYPE_INITIATIVE_REVIEWER, addr); raErr != nil {
			return false, nil
		}

		// Accuracy is measured over a rolling window rather than lifetime, so a
		// long-tenured reviewer's denominator cannot dilute recent overturns and
		// an inactive one ages out as their in-window verdicts fall off.
		upheld, overturned := k.GetRoleWindowedAccuracy(
			ctx, types.RoleType_ROLE_TYPE_INITIATIVE_REVIEWER, addr, epochNum, params.ReviewerAccuracyWindowEpochs)
		decided := upheld + overturned
		if decided == 0 {
			// No contested verdict in the window. Unchallenged work is not
			// evidence of accuracy — treating it as such would pay most for
			// reviewing whatever nobody bothers to challenge.
			return false, nil
		}

		accuracy := math.LegacyNewDec(int64(upheld)).Quo(math.LegacyNewDec(int64(decided)))
		minAccuracy := params.MinReviewerAccuracy
		if minAccuracy.IsNil() {
			minAccuracy = math.LegacyNewDecWithPrec(70, 2)
		}
		if accuracy.LT(minAccuracy) {
			return false, nil
		}

		// Weight by accuracy against the square root of decided verdicts, the
		// same damping the sentinel pool uses: volume should count, but not
		// linearly, or the pool concentrates on whoever files most.
		sqrtDecided, sErr := math.LegacyNewDec(int64(decided)).ApproxSqrt()
		if sErr != nil {
			return false, nil
		}
		score := accuracy.Mul(sqrtDecided)
		if !score.IsPositive() {
			return false, nil
		}
		eligible = append(eligible, reviewerRewardCandidate{addr: addr, score: score, accuracyRate: accuracy})
		totalScore = totalScore.Add(score)
		return false, nil
	}); err != nil {
		return fmt.Errorf("walk reviewers: %w", err)
	}

	pool := k.GetReviewerRewardPool(ctx)
	switch {
	case pool.IsZero():
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			"reviewer_rewards_skipped",
			sdk.NewAttribute("epoch", fmt.Sprintf("%d", epochNum)),
			sdk.NewAttribute("reason", "pool_empty"),
		))
	case totalScore.IsZero():
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			"reviewer_rewards_skipped",
			sdk.NewAttribute("epoch", fmt.Sprintf("%d", epochNum)),
			sdk.NewAttribute("reason", "no_eligible_reviewers"),
		))
	default:
		for _, c := range eligible {
			allocation := c.score.Quo(totalScore).MulInt(pool).TruncateInt()
			if !allocation.IsPositive() {
				continue
			}
			if err := k.payoutReviewerReward(ctx, c, allocation, epochNum); err != nil {
				sdkCtx.Logger().Error("reviewer reward payout failed",
					"reviewer", c.addr, "amount", allocation.String(), "error", err)
			}
		}
	}

	// Reset per-epoch counters on EVERY reviewer, eligible or not, or an
	// ineligible one would carry stale epoch activity into the next window.
	for _, addr := range allReviewers {
		if err := k.ResetRoleEpochCounters(ctx, types.RoleType_ROLE_TYPE_INITIATIVE_REVIEWER, addr); err != nil {
			return err
		}
	}
	return nil
}

// payoutReviewerReward moves uspark from the pool sub-address to the reviewer
// and records it on the role.
func (k Keeper) payoutReviewerReward(ctx context.Context, c reviewerRewardCandidate, amount math.Int, epochNum uint64) error {
	addr, err := sdk.AccAddressFromBech32(c.addr)
	if err != nil {
		return err
	}
	coins := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), amount))
	if err := k.bankKeeper.SendCoins(ctx, ReviewerRewardPoolAddress(), addr, coins); err != nil {
		return err
	}

	key := collections.Join(int32(types.RoleType_ROLE_TYPE_INITIATIVE_REVIEWER), c.addr)
	if br, bErr := k.BondedRoles.Get(ctx, key); bErr == nil {
		cumulative, _ := parseIntOrZero(br.CumulativeRewards)
		br.CumulativeRewards = cumulative.Add(amount).String()
		br.LastRewardEpoch = int64(epochNum)
		if err := k.BondedRoles.Set(ctx, key, br); err != nil {
			return err
		}
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		"reviewer_reward_paid",
		sdk.NewAttribute("reviewer", c.addr),
		sdk.NewAttribute("amount", amount.String()),
		sdk.NewAttribute("accuracy", c.accuracyRate.String()),
		sdk.NewAttribute("epoch", fmt.Sprintf("%d", epochNum)),
	))
	return nil
}

// BurnReviewerRewardPoolOverflow caps the reviewer pool the way the sentinel
// pool is capped: above max_reviewer_reward_pool, a fraction of the excess is
// burned each epoch and the rest stays to be distributed.
//
// The cap exists so an over-funded pool cannot sit as a standing prize that
// makes the role worth farming rather than doing; the burn is partial so a
// temporary spike is not destroyed outright.
func (k Keeper) BurnReviewerRewardPoolOverflow(ctx context.Context) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("load params: %w", err)
	}
	maxPool := params.MaxReviewerRewardPool
	burnRatio := params.ReviewerRewardPoolOverflowBurnRatio
	if maxPool.IsNil() || burnRatio.IsNil() {
		return nil
	}

	current := k.GetReviewerRewardPool(ctx)
	if !current.GT(maxPool) {
		return nil
	}
	burnAmount := burnRatio.MulInt(current.Sub(maxPool)).TruncateInt()
	if !burnAmount.IsPositive() {
		return nil
	}

	// BurnCoins needs a module account holding Burner, so route through the rep
	// module account. Both ops happen inside this call, so nothing observes the
	// intermediate balance.
	coins := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), burnAmount))
	// Module-aware send: a plain SendCoins to the raw module address creates a
	// BaseAccount there and the BurnCoins below then panics resolving it as a
	// module account.
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, ReviewerRewardPoolAddress(),
		types.ModuleName, coins); err != nil {
		return fmt.Errorf("move reviewer overflow to module account: %w", err)
	}
	if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, coins); err != nil {
		return fmt.Errorf("burn reviewer reward pool overflow: %w", err)
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		"reviewer_reward_pool_overflow_burned",
		sdk.NewAttribute("amount", burnAmount.String()),
		sdk.NewAttribute("pool_before", current.String()),
		sdk.NewAttribute("cap", maxPool.String()),
	))
	return nil
}
