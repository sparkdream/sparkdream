package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GetMaxInvitationCredits returns the maximum invitation credits for a trust level.
func GetMaxInvitationCredits(config types.TrustLevelConfig, level types.TrustLevel) uint32 {
	switch level {
	case types.TrustLevel_TRUST_LEVEL_NEW:
		return config.NewInvitationCredits
	case types.TrustLevel_TRUST_LEVEL_PROVISIONAL:
		return config.ProvisionalInvitationCredits
	case types.TrustLevel_TRUST_LEVEL_ESTABLISHED:
		return config.EstablishedInvitationCredits
	case types.TrustLevel_TRUST_LEVEL_TRUSTED:
		return config.TrustedInvitationCredits
	case types.TrustLevel_TRUST_LEVEL_CORE:
		return config.CoreInvitationCredits
	default:
		return 0
	}
}

// UpdateTrustLevel checks if a member is eligible for a trust level upgrade and updates it.
// Uses cached CompletedInterimsCount for O(1) lookup instead of walking all interims.
func (k Keeper) UpdateTrustLevel(ctx context.Context, memberAddr sdk.AccAddress) error {
	member, err := k.Member.Get(ctx, memberAddr.String())
	if err != nil {
		return types.ErrMemberNotFound
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	config := params.TrustLevelConfig

	// Get current season (placeholder until x/season is integrated)
	// For now we might use epoch or time, or just 0 if x/season is not ready.
	// Assuming 0 for now.
	currentSeason := uint32(0)

	// Logic to determine new trust level
	newLevel := member.TrustLevel

	// Use cached completed interims count - O(1) instead of O(all_interims)
	// This count is incremented when interims are completed via CompleteInterimDirectly
	completedInterims := member.CompletedInterimsCount

	// Helper to get total reputation across all tags
	getTotalReputation := func() (math.LegacyDec, error) {
		total := math.LegacyZeroDec()
		for _, repStr := range member.ReputationScores {
			rep, err := math.LegacyNewDecFromStr(repStr)
			if err != nil {
				return math.LegacyZeroDec(), err
			}
			total = total.Add(rep)
		}
		return total, nil
	}

	// Check eligibility for trust level upgrades based on config
	switch member.TrustLevel {
	case types.TrustLevel_TRUST_LEVEL_NEW:
		// NEW -> PROVISIONAL: Requires minimum reputation and completed interims
		totalRep, err := getTotalReputation()
		if err != nil {
			return err
		}

		if completedInterims >= config.ProvisionalMinInterims && totalRep.GTE(config.ProvisionalMinRep) {
			newLevel = types.TrustLevel_TRUST_LEVEL_PROVISIONAL
		}

	case types.TrustLevel_TRUST_LEVEL_PROVISIONAL:
		// PROVISIONAL -> ESTABLISHED: Requires minimum reputation and completed interims
		totalRep, err := getTotalReputation()
		if err != nil {
			return err
		}

		if completedInterims >= config.EstablishedMinInterims && totalRep.GTE(config.EstablishedMinRep) {
			newLevel = types.TrustLevel_TRUST_LEVEL_ESTABLISHED
		}

	case types.TrustLevel_TRUST_LEVEL_ESTABLISHED:
		// ESTABLISHED -> TRUSTED: Requires minimum seasons and reputation
		totalRep, err := getTotalReputation()
		if err != nil {
			return err
		}

		if currentSeason >= member.JoinedSeason &&
			uint32(currentSeason-member.JoinedSeason) >= config.TrustedMinSeasons &&
			totalRep.GTE(config.TrustedMinRep) {
			newLevel = types.TrustLevel_TRUST_LEVEL_TRUSTED
		}

	case types.TrustLevel_TRUST_LEVEL_TRUSTED:
		// TRUSTED -> CORE: Requires minimum seasons and reputation
		totalRep, err := getTotalReputation()
		if err != nil {
			return err
		}

		if currentSeason >= member.JoinedSeason &&
			uint32(currentSeason-member.JoinedSeason) >= config.CoreMinSeasons &&
			totalRep.GTE(config.CoreMinRep) {
			newLevel = types.TrustLevel_TRUST_LEVEL_CORE
		}
	}

	if newLevel > member.TrustLevel {
		oldLevel := member.TrustLevel
		member.TrustLevel = newLevel
		member.TrustLevelUpdatedAt = sdk.UnwrapSDKContext(ctx).BlockTime().Unix()

		// Grant invitation credits for the new trust level
		// Only grant if new level has more credits than old level
		oldMaxCredits := GetMaxInvitationCredits(config, oldLevel)
		newMaxCredits := GetMaxInvitationCredits(config, newLevel)
		if newMaxCredits > oldMaxCredits {
			// Grant the difference (bonus credits for upgrading)
			creditsToGrant := newMaxCredits - oldMaxCredits
			member.InvitationCredits += creditsToGrant
		}

		if err := k.Member.Set(ctx, memberAddr.String(), member); err != nil {
			return err
		}

		// Mark trust tree dirty — trust level changed affects anonymous posting ZK proofs
		k.MarkMemberDirty(ctx, memberAddr.String())

		sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
			sdk.NewEvent(
				"trust_level_updated",
				sdk.NewAttribute("member", memberAddr.String()),
				sdk.NewAttribute("old_level", oldLevel.String()),
				sdk.NewAttribute("new_level", newLevel.String()),
				sdk.NewAttribute("invitation_credits", fmt.Sprintf("%d", member.InvitationCredits)),
			),
		)
	}

	return nil
}

