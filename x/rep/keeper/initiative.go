package keeper

import (
	"context"
	"fmt"
	stdmath "math"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// CreateInitiative creates a new initiative under a project
func (k Keeper) CreateInitiative(
	ctx context.Context,
	creator sdk.AccAddress,
	projectID uint64,
	title, description string,
	tags []string,
	tier types.InitiativeTier,
	category types.InitiativeCategory,
	templateID string,
	budget math.Int,
) (uint64, error) {
	// Validate project exists and is active
	project, err := k.GetProject(ctx, projectID)
	if err != nil {
		return 0, err
	}
	if project.Status != types.ProjectStatus_PROJECT_STATUS_ACTIVE {
		return 0, fmt.Errorf("project must be in ACTIVE status")
	}

	// Get params for tier validation
	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get params: %w", err)
	}

	// Validate budget is within tier limits
	var tierConfig types.TierConfig
	var tierName string
	switch tier {
	case types.InitiativeTier_INITIATIVE_TIER_APPRENTICE:
		tierConfig = params.ApprenticeTier
		tierName = "apprentice"
	case types.InitiativeTier_INITIATIVE_TIER_STANDARD:
		tierConfig = params.StandardTier
		tierName = "standard"
	case types.InitiativeTier_INITIATIVE_TIER_EXPERT:
		tierConfig = params.ExpertTier
		tierName = "expert"
	case types.InitiativeTier_INITIATIVE_TIER_EPIC:
		tierConfig = params.EpicTier
		tierName = "epic"
	default:
		return 0, fmt.Errorf("invalid initiative tier: %v", tier)
	}

	if budget.GT(tierConfig.MaxBudget) {
		// Convert micro-DREAM to DREAM for readable error (1 DREAM = 1,000,000 micro-DREAM)
		budgetDream := budget.Quo(math.NewInt(1000000))
		maxDream := tierConfig.MaxBudget.Quo(math.NewInt(1000000))
		return 0, fmt.Errorf("budget %s DREAM exceeds %s tier maximum of %s DREAM", budgetDream.String(), tierName, maxDream.String())
	}

	// Allocate budget from project
	if err := k.AllocateBudget(ctx, projectID, budget); err != nil {
		return 0, fmt.Errorf("failed to allocate budget: %w", err)
	}

	// Get next initiative ID
	initiativeID, err := k.InitiativeSeq.Next(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get next initiative ID: %w", err)
	}

	// Calculate required conviction based on budget and conviction_per_dream parameter
	// (params already fetched above for tier validation)
	// Formula: required_conviction = conviction_per_dream × sqrt(budget_micro_amount)
	// This scales the same way as actual conviction (which uses sqrt dampening)
	// Maintains constant stake-to-budget ratio across all budget sizes
	//
	// IMPORTANT: We take sqrt of the integer value first, then convert to Dec
	// because LegacyDec.ApproxSqrt() operates on the internal representation (value × 10^18)
	// which would give us sqrt(budget × 10^18) = sqrt(budget) × 10^9, which is wrong
	budgetFloat := budget.BigInt().Uint64()
	sqrtBudgetFloat := stdmath.Sqrt(float64(budgetFloat))
	sqrtBudget := math.LegacyMustNewDecFromStr(fmt.Sprintf("%.18f", sqrtBudgetFloat))
	requiredConviction := params.ConvictionPerDream.Mul(sqrtBudget)

	// Create initiative
	initiative := types.Initiative{
		Id:                    initiativeID,
		ProjectId:             projectID,
		Title:                 title,
		Description:           description,
		Tags:                  tags,
		Tier:                  tier,
		Category:              category,
		TemplateId:            templateID,
		Budget:                PtrInt(budget),
		RequiredConviction:    PtrDec(requiredConviction),
		CurrentConviction:     PtrDec(math.LegacyZeroDec()),
		ExternalConviction:    PtrDec(math.LegacyZeroDec()),
		ConvictionLastUpdated: sdk.UnwrapSDKContext(ctx).BlockHeight(),
		Status:                types.InitiativeStatus_INITIATIVE_STATUS_OPEN,
		CreatedAt:             sdk.UnwrapSDKContext(ctx).BlockTime().Unix(),
	}

	// Store initiative
	if err := k.Initiative.Set(ctx, initiativeID, initiative); err != nil {
		return 0, fmt.Errorf("failed to store initiative: %w", err)
	}

	// Add to status index for efficient EndBlocker lookups
	if err := k.AddInitiativeToStatusIndex(ctx, initiative); err != nil {
		return 0, fmt.Errorf("failed to add initiative to status index: %w", err)
	}

	// Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"initiative_created",
			sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiativeID)),
			sdk.NewAttribute("project_id", fmt.Sprintf("%d", projectID)),
			sdk.NewAttribute("creator", creator.String()),
			sdk.NewAttribute("tier", tier.String()),
			sdk.NewAttribute("budget", budget.String()),
		),
	)

	return initiativeID, nil
}

