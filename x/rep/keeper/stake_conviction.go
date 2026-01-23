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

// CalculateStakeConviction calculates time-weighted conviction for a single stake.
// The initiativeTags parameter specifies which reputation tags are relevant for weighting.
// If initiativeTags is nil or empty, no reputation bonus applies (multiplier = 1.0).
// This prevents gaming by using unrelated reputation on untagged initiatives.
func (k Keeper) CalculateStakeConviction(ctx context.Context, stake types.Stake, initiativeTags []string) (math.LegacyDec, error) {
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

	// Calculate conviction
	baseConviction := math.LegacyNewDecFromInt(stake.Amount).Mul(timeFactor)
	weightedConviction := baseConviction.Mul(repMultiplier)

	// Apply quadratic dampening for large stakes to prevent whale dominance
	// conviction = sqrt(weighted_conviction)
	// This creates diminishing returns: 100 DREAM = 10 conviction, 10000 DREAM = 100 conviction
	dampenedConviction, err := weightedConviction.ApproxSqrt()
	if err != nil {
		// If sqrt fails (e.g., negative number which shouldn't happen), return zero
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

	// Calculate total conviction
	totalConviction := math.LegacyZeroDec()
	externalConviction := math.LegacyZeroDec()

	assigneeAddr := initiative.Assignee
	creatorAddr := project.Creator

	for _, stake := range stakes {
		// Calculate conviction for this stake using initiative's tags for reputation weighting
		conviction, err := k.CalculateStakeConviction(ctx, stake, initiative.Tags)
		if err != nil {
			continue
		}

		totalConviction = totalConviction.Add(conviction)

		// Check if stake is external (non-affiliated)
		if k.IsStakerExternal(stake.Staker, assigneeAddr, creatorAddr) {
			externalConviction = externalConviction.Add(conviction)
		}
	}

	// Update initiative
	initiative.CurrentConviction = PtrDec(totalConviction)
	initiative.ExternalConviction = PtrDec(externalConviction)
	initiative.ConvictionLastUpdated = sdk.UnwrapSDKContext(ctx).BlockHeight()

	return k.UpdateInitiative(ctx, initiative)
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
