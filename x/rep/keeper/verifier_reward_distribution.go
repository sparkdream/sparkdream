package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Pay for bonded federation verifiers: a SPARK pool plus a flat DREAM
// stipend, distributed together on one cadence.
//
// The role used to be paid in DREAM alone. That made it the only bonded role
// where doing the job is structurally SPARK-negative for the holder: a
// verifier fetches a peer's content off-chain, hashes it, and pays SPARK gas
// per submission, and was compensated in a token that cannot be sold and
// cannot buy gas. The bridge operator on the other side of the same exchange
// is paid in SPARK. Sentinels, curators and reviewers act on on-chain state,
// which is free to read, and they are paid in SPARK too.
//
// It lives in x/rep rather than x/federation because the accuracy it scores
// comes from the shared RoleActivity record rep owns, and because a
// distribution resets that record's per-epoch counters. Two modules
// distributing for one role on two independently-editable cadences would both
// reset those counters and neither would read a coherent window.
//
// SCORE:
//
//	score = 1 + accuracy * sqrt(epoch_appeals_resolved)
//
// The flat 1 is the point. The obvious formula -- verified_count * accuracy --
// is the one x/rep already rejected for curators, and it is worse here:
// verification is mechanical (match a hash), a verifier with no decided
// challenge scores as fully accurate, and challenges are rare. Paying per
// verification on that curve pays most for high-volume rubber-stamping, which
// is precisely the failure the role exists to prevent. So volume enters only
// as the min_epoch_verifications FLOOR, never as a weight: the flat term buys
// availability and covers gas, and the contested term rewards judgment that
// somebody actually tested.
//
// A consequence worth being explicit about: a verifier doing the minimum and
// one doing ten times the minimum earn the same base. That is deliberate. It
// mirrors the DREAM stipend, which has always been flat per eligible verifier.
//
// If challenges never materialise, every eligible verifier scores 1 and the
// pool splits evenly -- the pool is never stranded, and an empty roster draws
// nothing at all because funding is headroom-proportional.

// GetVerifierRewardPool returns the pool's current uspark balance.
func (k Keeper) GetVerifierRewardPool(ctx context.Context) math.Int {
	return k.bankKeeper.GetBalance(ctx, VerifierRewardPoolAddress(), k.BondDenom(ctx)).Amount
}

func (k Keeper) verifierRewardEpochBlocks(params types.Params) uint64 {
	if params.VerifierRewardEpochBlocks == 0 {
		// Stored params predating this field: fall back to the sentinel
		// cadence rather than dividing by zero below.
		if params.SentinelRewardEpochBlocks > 0 {
			return params.SentinelRewardEpochBlocks
		}
		return 14400
	}
	return params.VerifierRewardEpochBlocks
}

// IsVerifierRewardEpoch reports whether this block closes a reward epoch.
func (k Keeper) IsVerifierRewardEpoch(ctx context.Context) bool {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return false
	}
	h := sdk.UnwrapSDKContext(ctx).BlockHeight()
	return h > 0 && uint64(h)%k.verifierRewardEpochBlocks(params) == 0
}

// CurrentVerifierRewardEpoch is the epoch number this block falls in. Also
// read by roleRewardEpoch, so the accuracy ring is stamped in the same units
// the distribution below reads it in.
func (k Keeper) CurrentVerifierRewardEpoch(ctx context.Context) uint64 {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0
	}
	h := sdk.UnwrapSDKContext(ctx).BlockHeight()
	if h <= 0 {
		return 0
	}
	return uint64(h) / k.verifierRewardEpochBlocks(params)
}

type verifierRewardCandidate struct {
	addr         string
	score        math.LegacyDec
	accuracyRate math.LegacyDec
	bondStatus   types.BondedRoleStatus
	currentBond  math.Int
}

