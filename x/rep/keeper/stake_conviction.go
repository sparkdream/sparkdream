package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// CalculateStakingReward calculates staking rewards from the seasonal pool.
// The global SeasonalPoolAccPerShare is advanced once per epoch in EndBlocker,
// and each stake's RewardDebt records the accumulator value it joined at.
//
// This reads the seasonal accumulator unconditionally and is therefore only
// meaningful for INITIATIVE and PROJECT stakes. Callers that handle stakes of
// arbitrary target type must use GetPendingStakingRewards, which dispatches to
// the accumulator that actually owns the stake.
func (k Keeper) CalculateStakingReward(ctx context.Context, stake types.Stake) (math.Int, error) {
	accPerShare, err := k.getSeasonalPoolAccPerShare(ctx)
	if err != nil {
		return math.ZeroInt(), nil // no pool initialized yet
	}
	return pendingAgainst(stake.Amount, stakeRewardDebt(stake), accPerShare), nil
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

	// Calculate half life in seconds (approx 6s per block).
	// ConvictionHalfLifeEpochs and EpochBlocks are both rejected at <= 0 by
	// Params.Validate, so this product is always positive. Defensive guard only:
	// if it is somehow non-positive, treat conviction as zero rather than
	// substituting a magic value or risking a div-by-zero.
	halfLifeSeconds := int64(params.ConvictionHalfLifeEpochs * params.EpochBlocks * 6)
	if halfLifeSeconds <= 0 {
		return math.LegacyZeroDec(), nil
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
	stakes, err := k.GetInitiativeStakes(ctx, initiativeID)
	if err != nil {
		return err
	}
	return k.updateInitiativeConvictionWithStakes(ctx, initiativeID, stakes)
}

// updateInitiativeConvictionWithStakes is the body of the lazy update, taking an
// already-fetched stake slice. The conviction queue drainer needs the stake
// count to charge its per-block work budget, and re-reading the index just to
// count would double the cost of the very sweep the budget exists to bound.
func (k Keeper) updateInitiativeConvictionWithStakes(ctx context.Context, initiativeID uint64, stakes []types.Stake) error {
	// Get initiative
	initiative, err := k.GetInitiative(ctx, initiativeID)
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

	// Everyone with an insider stake in the outcome, so that conviction from
	// the initiative's own author does not count toward the external floor,
	// together with the invitation edges reaching them. Hoisted out of the
	// stake loop below: resolving it costs a member lookup per affiliate, and
	// this runs once per initiative per conviction refresh.
	affiliates := k.InvitationNeighborhoodOf(ctx, InitiativeAffiliates(initiative, project)...)

	// Track per-staker RAW conviction (pre-sqrt) for correct aggregation.
	// Using raw values prevents the stake splitting exploit: N small stakes
	// would otherwise yield sqrt(N)x more conviction than one large stake.
	// By aggregating raw values first and applying sqrt to the aggregate,
	// splitting provides zero advantage.
	stakerRawConviction := make(map[string]math.LegacyDec) // staker -> total raw conviction
	stakerIsExternal := make(map[string]bool)              // staker -> external flag

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

		// Check if stake is external (non-affiliated). One staker can hold
		// several stakes and the test is per address, so resolve it once.
		if _, seen := stakerIsExternal[stake.Staker]; !seen {
			stakerIsExternal[stake.Staker] = k.IsStakerExternalTo(ctx, stake.Staker, affiliates)
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
	propagatedConviction, err := k.GetPropagatedConvictionIn(ctx, initiativeID, affiliates)
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
func (k Keeper) GetExternalContentConviction(ctx context.Context, targetType types.StakeTargetType, targetID uint64, affiliated ...string) (math.LegacyDec, error) {
	// Hoisted for the same reason as the initiative path: the neighborhood is
	// fixed for the target, the per-staker test is not.
	return k.GetExternalContentConvictionIn(ctx, targetType, targetID, k.InvitationNeighborhoodOf(ctx, affiliated...))
}

// GetExternalContentConvictionIn is GetExternalContentConviction against an
// already-resolved neighborhood.
func (k Keeper) GetExternalContentConvictionIn(ctx context.Context, targetType types.StakeTargetType, targetID uint64, neighborhood InvitationNeighborhood) (math.LegacyDec, error) {
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
		if !k.IsStakerExternalTo(ctx, stake.Staker, neighborhood) {
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

// InvitationNeighborhood is the one-hop invitation-graph neighborhood of a set
// of affiliates, precomputed so that the per-staker independence test stays a
// single member lookup no matter how many stakers there are.
//
// Membership on this chain comes from an invitation, and an invitation is a
// vouching relationship with a staked bond behind it. A puppet account backing
// the work of the member who invited it is therefore not an independent voice —
// it is the inviter's own conviction wearing a second address, which is exactly
// the shape of the cheapest available attack on the external-conviction floor:
// invite a handful of accounts, gift them DREAM (GiftOnlyToInvitees permits the
// inviter -> own invitee direction), and have them vouch for a self-assigned
// mint.
//
// Exactly one hop is excluded, in both directions. That is a deliberate choice
// over a full subtree walk: one hop costs O(1) per staker against the member
// record that already exists, whereas walking a subtree is unbounded per-block
// work that any member can inflate for the price of more invitations — the same
// class of problem the conviction queue exists to bound. Siblings (two accounts
// invited by the same third party) are two hops apart and still count as
// external.
type InvitationNeighborhood struct {
	// affiliates are the insiders themselves.
	affiliates map[string]struct{}
	// inviters are the addresses that invited an affiliate. A staker in this
	// set vouched for an insider once already, with a bond, before the
	// initiative existed.
	inviters map[string]struct{}
}

// InvitationNeighborhoodOf resolves the inviter of each affiliate once, so that
// callers iterating stakes can reuse the result. Costs at most one member
// lookup per affiliate — four, for an initiative.
func (k Keeper) InvitationNeighborhoodOf(ctx context.Context, affiliated ...string) InvitationNeighborhood {
	n := InvitationNeighborhood{
		affiliates: make(map[string]struct{}, len(affiliated)),
		inviters:   make(map[string]struct{}, len(affiliated)),
	}
	for _, addr := range affiliated {
		if addr == "" {
			continue
		}
		n.affiliates[addr] = struct{}{}
	}
	for addr := range n.affiliates {
		// Read the member record directly rather than through GetMember:
		// invited_by is immutable and this must not trigger the lazy-decay
		// write path from inside a read-only independence test.
		member, err := k.Member.Get(ctx, addr)
		if err != nil {
			// A non-member affiliate (or one whose record is gone) simply
			// contributes no invitation edge.
			continue
		}
		if member.InvitedBy != "" {
			n.inviters[member.InvitedBy] = struct{}{}
		}
	}
	return n
}

// IsStakerExternalTo reports whether a staker is at arm's length from a
// precomputed neighborhood: neither an affiliate, nor one invitation hop from
// one in either direction.
//
// Note what this still is not. It excludes known insiders and their immediate
// invitation edges, but nothing here resists a sybil ring assembled through an
// unrelated inviter, and nothing here weighs trust level — any member may stake,
// and membership comes from an invitation.
func (k Keeper) IsStakerExternalTo(ctx context.Context, stakerAddr string, n InvitationNeighborhood) bool {
	if stakerAddr == "" {
		return false
	}
	if _, ok := n.affiliates[stakerAddr]; ok {
		return false
	}
	// The staker invited an affiliate.
	if _, ok := n.inviters[stakerAddr]; ok {
		return false
	}
	// An affiliate invited the staker. Checked against the affiliates only, not
	// against n.inviters, or two accounts sharing an inviter would be excluded
	// as well — that is the two-hop sibling case this deliberately allows.
	member, err := k.Member.Get(ctx, stakerAddr)
	if err != nil {
		return true
	}
	if member.InvitedBy == "" {
		return true
	}
	_, invitedByAffiliate := n.affiliates[member.InvitedBy]
	return !invitedByAffiliate
}

// IsStakerExternal reports whether a staker is external (non-affiliated) to the
// thing they staked on. The caller supplies the affiliated addresses — for an
// initiative use InitiativeAffiliates, which includes the initiative's author;
// the content paths pass the assignee and creator of the linked initiative.
//
// Convenience wrapper for callers testing a single staker. Anything iterating a
// stake list should hoist InvitationNeighborhoodOf out of the loop and call
// IsStakerExternalTo instead.
func (k Keeper) IsStakerExternal(ctx context.Context, stakerAddr string, affiliated ...string) bool {
	return k.IsStakerExternalTo(ctx, stakerAddr, k.InvitationNeighborhoodOf(ctx, affiliated...))
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

	// Check external conviction ratio (must be at least 50%).
	// When the work is self-assigned the "internal" roles collapse into one
	// party, so the ENTIRE conviction threshold must be met by external
	// (non-affiliated) stakers — the community alone vouches for it.
	project, err := k.GetProject(ctx, initiative.ProjectId)
	if err != nil {
		return false, err
	}
	// A cancelled parent project can never complete its initiatives — this
	// keeps the EndBlocker from advancing a SUBMITTED initiative toward payout
	// under a dead project (silent; the cancel cascade closes them out).
	if project.Status == types.ProjectStatus_PROJECT_STATUS_CANCELLED {
		return false, nil
	}
	externalRatio := params.ExternalConvictionRatio
	if IsSelfAssigned(initiative, project, initiative.Assignee) {
		externalRatio = params.SelfAssignedExternalConvictionRatio
	}
	minExternalConviction := DerefDec(initiative.RequiredConviction).Mul(externalRatio)
	if DerefDec(initiative.ExternalConviction).LT(minExternalConviction) {
		return false, nil
	}

	// Reviewer sign-off, required when the parent project asks for it OR when
	// the completion would mint more than review_required_above_budget. An
	// *additional* brake: every conviction gate above and the challenge window
	// below still apply.
	reviewed, err := k.ReviewGateSatisfied(ctx, params, initiative, project)
	if err != nil {
		return false, err
	}
	if !reviewed {
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
