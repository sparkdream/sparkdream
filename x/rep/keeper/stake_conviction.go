package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// CalculateStakingReward calculates time-based staking rewards using APY
// Formula: Stake × APY × (Duration / Year)
// Duration is in seconds, Year = 365.25 days = 31,557,600 seconds
func (k Keeper) CalculateStakingReward(ctx context.Context, stake types.Stake) (math.Int, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return math.ZeroInt(), err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentTime := sdkCtx.BlockTime().Unix()

	// Use last claimed time if available, otherwise use created time
	startTime := stake.LastClaimedAt
	if startTime == 0 {
		startTime = stake.CreatedAt
	}

	// Calculate duration in seconds
	duration := currentTime - startTime
	if duration <= 0 {
		return math.ZeroInt(), nil
	}

	// Year in seconds (365.25 days to account for leap years)
	const secondsPerYear = int64(365.25 * 24 * 60 * 60) // 31,557,600 seconds

	// Calculate reward: Stake × APY × (Duration / Year)
	// stake.Amount is now non-pointer
	reward := math.LegacyNewDecFromInt(stake.Amount).
		Mul(params.StakingApy).
		Mul(math.LegacyNewDec(duration)).
		Quo(math.LegacyNewDec(secondsPerYear)).
		TruncateInt()

	return reward, nil
}

// CalculateRawStakeConviction calculates the pre-sqrt (raw) time-weighted conviction
// for a single stake. This is used by UpdateInitiativeConvictionLazy to aggregate
// raw conviction per staker before applying sqrt dampening — preventing the stake
// splitting exploit where N small stakes yield sqrt(N) times more conviction than
// one large stake.
func (k Keeper) CalculateRawStakeConviction(ctx context.Context, stake types.Stake, initiativeTags []string) (math.LegacyDec, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return math.LegacyZeroDec(), err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Calculate time elapsed in seconds
	timeElapsed := sdkCtx.BlockTime().Unix() - stake.CreatedAt
	if timeElapsed < 0 {
		timeElapsed = 0
	}

	// Calculate half life in seconds (approx 6s per block)
	halfLifeSeconds := int64(params.ConvictionHalfLifeEpochs * params.EpochBlocks * 6)
	if halfLifeSeconds == 0 {
		halfLifeSeconds = 1 // avoid div by zero
	}

	// Calculate conviction with exponential decay (time-weighted)
	// conviction = amount * (1 - e^(-t/half_life))
	// Simplified: conviction = amount * min(1, t / (2 * half_life))

	timeFactor := math.LegacyNewDec(timeElapsed).Quo(math.LegacyNewDec(halfLifeSeconds).MulInt64(2))

	// Cap at 1.0
	if timeFactor.GT(math.LegacyOneDec()) {
		timeFactor = math.LegacyOneDec()
	}

	// Get staker's reputation for weighting
	stakerAddr, err := sdk.AccAddressFromBech32(stake.Staker)
	if err != nil {
		return math.LegacyZeroDec(), err
	}

	// Calculate tag-weighted reputation (only reputation in initiative's tags counts)
	// If initiative has no tags, no reputation bonus applies - this prevents gaming
	// by using unrelated reputation to boost conviction on untagged initiatives
	var avgRep math.LegacyDec
	if len(initiativeTags) > 0 {
		avgRep, err = k.GetReputationForTags(ctx, stakerAddr, initiativeTags)
		if err != nil {
			return math.LegacyZeroDec(), err
		}
	} else {
		// No tags = no reputation bonus (multiplier stays at 1.0)
		avgRep = math.LegacyZeroDec()
	}

	// Reputation multiplier: 1.0 + (rep / 1000)
	repMultiplier := math.LegacyOneDec().Add(avgRep.QuoInt64(1000))

	// Calculate raw weighted conviction (no sqrt dampening here)
	baseConviction := math.LegacyNewDecFromInt(stake.Amount).Mul(timeFactor)
	weightedConviction := baseConviction.Mul(repMultiplier)

	return weightedConviction, nil
}

// CalculateStakeConviction calculates time-weighted conviction for a single stake
// with sqrt dampening applied. Used for external queries and display purposes.
// For aggregation in UpdateInitiativeConvictionLazy, use CalculateRawStakeConviction
// to avoid the stake splitting exploit.
func (k Keeper) CalculateStakeConviction(ctx context.Context, stake types.Stake, initiativeTags []string) (math.LegacyDec, error) {
	raw, err := k.CalculateRawStakeConviction(ctx, stake, initiativeTags)
	if err != nil {
		return math.LegacyZeroDec(), err
	}

	// Apply quadratic dampening for large stakes to prevent whale dominance
	// conviction = sqrt(weighted_conviction)
	dampenedConviction, err := raw.ApproxSqrt()
	if err != nil {
		return math.LegacyZeroDec(), fmt.Errorf("failed to apply quadratic dampening: %w", err)
	}

	return dampenedConviction, nil
}