// GetInitiative retrieves an initiative by ID
func (k Keeper) GetInitiative(ctx context.Context, initiativeID uint64) (types.Initiative, error) {
	initiative, err := k.Initiative.Get(ctx, initiativeID)
	if err != nil {
		if err == collections.ErrNotFound {
			return types.Initiative{}, fmt.Errorf("initiative %d not found", initiativeID)
		}
		return types.Initiative{}, err
	}
	return initiative, nil
}

// UpdateInitiative updates an existing initiative and maintains the status index
func (k Keeper) UpdateInitiative(ctx context.Context, initiative types.Initiative) error {
	// Get old initiative to detect status changes
	oldInitiative, err := k.Initiative.Get(ctx, initiative.Id)
	if err == nil && oldInitiative.Status != initiative.Status {
		// Status changed - update the index
		if err := k.UpdateInitiativeStatusIndex(ctx, oldInitiative.Status, initiative.Status, initiative.Id); err != nil {
			return fmt.Errorf("failed to update initiative status index: %w", err)
		}
	}

	return k.Initiative.Set(ctx, initiative.Id, initiative)
}

// AssignInitiativeToMember assigns an initiative to a member
func (k Keeper) AssignInitiativeToMember(
	ctx context.Context,
	initiativeID uint64,
	assignee sdk.AccAddress,
) error {
	// Get initiative
	initiative, err := k.GetInitiative(ctx, initiativeID)
	if err != nil {
		return err
	}

	// Validate status
	if initiative.Status != types.InitiativeStatus_INITIATIVE_STATUS_OPEN {
		return fmt.Errorf("initiative must be in OPEN status")
	}

	// Get member to validate tier qualification
	member, err := k.GetMember(ctx, assignee)
	if err != nil {
		return fmt.Errorf("assignee is not a member: %w", err)
	}

	// Validate member is qualified for tier
	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("failed to get params: %w", err)
	}

	var tierConfig types.TierConfig
	switch initiative.Tier {
	case types.InitiativeTier_INITIATIVE_TIER_APPRENTICE:
		tierConfig = params.ApprenticeTier
	case types.InitiativeTier_INITIATIVE_TIER_STANDARD:
		tierConfig = params.StandardTier
	case types.InitiativeTier_INITIATIVE_TIER_EXPERT:
		tierConfig = params.ExpertTier
	case types.InitiativeTier_INITIATIVE_TIER_EPIC:
		tierConfig = params.EpicTier
	}

	// Check reputation for initiative tags
	totalRep := math.LegacyZeroDec()
	for _, tag := range initiative.Tags {
		if repStr, ok := member.ReputationScores[tag]; ok {
			rep, err := math.LegacyNewDecFromStr(repStr)
			if err == nil {
				totalRep = totalRep.Add(rep)
			}
		}
	}

	// Calculate average reputation - handle case where initiative has no tags
	var avgRep math.LegacyDec
	if len(initiative.Tags) > 0 {
		avgRep = totalRep.QuoInt64(int64(len(initiative.Tags)))
	} else {
		// No tags - calculate average from all reputation scores
		if len(member.ReputationScores) > 0 {
			totalAllRep := math.LegacyZeroDec()
			for _, repStr := range member.ReputationScores {
				rep, err := math.LegacyNewDecFromStr(repStr)
				if err == nil {
					totalAllRep = totalAllRep.Add(rep)
				}
			}
			avgRep = totalAllRep.QuoInt64(int64(len(member.ReputationScores)))
		} else {
			avgRep = math.LegacyZeroDec()
		}
	}

	if avgRep.LT(tierConfig.MinReputation) {
		return fmt.Errorf("insufficient reputation for tier: have %s, need %s", avgRep.String(), tierConfig.MinReputation.String())
	}

	// Prevent self-assignment if member created the project
	project, err := k.GetProject(ctx, initiative.ProjectId)
	if err != nil {
		return err
	}
	if project.Creator == assignee.String() {
		return fmt.Errorf("project creator cannot self-assign initiatives")
	}

	// Update initiative
	initiative.Assignee = assignee.String()
	initiative.AssignedAt = sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
	initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED

	if err := k.UpdateInitiative(ctx, initiative); err != nil {
		return err
	}

	// Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"initiative_assigned",
			sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiativeID)),
			sdk.NewAttribute("assignee", assignee.String()),
		),
	)

	return nil
}

