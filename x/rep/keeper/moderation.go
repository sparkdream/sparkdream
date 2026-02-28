package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ZeroMember zeros out a member's DREAM balance, reputation, and resets their status.
// This is the harshest penalty - the member can restart with a new address and invitation.
// Per spec: "Punish position, not person"
func (k Keeper) ZeroMember(ctx context.Context, memberAddr sdk.AccAddress, reason string) error {
	member, err := k.Member.Get(ctx, memberAddr.String())
	if err != nil {
		return types.ErrMemberNotFound
	}

	// Check if already zeroed
	if member.Status == types.MemberStatus_MEMBER_STATUS_ZEROED {
		return types.ErrMemberAlreadyZeroed
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// Track amounts before zeroing for event
	dreamBurned := member.DreamBalance.Add(*member.StakedDream)

	// Burn all DREAM (both available and staked)
	if member.DreamBalance.IsPositive() {
		*member.LifetimeBurned = member.LifetimeBurned.Add(*member.DreamBalance)
		*member.DreamBalance = math.NewInt(0)
	}
	if member.StakedDream.IsPositive() {
		*member.LifetimeBurned = member.LifetimeBurned.Add(*member.StakedDream)
		*member.StakedDream = math.NewInt(0)
	}

	// Archive current reputation to lifetime before zeroing
	if member.LifetimeReputation == nil {
		member.LifetimeReputation = make(map[string]string)
	}
	for tag, score := range member.ReputationScores {
		// Add to lifetime archive
		existingLifetime := math.LegacyZeroDec()
		if existingStr, ok := member.LifetimeReputation[tag]; ok {
			existingLifetime, _ = math.LegacyNewDecFromStr(existingStr)
		}
		currentScore, _ := math.LegacyNewDecFromStr(score)
		member.LifetimeReputation[tag] = existingLifetime.Add(currentScore).String()
	}

	// Zero all current season reputation
	for tag := range member.ReputationScores {
		member.ReputationScores[tag] = "0"
	}

	// Reset status and metadata
	member.Status = types.MemberStatus_MEMBER_STATUS_ZEROED
	member.ZeroedAt = now
	member.ZeroedCount++
	member.TrustLevel = types.TrustLevel_TRUST_LEVEL_NEW
	member.InvitationCredits = 0
	member.TipsGivenThisEpoch = 0
	if member.GiftsSentThisEpoch != nil {
		*member.GiftsSentThisEpoch = math.NewInt(0)
	}

	// Save member
	if err := k.Member.Set(ctx, memberAddr.String(), member); err != nil {
		return err
	}

	// Mark trust tree dirty — zeroed member must be removed from anonymous posting tree
	k.MarkMemberDirty(ctx, memberAddr.String())

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"member_zeroed",
			sdk.NewAttribute("member", memberAddr.String()),
			sdk.NewAttribute("reason", reason),
			sdk.NewAttribute("dream_burned", dreamBurned.String()),
			sdk.NewAttribute("zeroed_count", fmt.Sprintf("%d", member.ZeroedCount)),
		),
	)

	return nil
}

// SlashReputation reduces a member's reputation by a percentage across all or specified tags.
// This is a medium-level penalty that doesn't affect DREAM balance or member status.
// penaltyRate should be between 0 and 1 (e.g., 0.3 for 30% slash)
func (k Keeper) SlashReputation(ctx context.Context, memberAddr sdk.AccAddress, penaltyRate math.LegacyDec, tags []string, reason string) error {
	member, err := k.Member.Get(ctx, memberAddr.String())
	if err != nil {
		return types.ErrMemberNotFound
	}

	if member.Status != types.MemberStatus_MEMBER_STATUS_ACTIVE {
		return types.ErrMemberNotActive
	}

	if penaltyRate.IsNegative() || penaltyRate.GT(math.LegacyOneDec()) {
		return types.ErrInvalidAmount
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Determine which tags to slash
	tagsToSlash := tags
	if len(tagsToSlash) == 0 {
		// Slash all tags
		tagsToSlash = make([]string, 0, len(member.ReputationScores))
		for tag := range member.ReputationScores {
			tagsToSlash = append(tagsToSlash, tag)
		}
	}

	// Calculate retention factor (1 - penaltyRate)
	retentionFactor := math.LegacyOneDec().Sub(penaltyRate)

	totalSlashed := math.LegacyZeroDec()

	// Apply slash to each tag
	for _, tag := range tagsToSlash {
		if repStr, ok := member.ReputationScores[tag]; ok {
			currentRep, err := math.LegacyNewDecFromStr(repStr)
			if err != nil {
				continue
			}

			newRep := currentRep.Mul(retentionFactor)
			slashedAmount := currentRep.Sub(newRep)
			totalSlashed = totalSlashed.Add(slashedAmount)

			member.ReputationScores[tag] = newRep.String()
		}
	}

	// Save member
	if err := k.Member.Set(ctx, memberAddr.String(), member); err != nil {
		return err
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"reputation_slashed",
			sdk.NewAttribute("member", memberAddr.String()),
			sdk.NewAttribute("penalty_rate", penaltyRate.String()),
			sdk.NewAttribute("total_slashed", totalSlashed.String()),
			sdk.NewAttribute("reason", reason),
		),
	)

	return nil
}