// DistributeVerifierRewards splits the SPARK pool across eligible verifiers by
// score, mints each of them the flat DREAM stipend, then resets the per-epoch
// counters on every verifier regardless of eligibility.
//
// Idempotency: a double invocation on the same block would distribute twice;
// the EndBlocker guarantees a single call per boundary.
func (k Keeper) DistributeVerifierRewards(ctx context.Context) error {
	if !k.IsVerifierRewardEpoch(ctx) {
		return nil
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("load params: %w", err)
	}
	epochNum := k.CurrentVerifierRewardEpoch(ctx)

	var (
		eligible     []verifierRewardCandidate
		allVerifiers []string
		totalScore   = math.LegacyZeroDec()
	)

	prefix := collections.NewPrefixedPairRange[int32, string](int32(types.RoleType_ROLE_TYPE_FEDERATION_VERIFIER))
	if err := k.BondedRoles.Walk(ctx, prefix, func(key collections.Pair[int32, string], br types.BondedRole) (bool, error) {
		addr := key.K2()
		allVerifiers = append(allVerifiers, addr)

		// Gate 1: bond status. RECOVERY still earns -- that is the whole
		// point of the auto-bond path below, which lets a slashed verifier
		// rebuild by working rather than by fronting DREAM. UNBONDING and
		// DEMOTED mean the bond is not standing behind new work.
		if br.BondStatus != types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL &&
			br.BondStatus != types.BondedRoleStatus_BONDED_ROLE_STATUS_RECOVERY {
			return false, nil
		}

		ra, raErr := k.GetRoleActivity(ctx, types.RoleType_ROLE_TYPE_FEDERATION_VERIFIER, addr)
		if raErr != nil {
			return false, nil
		}

		// Gate 2: epoch work floor. The only place volume is consulted.
		if uint32(ra.EpochActions[types.ActionKindFederationVerify]) < params.MinEpochVerifications {
			return false, nil
		}

		// Gate 3: windowed accuracy. Rolling rather than lifetime, so a
		// long-tenured verifier's denominator cannot dilute recent overturns.
		// No decided challenge in the window means nobody has demonstrated
		// this verifier wrong -- treat as fully accurate, since most
		// verifications are never challenged.
		accuracyRate := math.LegacyOneDec()
		up, ov := k.GetRoleWindowedAccuracy(ctx, types.RoleType_ROLE_TYPE_FEDERATION_VERIFIER,
			addr, epochNum, params.VerifierAccuracyWindowEpochs)
		if decided := up + ov; decided > 0 {
			accuracyRate = math.LegacyNewDec(int64(up)).Quo(math.LegacyNewDec(int64(decided)))
			if !params.MinVerifierAccuracy.IsNil() && accuracyRate.LT(params.MinVerifierAccuracy) {
				return false, nil
			}
		}

		// Gate 4: no slash in the window being paid for. Stamped by SlashBond
		// on the shared record as floor(height / epoch_blocks).
		//
		// The window is NOT epochNum. This runs at height N*epoch_blocks, so
		// epochNum == N, but the counters being distributed accrued over
		// heights [(N-1)*epoch_blocks, N*epoch_blocks) -- every one of which
		// stamps N-1. Matching on == epochNum caught only a slash landing in
		// this exact boundary block and let the whole epoch through, so accept
		// either N-1 (the closed epoch) or N (this block).
		//
		// The != 0 guard is a sentinel, not an epoch test: LastSlashEpoch is a
		// plain int64 with no "never slashed" encoding, so a slash in epoch 0
		// is indistinguishable from an unslashed verifier. That only matters
		// for the first two distributions on a fresh chain.
		if ra.LastSlashEpoch != 0 && uint64(ra.LastSlashEpoch)+1 >= epochNum {
			return false, nil
		}

		// score = 1 (flat) + accuracy * sqrt(contested verdicts resolved).
		score := math.LegacyOneDec()
		if ra.EpochAppealsResolved > 0 {
			sqrtResolved, sErr := math.LegacyNewDec(int64(ra.EpochAppealsResolved)).ApproxSqrt()
			if sErr != nil {
				sdkCtx.Logger().Warn("verifier reward: sqrt failed", "verifier", addr, "error", sErr)
			} else {
				score = score.Add(accuracyRate.Mul(sqrtResolved))
			}
		}

		currentBond, _ := parseIntOrZero(br.CurrentBond)
		eligible = append(eligible, verifierRewardCandidate{
			addr:         addr,
			score:        score,
			accuracyRate: accuracyRate,
			bondStatus:   br.BondStatus,
			currentBond:  currentBond,
		})
		totalScore = totalScore.Add(score)
		return false, nil
	}); err != nil {
		return fmt.Errorf("walk bonded verifiers: %w", err)
	}

	switch {
	case len(eligible) == 0:
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("verifier_reward_epoch_skipped",
			sdk.NewAttribute("epoch", fmt.Sprintf("%d", epochNum)),
			sdk.NewAttribute("reason", "no_eligible_verifiers"),
		))
	default:
		k.payVerifierSpark(ctx, eligible, totalScore, epochNum)
		k.payVerifierDream(ctx, params, eligible, epochNum)
	}

	// Reset per-epoch counters on EVERY verifier, eligible or not, or an
	// ineligible one would carry stale epoch activity into the next window.
	for _, addr := range allVerifiers {
		if err := k.ResetRoleEpochCounters(ctx, types.RoleType_ROLE_TYPE_FEDERATION_VERIFIER, addr); err != nil {
			return err
		}
	}
	return nil
}

