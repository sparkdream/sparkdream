package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// SPARK pay for bonded collect curators.
//
// Before this existed the role was pure downside: a curator posted slashable
// DREAM, earned nothing for rating a collection, and on winning a challenge got
// back only their own committed bond while the challenger's deposit was burned.
// The only economic signal attached to the role was punishment, which is not a
// structure that staffs expert judgment work.
//
// Sized equal to the sentinel pool rather than scaled to the curator's smaller
// bond: rating a collection and hiding a post are comparable calls on
// comparable evidence. It stays a separate pool with its own accuracy bar so
// the two cannot cross-subsidise and neither is retuned by the other's
// parameters.
//
// Accuracy comes from challenge outcomes reported by x/collect into the shared
// RoleActivity record. A curator with no contested rating in the window earns
// nothing here — an unchallenged rating is not evidence of accuracy, and paying
// for it would pay most for rating whatever nobody bothers to dispute.
//
// The pool is filled automatically from the community pool each block (see
// role_reward_funding.go) and is also an ordinary bank sub-address, so a
// council can top it up with a plain send.

// GetCuratorRewardPool returns the pool's current uspark balance.
func (k Keeper) GetCuratorRewardPool(ctx context.Context) math.Int {
	return k.bankKeeper.GetBalance(ctx, CuratorRewardPoolAddress(), k.BondDenom(ctx)).Amount
}

func (k Keeper) curatorRewardEpochBlocks(params types.Params) uint64 {
	if params.CuratorRewardEpochBlocks == 0 {
		// Stored params predating this field: fall back to the sentinel cadence
		// rather than dividing by zero below.
		if params.SentinelRewardEpochBlocks > 0 {
			return params.SentinelRewardEpochBlocks
		}
		return 14400
	}
	return params.CuratorRewardEpochBlocks
}

// IsCuratorRewardEpoch reports whether this block closes a reward epoch.
func (k Keeper) IsCuratorRewardEpoch(ctx context.Context) bool {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return false
	}
	blocks := k.curatorRewardEpochBlocks(params)
	h := sdk.UnwrapSDKContext(ctx).BlockHeight()
	return h > 0 && uint64(h)%blocks == 0
}

// CurrentCuratorRewardEpoch is the epoch number this block falls in.
func (k Keeper) CurrentCuratorRewardEpoch(ctx context.Context) uint64 {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0
	}
	return uint64(sdk.UnwrapSDKContext(ctx).BlockHeight()) / k.curatorRewardEpochBlocks(params)
}

type curatorRewardCandidate struct {
	addr         string
	score        math.LegacyDec
	accuracyRate math.LegacyDec
}

// DistributeCuratorRewards splits the pool across curators whose windowed
// accuracy clears min_curator_accuracy, weighted by accuracy, then resets the
// per-epoch counters on every curator regardless of eligibility.
//
// Idempotency: a double invocation on the same block would distribute twice;
// the EndBlocker guarantees a single call per boundary.
func (k Keeper) DistributeCuratorRewards(ctx context.Context) error {
	if !k.IsCuratorRewardEpoch(ctx) {
		return nil
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("load params: %w", err)
	}
	epochNum := k.CurrentCuratorRewardEpoch(ctx)

	var (
		eligible    []curatorRewardCandidate
		allCurators []string
		totalScore  = math.LegacyZeroDec()
	)

	prefix := collections.NewPrefixedPairRange[int32, string](int32(types.RoleType_ROLE_TYPE_COLLECT_CURATOR))
	if err := k.BondedRoles.Walk(ctx, prefix, func(key collections.Pair[int32, string], br types.BondedRole) (bool, error) {
		addr := key.K2()
		allCurators = append(allCurators, addr)

		// Only a role actively backing liability earns from the pool. RECOVERY,
		// UNBONDING and DEMOTED all mean the bond is not standing behind new
		// ratings.
		if br.BondStatus != types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL {
			return false, nil
		}

		if _, raErr := k.GetRoleActivity(ctx, types.RoleType_ROLE_TYPE_COLLECT_CURATOR, addr); raErr != nil {
			return false, nil
		}

		// Accuracy is measured over a rolling window rather than lifetime, so a
		// long-tenured curator's denominator cannot dilute recent overturns and
		// an inactive one ages out as their in-window ratings fall off.
		upheld, overturned := k.GetRoleWindowedAccuracy(
			ctx, types.RoleType_ROLE_TYPE_COLLECT_CURATOR, addr, epochNum, params.CuratorAccuracyWindowEpochs)
		decided := upheld + overturned
		if decided == 0 {
			// No contested verdict in the window. Unchallenged work is not
			// evidence of accuracy — treating it as such would pay most for
			// rating whatever nobody bothers to challenge.
			return false, nil
		}

		accuracy := math.LegacyNewDec(int64(upheld)).Quo(math.LegacyNewDec(int64(decided)))
		minAccuracy := params.MinCuratorAccuracy
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
		eligible = append(eligible, curatorRewardCandidate{addr: addr, score: score, accuracyRate: accuracy})
		totalScore = totalScore.Add(score)
		return false, nil
	}); err != nil {
		return fmt.Errorf("walk curators: %w", err)
	}

	pool := k.GetCuratorRewardPool(ctx)
	switch {
	case pool.IsZero():
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			"curator_rewards_skipped",
			sdk.NewAttribute("epoch", fmt.Sprintf("%d", epochNum)),
			sdk.NewAttribute("reason", "pool_empty"),
		))
	case totalScore.IsZero():
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			"curator_rewards_skipped",
			sdk.NewAttribute("epoch", fmt.Sprintf("%d", epochNum)),
			sdk.NewAttribute("reason", "no_eligible_curators"),
		))
	default:
		for _, c := range eligible {
			allocation := c.score.Quo(totalScore).MulInt(pool).TruncateInt()
			if !allocation.IsPositive() {
				continue
			}
			if err := k.payoutCuratorReward(ctx, c, allocation, epochNum); err != nil {
				sdkCtx.Logger().Error("curator reward payout failed",
					"curator", c.addr, "amount", allocation.String(), "error", err)
			}
		}
	}

	// Reset per-epoch counters on EVERY curator, eligible or not, or an
	// ineligible one would carry stale epoch activity into the next window.
	for _, addr := range allCurators {
		if err := k.ResetRoleEpochCounters(ctx, types.RoleType_ROLE_TYPE_COLLECT_CURATOR, addr); err != nil {
			return err
		}
	}
	return nil
}

