package keeper

import (
	"context"
	"fmt"
	"strings"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// CreateInterimWork creates a new interim work assignment
func (k Keeper) CreateInterimWork(
	ctx context.Context,
	interimType types.InterimType,
	assignees []string,
	committee string,
	referenceID uint64,
	referenceType string,
	complexity types.InterimComplexity,
	deadline int64,
) (uint64, error) {
	// Get next interim ID
	interimID, err := k.InterimSeq.Next(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get next interim ID: %w", err)
	}

	// Calculate budget based on complexity
	budget := k.GetInterimBudget(ctx, complexity)

	// Solo expert bonus
	if len(assignees) == 1 && complexity == types.InterimComplexity_INTERIM_COMPLEXITY_EXPERT {
		params, err := k.Params.Get(ctx)
		if err == nil {
			bonus := math.LegacyNewDecFromInt(budget).Mul(params.SoloExpertBonusRate)
			budget = budget.Add(bonus.TruncateInt())
		}
	}

	// Create interim
	interim := types.Interim{
		Id:            interimID,
		Type:          interimType,
		Assignees:     assignees,
		Committee:     committee,
		ReferenceId:   referenceID,
		ReferenceType: referenceType,
		Complexity:    complexity,
		Budget:        PtrInt(budget),
		Deadline:      deadline,
		CreatedAt:     sdk.UnwrapSDKContext(ctx).BlockTime().Unix(),
		Status:        types.InterimStatus_INTERIM_STATUS_PENDING,
	}

	// Store interim
	if err := k.Interim.Set(ctx, interimID, interim); err != nil {
		return 0, fmt.Errorf("failed to store interim: %w", err)
	}

	// Add to status index for efficient EndBlocker lookups
	if err := k.AddInterimToStatusIndex(ctx, interim); err != nil {
		return 0, fmt.Errorf("failed to add interim to status index: %w", err)
	}

	// Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"interim_created",
			sdk.NewAttribute("interim_id", fmt.Sprintf("%d", interimID)),
			sdk.NewAttribute("type", interimType.String()),
			sdk.NewAttribute("complexity", complexity.String()),
			sdk.NewAttribute("budget", budget.String()),
		),
	)

	return interimID, nil
}

// GetInterim retrieves an interim by ID
func (k Keeper) GetInterim(ctx context.Context, interimID uint64) (types.Interim, error) {
	interim, err := k.Interim.Get(ctx, interimID)
	if err != nil {
		if err == collections.ErrNotFound {
			return types.Interim{}, fmt.Errorf("interim %d not found", interimID)
		}
		return types.Interim{}, err
	}
	return interim, nil
}

// UpdateInterim updates an existing interim and maintains the status index
func (k Keeper) UpdateInterim(ctx context.Context, interim types.Interim) error {
	// Get old interim to detect status changes
	oldInterim, err := k.Interim.Get(ctx, interim.Id)
	if err == nil && oldInterim.Status != interim.Status {
		// Status changed - update the index
		if err := k.UpdateInterimStatusIndex(ctx, oldInterim.Status, interim.Status, interim.Id); err != nil {
			return fmt.Errorf("failed to update interim status index: %w", err)
		}
	}

	return k.Interim.Set(ctx, interim.Id, interim)
}

// AssignInterimToMember assigns an interim to a specific member
func (k Keeper) AssignInterimToMember(
	ctx context.Context,
	interimID uint64,
	assignee sdk.AccAddress,
) error {
	// Get interim
	interim, err := k.GetInterim(ctx, interimID)
	if err != nil {
		return err
	}

	// Validate status
	if interim.Status != types.InterimStatus_INTERIM_STATUS_PENDING {
		return fmt.Errorf("interim must be in PENDING status")
	}

	// Validate assignee is a member
	_, err = k.GetMember(ctx, assignee)
	if err != nil {
		return fmt.Errorf("assignee is not a member: %w", err)
	}

	// Add assignee if not already in list
	found := false
	for _, existing := range interim.Assignees {
		if existing == assignee.String() {
			found = true
			break
		}
	}

	if !found {
		interim.Assignees = append(interim.Assignees, assignee.String())
	}

	// Update status
	interim.Status = types.InterimStatus_INTERIM_STATUS_IN_PROGRESS

	if err := k.UpdateInterim(ctx, interim); err != nil {
		return err
	}

	// Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"interim_assigned",
			sdk.NewAttribute("interim_id", fmt.Sprintf("%d", interimID)),
			sdk.NewAttribute("assignee", assignee.String()),
		),
	)

	return nil
}

