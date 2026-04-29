package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/rep/types"
)

// IsSentinelRewardEpoch reports whether the current block is a sentinel-reward
// distribution epoch boundary. Returns false for block 0 regardless of params.
func (k Keeper) IsSentinelRewardEpoch(ctx context.Context) bool {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return false
	}
	blocks := params.SentinelRewardEpochBlocks
	if blocks == 0 {
		return false
	}
	height := sdk.UnwrapSDKContext(ctx).BlockHeight()
	if height <= 0 {
		return false
	}
	return uint64(height)%blocks == 0
}

// sentinelRewardCandidate bundles an eligible sentinel with its computed score.
type sentinelRewardCandidate struct {
	addr  string
	score math.LegacyDec
	// Captured for event emission.
	accuracyRate math.LegacyDec
}

// DistributeSentinelRewards distributes the rep module's uspark reward pool
// pro-rata on an accuracy-weighted score to eligible sentinels, then resets
// forum-side per-epoch counters on ALL sentinels (regardless of eligibility).
//
// Runs only on sentinel-reward epoch boundaries (see IsSentinelRewardEpoch).
// Eligibility gates (evaluated in order) and the score formula are documented
// in docs/x-forum-spec.md (Stage D).
//
// Idempotency: double-invocation on the same block would distribute twice; the
// EndBlocker guarantees a single call per boundary.
func (k Keeper) DistributeSentinelRewards(ctx context.Context) error {
	if !k.IsSentinelRewardEpoch(ctx) {
		return nil
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("load params: %w", err)
	}

	epochBlocks := params.SentinelRewardEpochBlocks
	epochNum := uint64(sdkCtx.BlockHeight()) / epochBlocks

	// Walk all sentinels once: collect addrs + evaluate eligibility.
	var (
		eligibles    []sentinelRewardCandidate
		allSentinels []string
		totalScore   = math.LegacyZeroDec()
	)

	sentinelPrefix := collections.NewPrefixedPairRange[int32, string](int32(types.RoleType_ROLE_TYPE_FORUM_SENTINEL))
	err = k.BondedRoles.Walk(ctx, sentinelPrefix, func(key collections.Pair[int32, string], br types.BondedRole) (bool, error) {
		addr := key.K2()
		allSentinels = append(allSentinels, addr)

		// Forum-side counters (decoupled snapshot).
		if k.late.forumKeeper == nil {
			// No forum keeper wired -> cannot evaluate eligibility, but we still
			// want to proceed with other phases (caller can fix wiring later).
			return false, nil
		}
		counters, cerr := k.late.forumKeeper.GetSentinelActivityCounters(ctx, addr)
		if cerr != nil {
			sdkCtx.Logger().Warn("sentinel reward: counters lookup failed",
				"sentinel", addr, "error", cerr)
			return false, nil
		}

		// Gate 1: Counter availability — if all zero (no forum record), skip.
		if counters == (types.SentinelActivityCounters{}) {
			return false, nil
		}

		// Gate 2: Min appeals for accuracy.
		totalDecided := counters.UpheldHides + counters.OverturnedHides +
			counters.UpheldLocks + counters.OverturnedLocks +
			counters.UpheldMoves + counters.OverturnedMoves
		if totalDecided < params.MinAppealsForAccuracy {
			return false, nil
		}

		// Gate 3: Epoch activity.
		epochActivity := counters.EpochHides + counters.EpochLocks +
			counters.EpochMoves + counters.EpochPins
		if epochActivity < params.MinEpochActivityForReward {
			return false, nil
		}

		// Gate 4: Appeal rate on hides (anti-gaming) — skip when appeal_rate
		// is below the floor. Only hide actions are checked here; locks and
		// moves are separately rate-limited.
		if counters.EpochHides > 0 {
			appealRate := math.LegacyNewDec(int64(counters.EpochAppealsFiled)).
				Quo(math.LegacyNewDec(int64(counters.EpochHides)))
			if appealRate.LT(params.MinAppealRate) {
				return false, nil
			}
		}

		// Gate 5: Accuracy.
		totalUpheld := counters.UpheldHides + counters.UpheldLocks + counters.UpheldMoves
		accuracyRate := math.LegacyNewDec(int64(totalUpheld)).
			Quo(math.LegacyNewDec(int64(totalDecided)))
		if accuracyRate.LT(params.MinSentinelAccuracy) {
			return false, nil
		}

		// Gate 6: Bond status.
		if br.BondStatus == types.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED {
			return false, nil
		}

		// Score = accuracy_rate * sqrt(epoch_appeals_resolved)
		//       + epoch_hides * 0.01 + epoch_locks * 0.05 + epoch_moves * 0.03
		resolvedDec := math.LegacyNewDec(int64(counters.EpochAppealsResolved))
		sqrtResolved, serr := resolvedDec.ApproxSqrt()
		if serr != nil {
			sdkCtx.Logger().Warn("sentinel reward: sqrt failed",
				"sentinel", addr, "error", serr)
			return false, nil
		}
		score := accuracyRate.Mul(sqrtResolved)

		hideBonus := math.LegacyNewDec(int64(counters.EpochHides)).
			Mul(math.LegacyNewDecWithPrec(1, 2)) // 0.01
		lockBonus := math.LegacyNewDec(int64(counters.EpochLocks)).
			Mul(math.LegacyNewDecWithPrec(5, 2)) // 0.05
		moveBonus := math.LegacyNewDec(int64(counters.EpochMoves)).
			Mul(math.LegacyNewDecWithPrec(3, 2)) // 0.03

		score = score.Add(hideBonus).Add(lockBonus).Add(moveBonus)

		if !score.IsPositive() {
			return false, nil
		}

		eligibles = append(eligibles, sentinelRewardCandidate{
			addr:         addr,
			score:        score,
			accuracyRate: accuracyRate,
		})
		totalScore = totalScore.Add(score)
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("walk sentinels: %w", err)
	}

	pool := k.GetSentinelRewardPool(ctx)

	// Distribute only when both sides are live.
	switch {
	case pool.IsZero():
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("sentinel_reward_epoch_skipped",
			sdk.NewAttribute("epoch", fmt.Sprintf("%d", epochNum)),
			sdk.NewAttribute("reason", "pool_empty"),
		))
	case totalScore.IsZero():
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("sentinel_reward_epoch_skipped",
			sdk.NewAttribute("epoch", fmt.Sprintf("%d", epochNum)),
			sdk.NewAttribute("reason", "no_eligible_sentinels"),
		))
	default:
		for _, c := range eligibles {
			allocation := c.score.Quo(totalScore).MulInt(pool).TruncateInt()
			if !allocation.IsPositive() {
				continue
			}
			if err := k.payoutSentinelReward(ctx, c, allocation, epochNum); err != nil {
				sdkCtx.Logger().Error("sentinel reward payout failed",
					"sentinel", c.addr, "amount", allocation.String(), "error", err)
				// Continue — do not abort distribution on a per-sentinel failure.
				continue
			}
		}
	}

	// Reset forum-side epoch counters on EVERY sentinel regardless of
	// eligibility/distribution outcome.
	if k.late.forumKeeper != nil {
		for _, addr := range allSentinels {
			if err := k.late.forumKeeper.ResetSentinelEpochCounters(ctx, addr); err != nil {
				sdkCtx.Logger().Warn("sentinel reward: reset epoch counters failed",
					"sentinel", addr, "error", err)
			}
		}
	}

	return nil
}