// GetCurrentSeason returns the current season number from the x/season module.
// Returns 0 if the season keeper is not available (optional dependency).
func (k Keeper) GetCurrentSeason(ctx context.Context) (int64, error) {
	if k.late.seasonKeeper == nil {
		return 0, nil // Fallback when x/season not wired
	}
	season, err := k.late.seasonKeeper.GetCurrentSeason(ctx)
	if err != nil {
		return 0, err
	}
	return int64(season.Number), nil
}

// EnsureInvitationCreditsReset lazily resets invitation credits if we're in a new season.
// This is called on-demand when a member tries to invite, avoiding O(n) bulk updates.
// Returns true if credits were reset, false otherwise.
func (k Keeper) EnsureInvitationCreditsReset(ctx context.Context, memberAddr string) (bool, error) {
	member, err := k.Member.Get(ctx, memberAddr)
	if err != nil {
		return false, types.ErrMemberNotFound
	}

	currentSeason, err := k.GetCurrentSeason(ctx)
	if err != nil {
		return false, err
	}

	// Check if we need to reset (new season since last reset)
	if member.LastCreditResetSeason >= currentSeason {
		return false, nil // Already reset this season
	}

	// Reset credits to trust-level max
	params, err := k.Params.Get(ctx)
	if err != nil {
		return false, err
	}
	config := params.TrustLevelConfig

	maxCredits := GetMaxInvitationCredits(config, member.TrustLevel)
	member.InvitationCredits = maxCredits
	member.LastCreditResetSeason = currentSeason

	if err := k.Member.Set(ctx, memberAddr, member); err != nil {
		return false, err
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
		sdk.NewEvent(
			"invitation_credits_reset",
			sdk.NewAttribute("member", memberAddr),
			sdk.NewAttribute("season", fmt.Sprintf("%d", currentSeason)),
			sdk.NewAttribute("credits", fmt.Sprintf("%d", maxCredits)),
		),
	)

	return true, nil
}