// UpdateInitiativeConvictionLazy updates an initiative's conviction using lazy evaluation
// This is called when stakes are added/removed or when conviction is queried
func (k Keeper) UpdateInitiativeConvictionLazy(ctx context.Context, initiativeID uint64) error {
	// Get initiative
	initiative, err := k.GetInitiative(ctx, initiativeID)
	if err != nil {
		return err
	}

	// Get all stakes for this initiative
	stakes, err := k.GetInitiativeStakes(ctx, initiativeID)
	if err != nil {
		return err
	}

	// Get project to check affiliation
	project, err := k.GetProject(ctx, initiative.ProjectId)
	if err != nil {
		return err
	}

	// Get params for per-member conviction cap
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	assigneeAddr := initiative.Assignee
	creatorAddr := project.Creator

	// Track per-staker RAW conviction (pre-sqrt) for correct aggregation.
	// Using raw values prevents the stake splitting exploit: N small stakes
	// would otherwise yield sqrt(N)x more conviction than one large stake.
	// By aggregating raw values first and applying sqrt to the aggregate,
	// splitting provides zero advantage.
	stakerRawConviction := make(map[string]math.LegacyDec) // staker -> total raw conviction
	stakerIsExternal := make(map[string]bool)               // staker -> external flag

	for _, stake := range stakes {
		// Calculate RAW (pre-sqrt) conviction for correct per-staker aggregation
		rawConviction, err := k.CalculateRawStakeConviction(ctx, stake, initiative.Tags)
		if err != nil {
			continue
		}

		prev, exists := stakerRawConviction[stake.Staker]
		if !exists {
			prev = math.LegacyZeroDec()
		}
		stakerRawConviction[stake.Staker] = prev.Add(rawConviction)

		// Check if stake is external (non-affiliated)
		if k.IsStakerExternal(stake.Staker, assigneeAddr, creatorAddr) {
			stakerIsExternal[stake.Staker] = true
		}
	}

	// Apply sqrt dampening to each staker's AGGREGATE raw conviction, then cap.
	// This ensures splitting stakes gives zero advantage (sqrt is applied once
	// to the total, not per-stake).
	maxPerMember := DerefDec(initiative.RequiredConviction).Mul(params.MaxConvictionSharePerMember)

	totalConviction := math.LegacyZeroDec()
	externalConviction := math.LegacyZeroDec()

	for staker, rawConviction := range stakerRawConviction {
		// Apply sqrt dampening to staker's aggregate
		dampened, err := rawConviction.ApproxSqrt()
		if err != nil {
			continue
		}

		// Apply per-member cap
		capped := dampened
		if capped.GT(maxPerMember) {
			capped = maxPerMember
		}
		totalConviction = totalConviction.Add(capped)
		if stakerIsExternal[staker] {
			externalConviction = externalConviction.Add(capped)
		}
	}

	// Calculate conviction propagated from linked content (external stakers only)
	propagatedConviction, err := k.GetPropagatedConviction(ctx, initiativeID, assigneeAddr, creatorAddr)
	if err != nil {
		// Log but don't fail — propagation is a bonus, not critical
		propagatedConviction = math.LegacyZeroDec()
	}

	// Propagated conviction counts as external (content stakers are independent community members)
	totalConviction = totalConviction.Add(propagatedConviction)
	externalConviction = externalConviction.Add(propagatedConviction)

	// Update initiative
	initiative.PropagatedConviction = PtrDec(propagatedConviction)
	initiative.CurrentConviction = PtrDec(totalConviction)
	initiative.ExternalConviction = PtrDec(externalConviction)
	initiative.ConvictionLastUpdated = sdk.UnwrapSDKContext(ctx).BlockHeight()

	return k.UpdateInitiative(ctx, initiative)
}

// CalculateContentConviction calculates time-weighted conviction for a content stake.
// Uses ContentConvictionHalfLifeEpochs (slower decay than initiatives).
// No reputation weighting or quadratic dampening — simpler model for content signal.
func (k Keeper) CalculateContentConviction(ctx context.Context, stake types.Stake) (math.LegacyDec, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return math.LegacyZeroDec(), err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	timeElapsed := sdkCtx.BlockTime().Unix() - stake.CreatedAt
	if timeElapsed < 0 {
		timeElapsed = 0
	}

	// Use content-specific half-life (default 14 epochs vs 7 for initiatives)
	halfLifeSeconds := int64(params.ContentConvictionHalfLifeEpochs * params.EpochBlocks * 6)
	if halfLifeSeconds == 0 {
		halfLifeSeconds = 1
	}

	// Linear approximation: conviction = amount * min(1, t / (2 * half_life))
	timeFactor := math.LegacyNewDec(timeElapsed).Quo(math.LegacyNewDec(halfLifeSeconds).MulInt64(2))
	if timeFactor.GT(math.LegacyOneDec()) {
		timeFactor = math.LegacyOneDec()
	}

	conviction := math.LegacyNewDecFromInt(stake.Amount).Mul(timeFactor)
	return conviction, nil
}

