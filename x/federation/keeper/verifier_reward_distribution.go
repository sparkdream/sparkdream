package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/federation/types"
	reptypes "sparkdream/x/rep/types"
)

// IsVerifierRewardEpoch reports whether the current block is a Phase 10
// reward-distribution boundary. Returns false for block 0 and when the
// cadence is zero. Cadence is build-tag dependent — see
// getVerifierRewardEpochBlocks in genesis_vals_*.go.
func (k Keeper) IsVerifierRewardEpoch(ctx context.Context) bool {
	blocks := types.GetVerifierRewardEpochBlocks()
	if blocks == 0 {
		return false
	}
	height := sdk.UnwrapSDKContext(ctx).BlockHeight()
	if height <= 0 {
		return false
	}
	return uint64(height)%blocks == 0
}

// CurrentVerifierRewardEpoch returns the Phase 10 epoch number for the
// current block (floor(height / cadence)). Returns 0 when the cadence is
// zero. Used by the challenge-resolution path to stamp LastSlashEpoch in
// a way Phase 10 can compare against.
func (k Keeper) CurrentVerifierRewardEpoch(ctx context.Context) int64 {
	blocks := types.GetVerifierRewardEpochBlocks()
	if blocks == 0 {
		return 0
	}
	height := sdk.UnwrapSDKContext(ctx).BlockHeight()
	if height <= 0 {
		return 0
	}
	return height / int64(blocks)
}

// verifierRewardCandidate bundles an eligible verifier with the state
// captured for payout and event emission.
type verifierRewardCandidate struct {
	addr         string
	bondedRole   reptypes.BondedRole
	accuracyRate math.LegacyDec
}

// DistributeVerifierRewards implements Phase 10 of the federation
// EndBlocker (spec §9.11). Mints DREAM to eligible verifiers, auto-bonds
// the payout into RECOVERY-status bonds until min_verifier_bond is
// restored, and resets per-epoch counters on every verifier — regardless
// of eligibility — so the next epoch starts clean.
//
// Eligibility gates evaluated in order: bond record exists, BondStatus
// not DEMOTED, EpochVerifications >= min_epoch_verifications, accuracy
// >= min_verifier_accuracy, LastSlashEpoch != currentEpoch (no slashing
// this epoch — stamped by the challenge-resolution path).
//
// Idempotency: the EndBlocker guarantees a single call per height; the
// cadence check ensures at most one fire per epoch. Per-verifier failures
// are logged and skipped — a single bad address must not abort the
// distribution.
func (k Keeper) DistributeVerifierRewards(ctx context.Context) error {
	if !k.IsVerifierRewardEpoch(ctx) {
		return nil
	}
	if k.late.repKeeper == nil {
		return nil
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("load params: %w", err)
	}

	epochBlocks := types.GetVerifierRewardEpochBlocks()
	if epochBlocks == 0 {
		return nil
	}
	epochNum := uint64(sdkCtx.BlockHeight()) / epochBlocks
	currentEpoch := int64(epochNum)

	var (
		eligibles    []verifierRewardCandidate
		allVerifiers []string
	)
	err = k.VerifierActivity.Walk(ctx, nil, func(addr string, activity types.VerifierActivity) (bool, error) {
		allVerifiers = append(allVerifiers, addr)

		// Gate 1: epoch activity threshold.
		if uint32(activity.EpochVerifications) < params.MinEpochVerifications {
			return false, nil
		}

		// Gate 2: accuracy — share of decided verifications upheld.
		// A verifier with no decided challenges (totalDecided == 0)
		// hasn't been demonstrated wrong; treat as 100% accurate so
		// they pass the gate. This matters in practice because most
		// verifications are never challenged (accurate-by-default).
		accuracyRate := math.LegacyOneDec()
		totalDecided := activity.UpheldVerifications + activity.OverturnedVerifications
		if totalDecided > 0 {
			accuracyRate = math.LegacyNewDec(int64(activity.UpheldVerifications)).
				Quo(math.LegacyNewDec(int64(totalDecided)))
			if !params.MinVerifierAccuracy.IsNil() && accuracyRate.LT(params.MinVerifierAccuracy) {
				return false, nil
			}
		}

		// Gate 3: no slashing this epoch. LastSlashEpoch is stamped by
		// the challenge-resolution path on every CHALLENGE_UPHELD; an
		// exact match against the current reward epoch disqualifies.
		if activity.LastSlashEpoch == currentEpoch {
			return false, nil
		}

		// Gate 4: bond record present and not DEMOTED.
		br, gerr := k.late.repKeeper.GetBondedRole(ctx, reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER, addr)
		if gerr != nil {
			return false, nil
		}
		if br.BondStatus == reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED ||
			br.BondStatus == reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_UNBONDING {
			return false, nil
		}

		eligibles = append(eligibles, verifierRewardCandidate{
			addr:         addr,
			bondedRole:   br,
			accuracyRate: accuracyRate,
		})
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("walk verifier activity: %w", err)
	}

	// Compute scaled reward: equal per-verifier amount up to the cap.
	rewardPerVerifier := params.VerifierDreamReward
	if !rewardPerVerifier.IsNil() && len(eligibles) > 0 && !params.MaxVerifierDreamMintPerEpoch.IsNil() {
		totalRequested := rewardPerVerifier.Mul(math.NewInt(int64(len(eligibles))))
		if totalRequested.GT(params.MaxVerifierDreamMintPerEpoch) {
			// Scale equally so the sum is at most the cap.
			rewardPerVerifier = params.MaxVerifierDreamMintPerEpoch.Quo(math.NewInt(int64(len(eligibles))))
		}
	}

	switch {
	case len(eligibles) == 0:
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("verifier_reward_epoch_skipped",
			sdk.NewAttribute("epoch", fmt.Sprintf("%d", epochNum)),
			sdk.NewAttribute("reason", "no_eligible_verifiers"),
		))
	case rewardPerVerifier.IsNil() || !rewardPerVerifier.IsPositive():
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("verifier_reward_epoch_skipped",
			sdk.NewAttribute("epoch", fmt.Sprintf("%d", epochNum)),
			sdk.NewAttribute("reason", "reward_zero_after_scaling"),
		))
	default:
		minBond := params.MinVerifierBond
		for _, c := range eligibles {
			if err := k.payoutVerifierReward(ctx, c, rewardPerVerifier, minBond, epochNum); err != nil {
				sdkCtx.Logger().Error("verifier reward payout failed",
					"verifier", c.addr, "amount", rewardPerVerifier.String(), "error", err)
				continue
			}
		}
	}

	// Reset per-epoch counters on every verifier — eligibility/distribution
	// outcome irrelevant. This guarantees the next epoch starts from zero
	// even for verifiers who fell below the activity threshold.
	for _, addr := range allVerifiers {
		activity, gerr := k.VerifierActivity.Get(ctx, addr)
		if gerr != nil {
			continue
		}
		if activity.EpochVerifications == 0 && activity.EpochChallengesResolved == 0 {
			continue
		}
		activity.EpochVerifications = 0
		activity.EpochChallengesResolved = 0
		if err := k.VerifierActivity.Set(ctx, addr, activity); err != nil {
			sdkCtx.Logger().Warn("verifier reward: reset epoch counters failed",
				"verifier", addr, "error", err)
		}
	}

	return nil
}