// SubmitInitiativeWork submits work for review
func (k Keeper) SubmitInitiativeWork(
	ctx context.Context,
	initiativeID uint64,
	assignee sdk.AccAddress,
	deliverableURI string,
) error {
	// Get initiative
	initiative, err := k.GetInitiative(ctx, initiativeID)
	if err != nil {
		return err
	}

	// Validate assignee
	if initiative.Assignee != assignee.String() {
		return fmt.Errorf("only assignee can submit work")
	}

	// Validate status
	if initiative.Status != types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED {
		return fmt.Errorf("initiative must be in ASSIGNED status")
	}

	// Get params for review periods
	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("failed to get params: %w", err)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentHeight := sdkCtx.BlockHeight()

	// Update initiative
	initiative.DeliverableUri = deliverableURI
	initiative.SubmittedAt = sdkCtx.BlockTime().Unix()
	initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED
	initiative.ReviewPeriodEnd = currentHeight + (params.DefaultReviewPeriodEpochs * params.EpochBlocks)

	if err := k.UpdateInitiative(ctx, initiative); err != nil {
		return err
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"initiative_work_submitted",
			sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiativeID)),
			sdk.NewAttribute("assignee", assignee.String()),
			sdk.NewAttribute("deliverable_uri", deliverableURI),
		),
	)

	return nil
}

// AbandonInitiative allows assignee to abandon an initiative
func (k Keeper) AbandonInitiative(
	ctx context.Context,
	initiativeID uint64,
	assignee sdk.AccAddress,
	reason string,
) error {
	// Get initiative
	initiative, err := k.GetInitiative(ctx, initiativeID)
	if err != nil {
		return err
	}

	// Validate assignee
	if initiative.Assignee != assignee.String() {
		return fmt.Errorf("only assignee can abandon initiative")
	}

	// Return budget to project
	if err := k.ReturnBudget(ctx, initiative.ProjectId, DerefInt(initiative.Budget)); err != nil {
		return fmt.Errorf("failed to return budget: %w", err)
	}

	// Update initiative
	initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_ABANDONED

	if err := k.UpdateInitiative(ctx, initiative); err != nil {
		return err
	}

	// Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"initiative_abandoned",
			sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiativeID)),
			sdk.NewAttribute("assignee", assignee.String()),
			sdk.NewAttribute("reason", reason),
		),
	)

	return nil
}