// GrantReputationCapped adds reputation to a member for a given tag, enforcing
// the per-epoch per-tag cap (MaxReputationGainPerEpoch). Returns the amount
// actually granted (which may be less than requested if the cap is hit).
// Modifies member in-place; caller must save.
func (k Keeper) GrantReputationCapped(ctx context.Context, member *types.Member, tag string, amount math.LegacyDec) (math.LegacyDec, error) {
	if amount.IsNegative() || amount.IsZero() {
		return math.LegacyZeroDec(), nil
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return math.LegacyZeroDec(), err
	}

	currentEpoch, err := k.GetCurrentEpoch(ctx)
	if err != nil {
		return math.LegacyZeroDec(), err
	}

	// Reset per-epoch counters if epoch has changed
	if member.LastRepGainEpoch != currentEpoch {
		member.ReputationGainedThisEpoch = make(map[string]string)
		member.LastRepGainEpoch = currentEpoch
	}

	// Initialize maps if nil
	if member.ReputationGainedThisEpoch == nil {
		member.ReputationGainedThisEpoch = make(map[string]string)
	}
	if member.ReputationScores == nil {
		member.ReputationScores = make(map[string]string)
	}

	// Check how much has already been gained this epoch for this tag
	gainedSoFar := math.LegacyZeroDec()
	if gainedStr, ok := member.ReputationGainedThisEpoch[tag]; ok {
		gainedSoFar, _ = math.LegacyNewDecFromStr(gainedStr)
	}

	// Calculate remaining headroom (zero if cap is zero = unlimited)
	maxGain := params.MaxReputationGainPerEpoch
	effectiveGrant := amount
	if maxGain.IsPositive() {
		remaining := maxGain.Sub(gainedSoFar)
		if remaining.IsNegative() || remaining.IsZero() {
			return math.LegacyZeroDec(), nil // Cap already reached
		}
		if effectiveGrant.GT(remaining) {
			effectiveGrant = remaining
		}
	}

	// Get current reputation for this tag
	currentRep := math.LegacyZeroDec()
	if repStr, ok := member.ReputationScores[tag]; ok {
		currentRep, _ = math.LegacyNewDecFromStr(repStr)
	}

	// Apply the grant
	newRep := currentRep.Add(effectiveGrant)
	member.ReputationScores[tag] = newRep.String()

	// Update the epoch tracking
	newGained := gainedSoFar.Add(effectiveGrant)
	member.ReputationGainedThisEpoch[tag] = newGained.String()

	return effectiveGrant, nil
}

// ApplyReputationDecay lazily decays all reputation scores based on epochs elapsed
// since the last decay. Uses the same LastDecayEpoch field as DREAM decay.
// Modifies member in-place; caller must save if needed.
func (k Keeper) ApplyReputationDecay(ctx context.Context, member *types.Member) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	if params.ReputationDecayRate.IsZero() {
		return nil // No decay configured
	}

	currentEpoch, err := k.GetCurrentEpoch(ctx)
	if err != nil {
		return err
	}

	elapsed := currentEpoch - member.LastDecayEpoch
	if elapsed <= 0 {
		return nil // Already up to date
	}

	// Calculate compound decay multiplier: (1 - rate)^elapsed
	multiplier := math.LegacyOneDec().Sub(params.ReputationDecayRate).Power(uint64(elapsed))

	if member.ReputationScores == nil {
		return nil
	}

	for tag, repStr := range member.ReputationScores {
		rep, err := math.LegacyNewDecFromStr(repStr)
		if err != nil {
			continue
		}
		if rep.IsZero() {
			continue
		}
		newRep := rep.Mul(multiplier)
		member.ReputationScores[tag] = newRep.String()
	}

	// Note: we do NOT update LastDecayEpoch here. That field is managed by
	// ApplyPendingDecay (DREAM decay). Both decays share the same epoch counter
	// via GetMember(), which calls ApplyReputationDecay first, then ApplyPendingDecay.
	// ApplyPendingDecay updates LastDecayEpoch for both.
	return nil
}