// payoutVerifierReward mints the per-verifier DREAM reward, auto-bonds
// the portion needed to restore min_verifier_bond when the verifier is
// in RECOVERY, and updates LastRewardEpoch + CumulativeRewards on the
// generic BondedRole record.
func (k Keeper) payoutVerifierReward(
	ctx context.Context,
	c verifierRewardCandidate,
	reward math.Int,
	minBond math.Int,
	epochNum uint64,
) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	verifierAddr, err := sdk.AccAddressFromBech32(c.addr)
	if err != nil {
		return fmt.Errorf("invalid verifier address %q: %w", c.addr, err)
	}

	// Mint the full reward to the verifier first. Auto-bond (if any)
	// then re-locks from the same balance — keeps the bookkeeping
	// symmetric and surfaces the full reward in lifetime_earned.
	if err := k.late.repKeeper.MintDREAM(ctx, verifierAddr, reward); err != nil {
		return fmt.Errorf("mint DREAM: %w", err)
	}

	autoBond := math.ZeroInt()
	if c.bondedRole.BondStatus == reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_RECOVERY && !minBond.IsNil() {
		currentBond, ok := math.NewIntFromString(c.bondedRole.CurrentBond)
		if !ok || currentBond.IsNegative() {
			currentBond = math.ZeroInt()
		}
		if currentBond.LT(minBond) {
			needed := minBond.Sub(currentBond)
			if needed.GT(reward) {
				autoBond = reward
			} else {
				autoBond = needed
			}
		}
	}

	if autoBond.IsPositive() {
		if err := k.late.repKeeper.IncreaseBond(ctx, reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER, c.addr, autoBond); err != nil {
			// Partial-payout fallback: payout is already in the verifier's
			// balance; auto-bond failure leaves them out of RECOVERY
			// restoration but does not abort the distribution. Log and
			// proceed so other verifiers still get paid.
			sdkCtx.Logger().Warn("verifier reward auto-bond failed; payout retained as available balance",
				"verifier", c.addr, "auto_bond", autoBond.String(), "error", err)
			autoBond = math.ZeroInt()
		}
	}

	// Update reward bookkeeping on the BondedRole record.
	if err := k.late.repKeeper.RecordRewardPayout(ctx,
		reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER, c.addr,
		int64(epochNum), reward); err != nil {
		sdkCtx.Logger().Warn("record reward payout failed",
			"verifier", c.addr, "error", err)
	}

	if autoBond.IsPositive() {
		payout := reward.Sub(autoBond)
		newBond := math.ZeroInt()
		if cb, ok := math.NewIntFromString(c.bondedRole.CurrentBond); ok && !cb.IsNegative() {
			newBond = cb.Add(autoBond)
		} else {
			newBond = autoBond
		}
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeVerifierDreamRewardAutoBonded,
			sdk.NewAttribute(types.AttributeKeyVerifier, c.addr),
			sdk.NewAttribute("auto_bonded", autoBond.String()),
			sdk.NewAttribute("payout", payout.String()),
			sdk.NewAttribute("new_bond", newBond.String()),
			sdk.NewAttribute("epoch", fmt.Sprintf("%d", epochNum)),
		))
		if !minBond.IsNil() && newBond.GTE(minBond) {
			sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeVerifierBondRestored,
				sdk.NewAttribute(types.AttributeKeyVerifier, c.addr),
				sdk.NewAttribute("bond", newBond.String()),
				sdk.NewAttribute("epoch", fmt.Sprintf("%d", epochNum)),
			))
		}
	} else {
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeVerifierDreamRewardPaid,
			sdk.NewAttribute(types.AttributeKeyVerifier, c.addr),
			sdk.NewAttribute(types.AttributeKeyAmount, reward.String()),
			sdk.NewAttribute("accuracy_rate", c.accuracyRate.String()),
			sdk.NewAttribute("epoch", fmt.Sprintf("%d", epochNum)),
		))
	}

	return nil
}