// CompleteInitiative completes an initiative and distributes rewards
func (k Keeper) CompleteInitiative(ctx context.Context, initiativeID uint64) error {
	// Get initiative
	initiative, err := k.GetInitiative(ctx, initiativeID)
	if err != nil {
		return err
	}

	// Validate status - must be SUBMITTED or IN_REVIEW
	// SUBMITTED: Manual completion after conviction met
	// IN_REVIEW: Automatic completion after challenge period
	if initiative.Status != types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED &&
		initiative.Status != types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW {
		return fmt.Errorf("initiative must be in SUBMITTED or IN_REVIEW status, got %s", initiative.Status)
	}

	// Check if completion requirements are met
	canComplete, err := k.CanCompleteInitiative(ctx, initiativeID)
	if err != nil {
		return fmt.Errorf("failed to check completion requirements: %w", err)
	}
	if !canComplete {
		return fmt.Errorf("initiative does not meet completion requirements")
	}

	// Get params for reward distribution
	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("failed to get params: %w", err)
	}

	// Calculate rewards
	totalReward := DerefInt(initiative.Budget)
	completerReward := math.LegacyNewDecFromInt(totalReward).Mul(params.CompleterShare).TruncateInt()
	// Treasury share is tracked but not distributed here (handled by treasury module)
	_ = math.LegacyNewDecFromInt(totalReward).Mul(params.TreasuryShare).TruncateInt()

	// Mint DREAM to assignee (completer)
	assigneeAddr, err := sdk.AccAddressFromBech32(initiative.Assignee)
	if err != nil {
		return fmt.Errorf("invalid assignee address: %w", err)
	}
	if err := k.MintDREAM(ctx, assigneeAddr, completerReward); err != nil {
		return fmt.Errorf("failed to mint DREAM for completer: %w", err)
	}

	// Distribute staking rewards to stakers based on time-weighted APY
	stakes, err := k.GetInitiativeStakes(ctx, initiativeID)
	if err != nil {
		return fmt.Errorf("failed to get stakes: %w", err)
	}

	// Get SDK context for event emission
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Distribute time-based APY rewards to stakers
	if len(stakes) > 0 {
		for _, stake := range stakes {
			stakerAddr, err := sdk.AccAddressFromBech32(stake.Staker)
			if err != nil {
				continue
			}

			// Calculate time-based staking reward (Stake × APY × Duration / Year)
			stakingReward, err := k.CalculateStakingReward(ctx, stake)
			if err != nil {
				return fmt.Errorf("failed to calculate staking reward for %s: %w", stake.Staker, err)
			}

			// Mint staking rewards to staker
			if stakingReward.GT(math.ZeroInt()) {
				if err := k.MintDREAM(ctx, stakerAddr, stakingReward); err != nil {
					return fmt.Errorf("failed to mint DREAM for staker %s: %w", stake.Staker, err)
				}
			}

			// Unlock staked DREAM
			if err := k.UnlockDREAM(ctx, stakerAddr, stake.Amount); err != nil {
				return fmt.Errorf("failed to unlock DREAM for staker %s: %w", stake.Staker, err)
			}

			// Remove stake from target index
			_ = k.RemoveStakeFromTargetIndex(ctx, stake)

			// Remove stake
			if err := k.Stake.Remove(ctx, stake.Id); err != nil {
				return fmt.Errorf("failed to remove stake: %w", err)
			}

			// Emit event for stake completion
			sdkCtx.EventManager().EmitEvent(
				sdk.NewEvent(
					"stake_completed",
					sdk.NewAttribute("stake_id", fmt.Sprintf("%d", stake.Id)),
					sdk.NewAttribute("staker", stake.Staker),
					sdk.NewAttribute("amount", stake.Amount.String()),
					sdk.NewAttribute("reward", stakingReward.String()),
					sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiativeID)),
				),
			)
		}
	}

	// Grant reputation to completer
	member, err := k.GetMember(ctx, assigneeAddr)
	if err != nil {
		return fmt.Errorf("failed to get member: %w", err)
	}

	// Calculate reputation grant based on tier
	var tierConfig types.TierConfig
	switch initiative.Tier {
	case types.InitiativeTier_INITIATIVE_TIER_APPRENTICE:
		tierConfig = params.ApprenticeTier
	case types.InitiativeTier_INITIATIVE_TIER_STANDARD:
		tierConfig = params.StandardTier
	case types.InitiativeTier_INITIATIVE_TIER_EXPERT:
		tierConfig = params.ExpertTier
	case types.InitiativeTier_INITIATIVE_TIER_EPIC:
		tierConfig = params.EpicTier
	}

	// Grant reputation for each tag
	for _, tag := range initiative.Tags {
		currentRep := math.LegacyZeroDec()
		if repStr, ok := member.ReputationScores[tag]; ok {
			currentRep, _ = math.LegacyNewDecFromStr(repStr)
		}

		// Reputation grant = min(budget / 10, tier cap - current rep)
		repGrant := math.LegacyNewDecFromInt(DerefInt(initiative.Budget)).QuoInt64(10)
		maxGrant := tierConfig.ReputationCap.Sub(currentRep)
		if repGrant.GT(maxGrant) {
			repGrant = maxGrant
		}

		if repGrant.GT(math.LegacyZeroDec()) {
			newRep := currentRep.Add(repGrant)
			if member.ReputationScores == nil {
				member.ReputationScores = make(map[string]string)
			}
			member.ReputationScores[tag] = newRep.String()
		}
	}

	// Increment completed initiatives count for potential future use (O(1) lookup)
	member.CompletedInitiativesCount++

	// Update member
	if err := k.Member.Set(ctx, assigneeAddr.String(), member); err != nil {
		return fmt.Errorf("failed to update member: %w", err)
	}

	// Check for trust level upgrade after reputation change (lazy evaluation)
	// This is a trigger point - we only check when reputation actually changes
	_ = k.UpdateTrustLevel(ctx, assigneeAddr)

	// Distribute revenue share to member stakers
	// Members who stake on the assignee receive a portion of the initiative earnings
	if err := k.AccumulateMemberStakeRevenue(ctx, assigneeAddr, completerReward); err != nil {
		// Log but don't fail - stake pools might not exist
		sdkCtx.Logger().Debug("failed to accumulate member stake revenue", "error", err, "member", assigneeAddr)
	}

	// Distribute revenue share to tag stakers
	// Members who stake on matching tags receive a portion of the initiative earnings
	if len(initiative.Tags) > 0 {
		if err := k.AccumulateTagStakeRevenue(ctx, initiative.Tags, completerReward); err != nil {
			sdkCtx.Logger().Debug("failed to accumulate tag stake revenue", "error", err, "tags", initiative.Tags)
		}
	}

	// Distribute conviction-based completion bonus to initiative stakers
	// This is a 10% bonus pool distributed based on conviction weight
	if len(stakes) > 0 {
		if err := k.DistributeInitiativeCompletionBonus(ctx, initiativeID, totalReward); err != nil {
			sdkCtx.Logger().Debug("failed to distribute initiative completion bonus", "error", err, "initiative_id", initiativeID)
		}
	}

	// Mark budget as spent in project
	if err := k.SpendBudget(ctx, initiative.ProjectId, DerefInt(initiative.Budget)); err != nil {
		return fmt.Errorf("failed to mark budget as spent: %w", err)
	}

	// Update initiative
	initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_COMPLETED
	initiative.CompletedAt = sdk.UnwrapSDKContext(ctx).BlockTime().Unix()

	if err := k.UpdateInitiative(ctx, initiative); err != nil {
		return err
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"initiative_completed",
			sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiativeID)),
			sdk.NewAttribute("assignee", initiative.Assignee),
			sdk.NewAttribute("project_id", fmt.Sprintf("%d", initiative.ProjectId)),
			sdk.NewAttribute("budget", initiative.Budget.String()),
			sdk.NewAttribute("completer_reward", completerReward.String()),
		),
	)

	return nil
}

// GetMember retrieves a member by address with lazy decay applied.
// This is the canonical way to get a member - it ensures decay is always current.
// Note: This applies and persists decay. For read-only access without persistence,
// use Member.Get directly and call ApplyPendingDecay without saving.
func (k Keeper) GetMember(ctx context.Context, address sdk.AccAddress) (types.Member, error) {
	member, err := k.Member.Get(ctx, address.String())
	if err != nil {
		if err == collections.ErrNotFound {
			return types.Member{}, fmt.Errorf("member not found: %s", address.String())
		}
		return types.Member{}, err
	}

	// Apply lazy decay - this ensures balances are always accurate
	if err := k.ApplyPendingDecay(ctx, &member); err != nil {
		return types.Member{}, err
	}

	// Persist the updated decay state
	if err := k.Member.Set(ctx, address.String(), member); err != nil {
		return types.Member{}, err
	}

	return member, nil
}