// payVerifierSpark splits the pool pro-rata on score. Per-verifier failures
// are logged and skipped so one bad address cannot abort the distribution.
func (k Keeper) payVerifierSpark(
	ctx context.Context,
	eligible []verifierRewardCandidate,
	totalScore math.LegacyDec,
	epochNum uint64,
) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	pool := k.GetVerifierRewardPool(ctx)
	if !pool.IsPositive() || !totalScore.IsPositive() {
		return
	}
	for _, c := range eligible {
		allocation := c.score.Quo(totalScore).MulInt(pool).TruncateInt()
		if !allocation.IsPositive() {
			continue
		}
		if err := k.payoutVerifierSparkReward(ctx, c, allocation, epochNum); err != nil {
			sdkCtx.Logger().Error("verifier SPARK reward payout failed",
				"verifier", c.addr, "amount", allocation.String(), "error", err)
		}
	}
}

// payoutVerifierSparkReward moves uspark from the pool sub-address to the
// verifier. SPARK is paid straight out even in RECOVERY: it reimburses gas and
// infrastructure already spent, and withholding it would make recovery harder
// exactly when the holder is least able to fund the work.
func (k Keeper) payoutVerifierSparkReward(
	ctx context.Context,
	c verifierRewardCandidate,
	amount math.Int,
	epochNum uint64,
) error {
	addr, err := sdk.AccAddressFromBech32(c.addr)
	if err != nil {
		return err
	}
	coins := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), amount))
	if err := k.bankKeeper.SendCoins(ctx, VerifierRewardPoolAddress(), addr, coins); err != nil {
		return err
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		"verifier_spark_reward_paid",
		sdk.NewAttribute("verifier", c.addr),
		sdk.NewAttribute("amount", amount.String()),
		sdk.NewAttribute("accuracy", c.accuracyRate.String()),
		sdk.NewAttribute("epoch", fmt.Sprintf("%d", epochNum)),
	))
	return nil
}

// payVerifierDream mints the flat per-verifier DREAM stipend, scaling every
// verifier down equally when the roster would push the epoch past the mint
// cap. Auto-bonds into RECOVERY bonds until min_bond is restored.
func (k Keeper) payVerifierDream(
	ctx context.Context,
	params types.Params,
	eligible []verifierRewardCandidate,
	epochNum uint64,
) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	reward := params.VerifierDreamReward
	if reward.IsNil() || !reward.IsPositive() {
		return
	}
	if !params.MaxVerifierDreamMintPerEpoch.IsNil() {
		requested := reward.Mul(math.NewInt(int64(len(eligible))))
		if requested.GT(params.MaxVerifierDreamMintPerEpoch) {
			reward = params.MaxVerifierDreamMintPerEpoch.Quo(math.NewInt(int64(len(eligible))))
		}
	}
	if !reward.IsPositive() {
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("verifier_reward_epoch_skipped",
			sdk.NewAttribute("epoch", fmt.Sprintf("%d", epochNum)),
			sdk.NewAttribute("reason", "reward_zero_after_scaling"),
		))
		return
	}

	// Auto-bond target comes from the role's own config, which x/federation
	// write-throughs from min_verifier_bond.
	minBond := math.ZeroInt()
	if cfg, err := k.GetBondedRoleConfig(ctx, types.RoleType_ROLE_TYPE_FEDERATION_VERIFIER); err == nil {
		if v, ok := math.NewIntFromString(cfg.MinBond); ok && !v.IsNegative() {
			minBond = v
		}
	}

	for _, c := range eligible {
		if err := k.payoutVerifierDreamReward(ctx, c, reward, minBond, epochNum); err != nil {
			sdkCtx.Logger().Error("verifier DREAM reward payout failed",
				"verifier", c.addr, "amount", reward.String(), "error", err)
		}
	}
}