// SubmitInterimWork submits work for an interim
func (k Keeper) SubmitInterimWork(
	ctx context.Context,
	interimID uint64,
	assignee sdk.AccAddress,
	deliverableURI, comments string,
) error {
	// Get interim
	interim, err := k.GetInterim(ctx, interimID)
	if err != nil {
		return err
	}

	// Validate assignee
	found := false
	for _, existing := range interim.Assignees {
		if existing == assignee.String() {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("not an assignee of this interim")
	}

	// Validate status
	if interim.Status != types.InterimStatus_INTERIM_STATUS_IN_PROGRESS && interim.Status != types.InterimStatus_INTERIM_STATUS_PENDING {
		return fmt.Errorf("interim must be in PENDING or IN_PROGRESS status")
	}

	// Update status - work submitted, awaiting approval
	interim.Status = types.InterimStatus_INTERIM_STATUS_IN_PROGRESS
	interim.CompletionNotes = comments

	if err := k.UpdateInterim(ctx, interim); err != nil {
		return err
	}

	// Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"interim_work_submitted",
			sdk.NewAttribute("interim_id", fmt.Sprintf("%d", interimID)),
			sdk.NewAttribute("assignee", assignee.String()),
			sdk.NewAttribute("deliverable_uri", deliverableURI),
		),
	)

	return nil
}

// ApproveInterim approves an interim and pays the assignees
func (k Keeper) ApproveInterim(
	ctx context.Context,
	interimID uint64,
	approver sdk.AccAddress,
	approved bool,
	comments string,
) error {
	// Get interim
	interim, err := k.GetInterim(ctx, interimID)
	if err != nil {
		return err
	}

	// Validate approver has authority (Operations Committee)
	if !k.IsOperationsCommittee(ctx, approver) {
		return fmt.Errorf("approver %s is not authorized (requires Operations Committee)", approver.String())
	}

	// If approved, pay assignees and mark complete
	if approved {
		// Distribute payment equally among assignees
		if len(interim.Assignees) > 0 {
			paymentPerAssignee := interim.Budget.QuoRaw(int64(len(interim.Assignees)))

			for _, assigneeStr := range interim.Assignees {
				assigneeAddr, err := sdk.AccAddressFromBech32(assigneeStr)
				if err != nil {
					continue
				}
				// Mint DREAM to assignee
				if err := k.MintDREAM(ctx, assigneeAddr, paymentPerAssignee); err != nil {
					return fmt.Errorf("failed to mint DREAM for assignee %s: %w", assigneeStr, err)
				}

				// Grant reputation for interim completion
				// Reputation tag is based on interim type for tracking different work categories
				if err := k.GrantInterimReputation(ctx, assigneeAddr, interim); err != nil {
					sdk.UnwrapSDKContext(ctx).Logger().Debug("failed to grant interim reputation", "error", err)
				}

				// Increment completed interims count for trust level calculation (O(1) lookup)
				if err := k.IncrementMemberCompletedInterims(ctx, assigneeAddr); err != nil {
					// Log but don't fail - cached count is for optimization only
					sdk.UnwrapSDKContext(ctx).Logger().Debug("failed to increment completed interims count", "error", err)
				}

				// Check for trust level upgrade after interim completion (lazy evaluation)
				_ = k.UpdateTrustLevel(ctx, assigneeAddr)
			}
		}

		interim.Status = types.InterimStatus_INTERIM_STATUS_COMPLETED
		interim.CompletedAt = sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
	} else {
		// Rejected - mark as expired
		interim.Status = types.InterimStatus_INTERIM_STATUS_EXPIRED
	}

	interim.CompletionNotes = comments

	if err := k.UpdateInterim(ctx, interim); err != nil {
		return err
	}

	// Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"interim_approved",
			sdk.NewAttribute("interim_id", fmt.Sprintf("%d", interimID)),
			sdk.NewAttribute("approver", approver.String()),
			sdk.NewAttribute("approved", fmt.Sprintf("%t", approved)),
		),
	)

	return nil
}