// payoutCuratorReward moves uspark from the pool sub-address to the curator
// and records it on the role.
func (k Keeper) payoutCuratorReward(ctx context.Context, c curatorRewardCandidate, amount math.Int, epochNum uint64) error {
	addr, err := sdk.AccAddressFromBech32(c.addr)
	if err != nil {
		return err
	}
	coins := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), amount))
	if err := k.bankKeeper.SendCoins(ctx, CuratorRewardPoolAddress(), addr, coins); err != nil {
		return err
	}

	key := collections.Join(int32(types.RoleType_ROLE_TYPE_COLLECT_CURATOR), c.addr)
	if br, bErr := k.BondedRoles.Get(ctx, key); bErr == nil {
		cumulative, _ := parseIntOrZero(br.CumulativeRewards)
		br.CumulativeRewards = cumulative.Add(amount).String()
		br.LastRewardEpoch = int64(epochNum)
		if err := k.BondedRoles.Set(ctx, key, br); err != nil {
			return err
		}
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		"curator_reward_paid",
		sdk.NewAttribute("curator", c.addr),
		sdk.NewAttribute("amount", amount.String()),
		sdk.NewAttribute("accuracy", c.accuracyRate.String()),
		sdk.NewAttribute("epoch", fmt.Sprintf("%d", epochNum)),
	))
	return nil
}

// BurnCuratorRewardPoolOverflow caps the curator pool the way the sentinel
// pool is capped: above max_curator_reward_pool, a fraction of the excess is
// burned each epoch and the rest stays to be distributed.
//
// The cap exists so an over-funded pool cannot sit as a standing prize that
// makes the role worth farming rather than doing; the burn is partial so a
// temporary spike is not destroyed outright.
func (k Keeper) BurnCuratorRewardPoolOverflow(ctx context.Context) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("load params: %w", err)
	}
	maxPool := params.MaxCuratorRewardPool
	burnRatio := params.CuratorRewardPoolOverflowBurnRatio
	if maxPool.IsNil() || burnRatio.IsNil() {
		return nil
	}

	current := k.GetCuratorRewardPool(ctx)
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
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, CuratorRewardPoolAddress(),
		types.ModuleName, coins); err != nil {
		return fmt.Errorf("move curator overflow to module account: %w", err)
	}
	if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, coins); err != nil {
		return fmt.Errorf("burn curator reward pool overflow: %w", err)
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		"curator_reward_pool_overflow_burned",
		sdk.NewAttribute("amount", burnAmount.String()),
		sdk.NewAttribute("pool_before", current.String()),
		sdk.NewAttribute("cap", maxPool.String()),
	))
	return nil
}