// GetInterimReputationTag returns the reputation tag for an interim type
func GetInterimReputationTag(interimType types.InterimType) string {
	switch interimType {
	case types.InterimType_INTERIM_TYPE_JURY_DUTY:
		return "jury-duty"
	case types.InterimType_INTERIM_TYPE_EXPERT_TESTIMONY:
		return "expert-testimony"
	case types.InterimType_INTERIM_TYPE_DISPUTE_MEDIATION:
		return "dispute-mediation"
	case types.InterimType_INTERIM_TYPE_PROJECT_APPROVAL:
		return "project-approval"
	case types.InterimType_INTERIM_TYPE_BUDGET_REVIEW:
		return "budget-review"
	case types.InterimType_INTERIM_TYPE_CONTRIBUTION_REVIEW:
		return "contribution-review"
	case types.InterimType_INTERIM_TYPE_EXCEPTION_REQUEST:
		return "exception-request"
	case types.InterimType_INTERIM_TYPE_TRANCHE_VERIFICATION:
		return "tranche-verification"
	case types.InterimType_INTERIM_TYPE_AUDIT:
		return "audit"
	case types.InterimType_INTERIM_TYPE_MODERATION:
		return "moderation"
	case types.InterimType_INTERIM_TYPE_MENTORSHIP:
		return "mentorship"
	case types.InterimType_INTERIM_TYPE_ADJUDICATION:
		return "adjudication"
	default:
		return "interim-work"
	}
}

// GetInterimReputationGrant calculates the reputation grant for an interim based on complexity
// Returns the reputation amount as a LegacyDec (base reputation scaled by complexity)
func GetInterimReputationGrant(complexity types.InterimComplexity) math.LegacyDec {
	// Base reputation grants scale with complexity
	// These are in "whole" reputation units (will be stored as Dec strings)
	switch complexity {
	case types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE:
		return math.LegacyNewDec(5) // 5 reputation for simple work
	case types.InterimComplexity_INTERIM_COMPLEXITY_STANDARD:
		return math.LegacyNewDec(10) // 10 reputation for standard work
	case types.InterimComplexity_INTERIM_COMPLEXITY_COMPLEX:
		return math.LegacyNewDec(20) // 20 reputation for complex work
	case types.InterimComplexity_INTERIM_COMPLEXITY_EXPERT:
		return math.LegacyNewDec(40) // 40 reputation for expert work
	case types.InterimComplexity_INTERIM_COMPLEXITY_EPIC:
		return math.LegacyNewDec(100) // 100 reputation for epic work
	default:
		return math.LegacyNewDec(5)
	}
}

// GrantInterimReputation grants reputation to a member for completing an interim.
// Subject to per-epoch per-tag reputation cap to prevent interim grinding.
func (k Keeper) GrantInterimReputation(ctx context.Context, memberAddr sdk.AccAddress, interim types.Interim) error {
	member, err := k.Member.Get(ctx, memberAddr.String())
	if err != nil {
		return types.ErrMemberNotFound
	}

	// Get reputation tag based on interim type
	tag := GetInterimReputationTag(interim.Type)

	// Get reputation grant based on complexity
	repGrant := GetInterimReputationGrant(interim.Complexity)

	// Apply the grant with per-epoch cap enforcement
	actualGrant, err := k.GrantReputationCapped(ctx, &member, tag, repGrant)
	if err != nil {
		return err
	}

	// Save member
	if err := k.Member.Set(ctx, memberAddr.String(), member); err != nil {
		return err
	}

	// Get new total for event
	newTotal := math.LegacyZeroDec()
	if repStr, ok := member.ReputationScores[tag]; ok {
		newTotal, _ = math.LegacyNewDecFromStr(repStr)
	}

	// Emit event
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
		sdk.NewEvent(
			"reputation_granted",
			sdk.NewAttribute("member", memberAddr.String()),
			sdk.NewAttribute("tag", tag),
			sdk.NewAttribute("amount", actualGrant.String()),
			sdk.NewAttribute("new_total", newTotal.String()),
			sdk.NewAttribute("source", "interim"),
			sdk.NewAttribute("interim_id", fmt.Sprintf("%d", interim.Id)),
		),
	)

	return nil
}