// payoutSentinelReward transfers `amount` uspark from the rep module account
// to the sentinel, updates CumulativeRewards + LastRewardEpoch on the
// BondedRole (ROLE_TYPE_FORUM_SENTINEL) record, and emits a
// `sentinel_reward_distributed` event.
func (k Keeper) payoutSentinelReward(
	ctx context.Context,
	c sentinelRewardCandidate,
	amount math.Int,
	epochNum uint64,
) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	sentinelAddr, err := sdk.AccAddressFromBech32(c.addr)
	if err != nil {
		return fmt.Errorf("invalid sentinel address %q: %w", c.addr, err)
	}

	coins := sdk.NewCoins(sdk.NewCoin(types.RewardDenom, amount))
	if err := k.bankKeeper.SendCoins(ctx, SentinelRewardPoolAddress(), sentinelAddr, coins); err != nil {
		return fmt.Errorf("send coins: %w", err)
	}

	key := collections.Join(int32(types.RoleType_ROLE_TYPE_FORUM_SENTINEL), c.addr)
	br, err := k.BondedRoles.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("load bonded role: %w", err)
	}
	prev, err := parseIntOrZero(br.CumulativeRewards)
	if err != nil {
		return fmt.Errorf("invalid cumulative_rewards on bonded role: %w", err)
	}
	br.CumulativeRewards = prev.Add(amount).String()
	br.LastRewardEpoch = int64(epochNum)
	if err := k.BondedRoles.Set(ctx, key, br); err != nil {
		return fmt.Errorf("persist bonded role: %w", err)
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("sentinel_reward_distributed",
		sdk.NewAttribute("sentinel", c.addr),
		sdk.NewAttribute("amount", amount.String()),
		sdk.NewAttribute("score", c.score.String()),
		sdk.NewAttribute("accuracy_rate", c.accuracyRate.String()),
		sdk.NewAttribute("epoch", fmt.Sprintf("%d", epochNum)),
	))
	return nil
}