// GetContentConviction returns the total conviction score for a content item.
// Sums CalculateContentConviction across all community conviction stakes on the target.
func (k Keeper) GetContentConviction(ctx context.Context, targetType types.StakeTargetType, targetID uint64) (math.LegacyDec, error) {
	if !types.IsContentConvictionType(targetType) {
		return math.LegacyZeroDec(), types.ErrNotContentTargetType
	}

	stakes, err := k.GetStakesByTarget(ctx, targetType, targetID)
	if err != nil {
		return math.LegacyZeroDec(), err
	}

	totalConviction := math.LegacyZeroDec()
	for _, stake := range stakes {
		conviction, err := k.CalculateContentConviction(ctx, stake)
		if err != nil {
			continue
		}
		totalConviction = totalConviction.Add(conviction)
	}

	return totalConviction, nil
}

// GetExternalContentConviction returns the total conviction score for a content item,
// counting only stakes from members who are not affiliated with a linked initiative.
// This prevents sybil networks from bypassing the external conviction requirement
// by routing conviction through the content layer.
func (k Keeper) GetExternalContentConviction(ctx context.Context, targetType types.StakeTargetType, targetID uint64, assigneeAddr, creatorAddr string) (math.LegacyDec, error) {
	if !types.IsContentConvictionType(targetType) {
		return math.LegacyZeroDec(), types.ErrNotContentTargetType
	}

	stakes, err := k.GetStakesByTarget(ctx, targetType, targetID)
	if err != nil {
		return math.LegacyZeroDec(), err
	}

	totalConviction := math.LegacyZeroDec()
	for _, stake := range stakes {
		// Only count stakes from external (non-affiliated) members
		if !k.IsStakerExternal(stake.Staker, assigneeAddr, creatorAddr) {
			continue
		}
		conviction, err := k.CalculateContentConviction(ctx, stake)
		if err != nil {
			continue
		}
		totalConviction = totalConviction.Add(conviction)
	}

	return totalConviction, nil
}

// GetContentStakes returns all community conviction stakes for a content item.
func (k Keeper) GetContentStakes(ctx context.Context, targetType types.StakeTargetType, targetID uint64) ([]types.Stake, error) {
	if !types.IsContentConvictionType(targetType) {
		return nil, types.ErrNotContentTargetType
	}
	return k.GetStakesByTarget(ctx, targetType, targetID)
}

// IsStakerExternal checks if a staker is external (non-affiliated) to an initiative
func (k Keeper) IsStakerExternal(stakerAddr, assigneeAddr, creatorAddr string) bool {
	// Staker is external if they are not the assignee or creator
	return stakerAddr != assigneeAddr && stakerAddr != creatorAddr
}

// CanCompleteInitiative checks if an initiative has met completion requirements
func (k Keeper) CanCompleteInitiative(ctx context.Context, initiativeID uint64) (bool, error) {
	// Get initiative (this will have updated conviction from lazy evaluation)
	initiative, err := k.GetInitiative(ctx, initiativeID)
	if err != nil {
		return false, err
	}

	// Must be in SUBMITTED or IN_REVIEW status
	// SUBMITTED: Can transition to challenge period
	// IN_REVIEW: Can complete after challenge period ends
	if initiative.Status != types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED &&
		initiative.Status != types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW {
		return false, nil
	}

	// Get params
	params, err := k.Params.Get(ctx)
	if err != nil {
		return false, err
	}

	// Check conviction threshold
	if DerefDec(initiative.CurrentConviction).LT(DerefDec(initiative.RequiredConviction)) {
		return false, nil
	}

	// Check external conviction ratio (must be at least 50%)
	minExternalConviction := DerefDec(initiative.RequiredConviction).Mul(params.ExternalConvictionRatio)
	if DerefDec(initiative.ExternalConviction).LT(minExternalConviction) {
		return false, nil
	}

	// Check for active challenges - initiative cannot complete if there are unresolved challenges
	hasActiveChallenge, err := k.HasActiveChallenges(ctx, initiativeID)
	if err != nil {
		return false, err
	}
	if hasActiveChallenge {
		return false, nil
	}

	return true, nil
}
