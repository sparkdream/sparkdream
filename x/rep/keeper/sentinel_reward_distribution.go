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

// CurrentSentinelRewardEpoch returns the current reward-epoch index
// (blockHeight / SentinelRewardEpochBlocks). Used both to bucket resolved
// appeals into the accuracy ring and to read the rolling window at distribution
// time, so a resolution and the distribution that follows agree on the epoch.
// Returns 0 if params are unreadable or the cadence is unset.
func (k Keeper) CurrentSentinelRewardEpoch(ctx context.Context) uint64 {
	params, err := k.Params.Get(ctx)
	if err != nil || params.SentinelRewardEpochBlocks == 0 {
		return 0
	}
	height := sdk.UnwrapSDKContext(ctx).BlockHeight()
	if height <= 0 {
		return 0
	}
	return uint64(height) / params.SentinelRewardEpochBlocks
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
// the rep-local RoleActivity per-epoch counters on ALL sentinels (regardless
// of eligibility). Fully rep-internal: eligibility, accuracy, and activity
// all read the shared RoleActivity record — no forum-keeper counter pull.
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

	sentinelPrefix := collections.NewPrefixedPairRange[int32, string](int32(types.RoleType_ROLE_TYPE_CONTENT_SENTINEL))
	err = k.BondedRoles.Walk(ctx, sentinelPrefix, func(key collections.Pair[int32, string], br types.BondedRole) (bool, error) {
		addr := key.K2()
		allSentinels = append(allSentinels, addr)

		// Shared accountability record — rep-local now (RoleActivity); the
		// forum-keeper counter pull is retired.
		ra, raErr := k.GetRoleActivity(ctx, types.RoleType_ROLE_TYPE_CONTENT_SENTINEL, addr)
		if raErr != nil {
			sdkCtx.Logger().Warn("sentinel reward: role activity lookup failed",
				"sentinel", addr, "error", raErr)
			return false, nil
		}

		// Gate 1: Activity-record availability — no reported actions, skip.
		if len(ra.TotalActions) == 0 && ra.EpochAppealsResolved == 0 {
			return false, nil
		}

		// Gate 2: Min appeals for accuracy — measured over the rolling window
		// (last SentinelAccuracyWindowEpochs reward epochs), NOT lifetime. This
		// keeps accuracy responsive to recent behavior: a long-tenured sentinel's
		// huge lifetime denominator no longer dilutes recent overturns, and a
		// sentinel who goes inactive ages out of eligibility as their in-window
		// resolved appeals fall off.
		windowUpheld, windowOverturned := k.GetRoleWindowedAccuracy(
			ctx, types.RoleType_ROLE_TYPE_CONTENT_SENTINEL, addr, epochNum, params.SentinelAccuracyWindowEpochs)
		totalDecided := windowUpheld + windowOverturned
		if totalDecided < params.MinAppealsForAccuracy {
			return false, nil
		}

		// Gate 3: Epoch activity — the role holder's own moderation work
		// across every surface (forum actions + collect hides). Appeal-filed
		// kinds are excluded: appeals against the sentinel are not activity.
		var epochActivity uint64
		for _, kind := range types.ActivityKinds {
			epochActivity += ra.EpochActions[kind]
		}
		if epochActivity < params.MinEpochActivityForReward {
			return false, nil
		}

		// Gate 4: Appeal rate on hides (anti-gaming) — skip when appeal_rate
		// is below the floor. Cross-surface now that both surfaces report
		// hides AND appeals-filed: (forum+collect appeals)/(forum+collect
		// hides). Locks and moves are separately rate-limited.
		var epochHides, epochAppealsFiled uint64
		for _, kind := range types.HideKinds {
			epochHides += ra.EpochActions[kind]
		}
		for _, kind := range types.AppealFiledKinds {
			epochAppealsFiled += ra.EpochActions[kind]
		}
		if epochHides > 0 {
			appealRate := math.LegacyNewDec(int64(epochAppealsFiled)).
				Quo(math.LegacyNewDec(int64(epochHides)))
			if appealRate.LT(params.MinAppealRate) {
				return false, nil
			}
		}

		// Gate 5: Accuracy — windowed upheld / windowed decided.
		accuracyRate := math.LegacyNewDec(int64(windowUpheld)).
			Quo(math.LegacyNewDec(int64(totalDecided)))
		if accuracyRate.LT(params.MinSentinelAccuracy) {
			return false, nil
		}

		// Gate 6: Bond status.
		if br.BondStatus == types.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED {
			return false, nil
		}

		// Score = accuracy_rate * sqrt(epoch_appeals_resolved)
		//       + sum(epoch_actions[kind] * ScoreWeights[kind])
		// (weights preserved from the pre-migration formula; kinds without a
		// weight — pins, appeal-filed — contribute no bonus).
		resolvedDec := math.LegacyNewDec(int64(ra.EpochAppealsResolved))
		sqrtResolved, serr := resolvedDec.ApproxSqrt()
		if serr != nil {
			sdkCtx.Logger().Warn("sentinel reward: sqrt failed",
				"sentinel", addr, "error", serr)
			return false, nil
		}
		score := accuracyRate.Mul(sqrtResolved)

		for kind, weight := range types.ScoreWeights {
			if n := ra.EpochActions[kind]; n > 0 {
				score = score.Add(math.LegacyNewDec(int64(n)).Mul(weight))
			}
		}

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

	// Reset the rep-local per-epoch counters on EVERY sentinel regardless of
	// eligibility/distribution outcome.
	for _, addr := range allSentinels {
		if err := k.ResetRoleEpochCounters(ctx, types.RoleType_ROLE_TYPE_CONTENT_SENTINEL, addr); err != nil {
			sdkCtx.Logger().Warn("sentinel reward: reset epoch counters failed",
				"sentinel", addr, "error", err)
		}
	}

	return nil
}

// payoutSentinelReward transfers `amount` uspark from the rep module account
// to the sentinel, updates CumulativeRewards + LastRewardEpoch on the
// BondedRole (ROLE_TYPE_CONTENT_SENTINEL) record, and emits a
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

	coins := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), amount))
	if err := k.bankKeeper.SendCoins(ctx, SentinelRewardPoolAddress(), sentinelAddr, coins); err != nil {
		return fmt.Errorf("send coins: %w", err)
	}

	key := collections.Join(int32(types.RoleType_ROLE_TYPE_CONTENT_SENTINEL), c.addr)
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