// AddReputation increases a member's reputation for a specific tag by an absolute amount.
// Used by other modules (e.g. x/reveal) to reward contributions.
func (k Keeper) AddReputation(ctx context.Context, memberAddr sdk.AccAddress, tag string, amount math.LegacyDec) error {
	member, err := k.Member.Get(ctx, memberAddr.String())
	if err != nil {
		return types.ErrMemberNotFound
	}

	if member.Status != types.MemberStatus_MEMBER_STATUS_ACTIVE {
		return types.ErrMemberNotActive
	}

	if amount.IsNegative() {
		return types.ErrInvalidAmount
	}

	if member.ReputationScores == nil {
		member.ReputationScores = make(map[string]string)
	}

	currentRep := math.LegacyZeroDec()
	if repStr, ok := member.ReputationScores[tag]; ok {
		currentRep, _ = math.LegacyNewDecFromStr(repStr)
	}

	newRep := currentRep.Add(amount)
	member.ReputationScores[tag] = newRep.String()

	if err := k.Member.Set(ctx, memberAddr.String(), member); err != nil {
		return err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"reputation_added",
			sdk.NewAttribute("member", memberAddr.String()),
			sdk.NewAttribute("tag", tag),
			sdk.NewAttribute("amount", amount.String()),
			sdk.NewAttribute("new_score", newRep.String()),
		),
	)

	return nil
}

// DeductReputation decreases a member's reputation for a specific tag by an absolute amount.
// The score is floored at zero. Used by other modules (e.g. x/reveal) to penalize failures.
func (k Keeper) DeductReputation(ctx context.Context, memberAddr sdk.AccAddress, tag string, amount math.LegacyDec) error {
	member, err := k.Member.Get(ctx, memberAddr.String())
	if err != nil {
		return types.ErrMemberNotFound
	}

	if member.Status != types.MemberStatus_MEMBER_STATUS_ACTIVE {
		return types.ErrMemberNotActive
	}

	if amount.IsNegative() {
		return types.ErrInvalidAmount
	}

	if member.ReputationScores == nil {
		member.ReputationScores = make(map[string]string)
	}

	currentRep := math.LegacyZeroDec()
	if repStr, ok := member.ReputationScores[tag]; ok {
		currentRep, _ = math.LegacyNewDecFromStr(repStr)
	}

	newRep := currentRep.Sub(amount)
	if newRep.IsNegative() {
		newRep = math.LegacyZeroDec()
	}
	member.ReputationScores[tag] = newRep.String()

	if err := k.Member.Set(ctx, memberAddr.String(), member); err != nil {
		return err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"reputation_deducted",
			sdk.NewAttribute("member", memberAddr.String()),
			sdk.NewAttribute("tag", tag),
			sdk.NewAttribute("amount", amount.String()),
			sdk.NewAttribute("new_score", newRep.String()),
		),
	)

	return nil
}

// DemoteMember applies a reputation slash as a demotion penalty.
// Per the spec, trust levels never decrease, so "demotion" is actually a reputation slash.
// The member keeps their trust level but loses reputation, making it harder to participate in tier-gated activities.
func (k Keeper) DemoteMember(ctx context.Context, memberAddr sdk.AccAddress, reason string) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	// Use severe slash penalty (30% by default) for demotion
	penaltyRate := params.SevereSlashPenalty
	if penaltyRate.IsZero() {
		penaltyRate = math.LegacyMustNewDecFromStr("0.3") // Default 30%
	}

	// Slash all reputation tags
	return k.SlashReputation(ctx, memberAddr, penaltyRate, nil, reason)
}

// IsMember checks if an address is a registered member (not necessarily active).
func (k Keeper) IsMember(ctx context.Context, addr sdk.AccAddress) bool {
	_, err := k.Member.Get(ctx, addr.String())
	return err == nil
}

// IsActiveMember checks if an address is an active member (not zeroed or inactive).
func (k Keeper) IsActiveMember(ctx context.Context, addr sdk.AccAddress) bool {
	member, err := k.Member.Get(ctx, addr.String())
	if err != nil {
		return false
	}
	return member.Status == types.MemberStatus_MEMBER_STATUS_ACTIVE
}

// GetTrustLevel returns the trust level for a member.
func (k Keeper) GetTrustLevel(ctx context.Context, addr sdk.AccAddress) (types.TrustLevel, error) {
	member, err := k.Member.Get(ctx, addr.String())
	if err != nil {
		return types.TrustLevel_TRUST_LEVEL_NEW, types.ErrMemberNotFound
	}
	return member.TrustLevel, nil
}

// GetReputationTier returns a tier (0-5) based on total reputation across tags.
// This is used by other modules for reputation-gated access control.
func (k Keeper) GetReputationTier(ctx context.Context, addr sdk.AccAddress) (uint64, error) {
	member, err := k.Member.Get(ctx, addr.String())
	if err != nil {
		return 0, types.ErrMemberNotFound
	}

	// Calculate total reputation
	totalRep := math.LegacyZeroDec()
	for _, repStr := range member.ReputationScores {
		rep, err := math.LegacyNewDecFromStr(repStr)
		if err != nil {
			continue
		}
		totalRep = totalRep.Add(rep)
	}

	// Map to tiers (0-5) based on total reputation
	// Tier 0: < 10 rep
	// Tier 1: 10-49 rep
	// Tier 2: 50-199 rep
	// Tier 3: 200-499 rep
	// Tier 4: 500-999 rep
	// Tier 5: 1000+ rep
	tier := uint64(0)
	repInt := totalRep.TruncateInt64()

	switch {
	case repInt >= 1000:
		tier = 5
	case repInt >= 500:
		tier = 4
	case repInt >= 200:
		tier = 3
	case repInt >= 50:
		tier = 2
	case repInt >= 10:
		tier = 1
	default:
		tier = 0
	}

	return tier, nil
}