// payoutVerifierDreamReward mints the stipend, then re-locks the portion
// needed to restore min_bond when the verifier is in RECOVERY.
func (k Keeper) payoutVerifierDreamReward(
	ctx context.Context,
	c verifierRewardCandidate,
	reward math.Int,
	minBond math.Int,
	epochNum uint64,
) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	addr, err := sdk.AccAddressFromBech32(c.addr)
	if err != nil {
		return err
	}

	// Mint the full reward first; the auto-bond then re-locks from the same
	// balance, which keeps the bookkeeping symmetric and surfaces the whole
	// reward in lifetime_earned.
	if err := k.MintDREAM(ctx, addr, reward); err != nil {
		return fmt.Errorf("mint DREAM: %w", err)
	}

	autoBond := math.ZeroInt()
	if c.bondStatus == types.BondedRoleStatus_BONDED_ROLE_STATUS_RECOVERY && c.currentBond.LT(minBond) {
		autoBond = minBond.Sub(c.currentBond)
		if autoBond.GT(reward) {
			autoBond = reward
		}
	}
	if autoBond.IsPositive() {
		if err := k.IncreaseBond(ctx, types.RoleType_ROLE_TYPE_FEDERATION_VERIFIER, c.addr, autoBond); err != nil {
			// The payout is already in the verifier's balance; a failed
			// auto-bond leaves them short of restoration but must not abort
			// the distribution for everyone else.
			sdkCtx.Logger().Warn("verifier reward auto-bond failed; payout retained as available balance",
				"verifier", c.addr, "auto_bond", autoBond.String(), "error", err)
			autoBond = math.ZeroInt()
		}
	}

	if err := k.RecordRewardPayout(ctx, types.RoleType_ROLE_TYPE_FEDERATION_VERIFIER,
		c.addr, int64(epochNum), reward); err != nil {
		sdkCtx.Logger().Warn("record verifier reward payout failed",
			"verifier", c.addr, "error", err)
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"verifier_dream_reward_paid",
		sdk.NewAttribute("verifier", c.addr),
		sdk.NewAttribute("amount", reward.String()),
		sdk.NewAttribute("epoch", fmt.Sprintf("%d", epochNum)),
	))
	if autoBond.IsPositive() {
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			"verifier_dream_reward_auto_bonded",
			sdk.NewAttribute("verifier", c.addr),
			sdk.NewAttribute("amount", autoBond.String()),
			sdk.NewAttribute("epoch", fmt.Sprintf("%d", epochNum)),
		))
		// Crossing min_bond flips RECOVERY -> NORMAL inside IncreaseBond;
		// surface it, since "am I out of recovery yet" is the question a
		// verifier working their way back is actually asking.
		if br, err := k.GetBondedRole(ctx, types.RoleType_ROLE_TYPE_FEDERATION_VERIFIER, c.addr); err == nil &&
			br.BondStatus == types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL {
			sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
				"verifier_bond_restored",
				sdk.NewAttribute("verifier", c.addr),
				sdk.NewAttribute("new_bond", br.CurrentBond),
			))
		}
	}
	return nil
}

// BurnVerifierRewardPoolOverflow caps the verifier pool the way the sentinel
// and curator pools are capped: above max_verifier_reward_pool a fraction of
// the excess is burned each epoch and the rest stays to be distributed, so an
// over-funded pool cannot sit as a standing prize that makes the role worth
// farming rather than doing.
func (k Keeper) BurnVerifierRewardPoolOverflow(ctx context.Context) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("load params: %w", err)
	}
	maxPool := params.MaxVerifierRewardPool
	burnRatio := params.VerifierRewardPoolOverflowBurnRatio
	if maxPool.IsNil() || burnRatio.IsNil() {
		return nil
	}

	current := k.GetVerifierRewardPool(ctx)
	if !current.GT(maxPool) {
		return nil
	}
	burnAmount := burnRatio.MulInt(current.Sub(maxPool)).TruncateInt()
	if !burnAmount.IsPositive() {
		return nil
	}

	// Module-aware send: a plain SendCoins to the raw module address creates a
	// BaseAccount there and the BurnCoins below then panics resolving it as a
	// module account.
	coins := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), burnAmount))
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, VerifierRewardPoolAddress(),
		types.ModuleName, coins); err != nil {
		return fmt.Errorf("move verifier overflow to module account: %w", err)
	}
	if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, coins); err != nil {
		return fmt.Errorf("burn verifier reward pool overflow: %w", err)
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		"verifier_reward_pool_overflow_burned",
		sdk.NewAttribute("amount", burnAmount.String()),
	))
	return nil
}