// CompleteInterimDirectly completes an interim without approval (for automatic completions)
func (k Keeper) CompleteInterimDirectly(
	ctx context.Context,
	interimID uint64,
	notes string,
) error {
	interim, err := k.GetInterim(ctx, interimID)
	if err != nil {
		return err
	}

	// Distribute payment equally among assignees
	// Skip payment for ADJUDICATION interims (committee work, no DREAM reward)
	if len(interim.Assignees) > 0 && interim.Type != types.InterimType_INTERIM_TYPE_ADJUDICATION {
		paymentPerAssignee := interim.Budget.QuoRaw(int64(len(interim.Assignees)))

		for _, assigneeStr := range interim.Assignees {
			assigneeAddr, err := sdk.AccAddressFromBech32(assigneeStr)
			if err != nil {
				continue
			}
			// Mint DREAM to assignee
			if err := k.MintDREAM(ctx, assigneeAddr, paymentPerAssignee); err != nil {
				return fmt.Errorf("failed to mint DREAM for assignee %s: %w", assigneeStr, err)
			}

			// Grant reputation for interim completion
			// Reputation tag is based on interim type for tracking different work categories
			if err := k.GrantInterimReputation(ctx, assigneeAddr, interim); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Debug("failed to grant interim reputation", "error", err)
			}

			// Increment completed interims count for trust level calculation (O(1) lookup)
			if err := k.IncrementMemberCompletedInterims(ctx, assigneeAddr); err != nil {
				// Log but don't fail - cached count is for optimization only
				sdk.UnwrapSDKContext(ctx).Logger().Debug("failed to increment completed interims count", "error", err)
			}

			// Check for trust level upgrade after interim completion (lazy evaluation)
			// Completed interims count toward trust level progression
			_ = k.UpdateTrustLevel(ctx, assigneeAddr)
		}
	}

	interim.Status = types.InterimStatus_INTERIM_STATUS_COMPLETED
	interim.CompletedAt = sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
	interim.CompletionNotes = notes

	// For ADJUDICATION interims, automatically resolve the challenge based on decision
	if interim.Type == types.InterimType_INTERIM_TYPE_ADJUDICATION && interim.ReferenceId != 0 {
		// The reference_id is the initiative_id
		// Find the challenge associated with this initiative
		var challengeID uint64
		err := k.Challenge.Walk(ctx, nil, func(id uint64, challenge types.Challenge) (stop bool, err error) {
			if challenge.InitiativeId == interim.ReferenceId &&
				challenge.Status == types.ChallengeStatus_CHALLENGE_STATUS_IN_JURY_REVIEW {
				challengeID = id
				return true, nil // stop iteration
			}
			return false, nil
		})
		if err == nil && challengeID != 0 {
			// Parse decision from completion notes (UPHOLD or REJECT)
			decision := strings.ToUpper(notes)
			if strings.Contains(decision, "REJECT") || strings.Contains(decision, "REJECTED") {
				// Committee decided to reject the challenge
				_ = k.RejectChallenge(ctx, challengeID)
			} else if strings.Contains(decision, "UPHOLD") || strings.Contains(decision, "UPHELD") {
				// Committee decided to uphold the challenge
				_ = k.UpholdChallenge(ctx, challengeID)
			}
			// If neither keyword found, challenge remains in review (awaiting clearer decision)
		}
	}

	return k.UpdateInterim(ctx, interim)
}

// ExpireInterim marks an interim as expired when deadline passes
func (k Keeper) ExpireInterim(ctx context.Context, interimID uint64) error {
	interim, err := k.GetInterim(ctx, interimID)
	if err != nil {
		return err
	}

	interim.Status = types.InterimStatus_INTERIM_STATUS_EXPIRED

	return k.UpdateInterim(ctx, interim)
}

// GetInterimBudget returns the budget for a given complexity level
func (k Keeper) GetInterimBudget(ctx context.Context, complexity types.InterimComplexity) math.Int {
	params, err := k.Params.Get(ctx)
	if err != nil {
		// Return defaults if params not available
		switch complexity {
		case types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE:
			return math.NewInt(50)
		case types.InterimComplexity_INTERIM_COMPLEXITY_STANDARD:
			return math.NewInt(150)
		case types.InterimComplexity_INTERIM_COMPLEXITY_COMPLEX:
			return math.NewInt(400)
		case types.InterimComplexity_INTERIM_COMPLEXITY_EXPERT:
			return math.NewInt(1000)
		// For EPIC (newly added), assuming large budget
		case types.InterimComplexity_INTERIM_COMPLEXITY_EPIC:
			return math.NewInt(2500)
		}
	}

	switch complexity {
	case types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE:
		return params.SimpleComplexityBudget
	case types.InterimComplexity_INTERIM_COMPLEXITY_STANDARD:
		return params.StandardComplexityBudget
	case types.InterimComplexity_INTERIM_COMPLEXITY_COMPLEX:
		return params.ComplexComplexityBudget
	case types.InterimComplexity_INTERIM_COMPLEXITY_EXPERT:
		return params.ExpertComplexityBudget
	case types.InterimComplexity_INTERIM_COMPLEXITY_EPIC:
		// Fallback for Epic if params not updated
		return math.NewInt(2500)
	default:
		return math.NewInt(150)
	}
}
