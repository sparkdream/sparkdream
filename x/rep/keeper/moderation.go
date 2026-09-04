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

	// The member's whole DREAM holding. staked_dream is a SUBSET of
	// dream_balance — LockDREAM adds to the former without reducing the latter
	// — so the old `dream_balance + staked_dream` double-counted every staked
	// coin in both lifetime_burned and the season burn counter.
	dreamBurned := *member.DreamBalance

	// Release the member's live stake positions before zeroing the aggregates.
	// Zeroing used to leave them behind: decayStakes went on shrinking records
	// backed by a now-zero aggregate (driving staked_dream and dream_balance
	// negative), and settleStake went on minting rewards to the zeroed member,
	// who kept unbacked earning positions indefinitely.
	if err := k.releaseStakesForZeroing(ctx, memberAddr.String()); err != nil {
		return err
	}

	// Burn all DREAM. Zeroing writes the member record directly rather than
	// routing through BurnDREAM, so it counts its own burn against the season.
	if dreamBurned.IsPositive() {
		*member.LifetimeBurned = member.LifetimeBurned.Add(dreamBurned)
		if err := k.TrackBurn(ctx, dreamBurned); err != nil {
			return err
		}
	}
	*member.DreamBalance = math.NewInt(0)
	*member.StakedDream = math.NewInt(0)
	member.ContentStakedDream = PtrInt(math.ZeroInt())

	// Zeroing is the severest penalty the module has, and it is exactly the
	// event the invitation stake exists to price: whoever vouched for this
	// member is accountable for them for invitation_accountability_epochs.
	// ProcessInviterAccountability had no production caller at all, so the
	// slash never fired and the only realized cost of a bad invitation was the
	// 10% acceptance burn — the sybil-cost model the spec's invitation
	// economics assume was not wired to anything.
	//
	// Non-fatal: the inviter may have left, the accountability window may have
	// closed, or there may be no invitation at all for a genesis-seeded member.
	// None of those should block the zeroing itself.
	if err := k.ProcessInviterAccountability(ctx, memberAddr, reason); err != nil {
		sdkCtx.Logger().Info("inviter accountability not applied on zeroing",
			"member", memberAddr.String(), "error", err)
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

	// Use severe slash penalty for demotion. Bounded to [0,1] by Params.Validate;
	// 0 legitimately means "no demotion slash".
	penaltyRate := params.SevereSlashPenalty

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

// releaseStakesForZeroing deletes every stake record held by a member being
// zeroed, shrinking each pool denominator by the amount leaving it.
//
// Zeroing burns the member's entire DREAM balance, staked portion included, so
// the stake records it backed have to go with it. Leaving them behind was the
// worst of both worlds: `decayStakes` kept shrinking records whose aggregate
// was already zero (driving staked_dream and dream_balance negative, which
// corrupts `unlocked = dream_balance - staked_dream` everywhere it is read),
// and `settleStake` kept minting rewards to the zeroed member, since no payout
// path checks member status.
//
// The principal is NOT returned — it was just burned. This only removes the
// records and the pool weight they carried, so the remaining stakers' shares
// are not diluted by a position that no longer has DREAM behind it.
//
// Collect-then-delete: rewriting the range being walked is the pattern the rest
// of the module is careful to avoid.
func (k Keeper) releaseStakesForZeroing(ctx context.Context, staker string) error {
	type doomed struct {
		id    uint64
		stake types.Stake
	}
	var stakes []doomed

	if err := k.Stake.Walk(ctx, nil, func(id uint64, stake types.Stake) (bool, error) {
		if stake.Staker == staker {
			stakes = append(stakes, doomed{id: id, stake: stake})
		}
		return false, nil
	}); err != nil {
		return fmt.Errorf("failed to walk stakes for zeroed member %s: %w", staker, err)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	for _, d := range stakes {
		if !d.stake.Amount.IsNil() && d.stake.Amount.IsPositive() {
			if err := k.updateStakePoolTotals(ctx, d.stake, d.stake.Amount.Neg()); err != nil {
				return fmt.Errorf("failed to shrink pool totals for stake %d: %w", d.id, err)
			}
		}
		if err := k.RemoveStakeFromTargetIndex(ctx, d.stake); err != nil {
			// Index drift is recoverable; a half-deleted stake is not.
			sdkCtx.Logger().Error("failed to remove zeroed stake from target index",
				"stake_id", d.id, "staker", staker, "error", err)
		}
		if err := k.Stake.Remove(ctx, d.id); err != nil {
			return fmt.Errorf("failed to remove stake %d for zeroed member: %w", d.id, err)
		}
	}

	if len(stakes) > 0 {
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				"zeroed_member_stakes_released",
				sdk.NewAttribute("member", staker),
				sdk.NewAttribute("stakes_removed", fmt.Sprintf("%d", len(stakes))),
			),
		)
	}
	return nil
}
