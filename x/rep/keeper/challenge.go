package keeper

import (
	"context"
	"fmt"
	"strings"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/rep/types"
)

// CreateChallenge creates a new challenge on an initiative
func (k Keeper) CreateChallenge(
	ctx context.Context,
	challengerAddr sdk.AccAddress,
	initiativeID uint64,
	reason string,
	evidence []string,
	stakedDream math.Int,
) (uint64, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0, err
	}

	// Get the initiative
	initiative, err := k.GetInitiative(ctx, initiativeID)
	if err != nil {
		return 0, err
	}

	// Validate initiative status - allow challenges during submission and review
	// Challenges are not allowed on completed, abandoned, or already challenged initiatives
	validStatuses := []types.InitiativeStatus{
		types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED,
		types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW,
	}

	statusValid := false
	for _, validStatus := range validStatuses {
		if initiative.Status == validStatus {
			statusValid = true
			break
		}
	}

	if !statusValid {
		return 0, fmt.Errorf("challenges can only be created on SUBMITTED or IN_REVIEW initiatives, current status: %s", initiative.Status)
	}

	// Don't allow duplicate challenges on already challenged initiatives
	if initiative.Status == types.InitiativeStatus_INITIATIVE_STATUS_CHALLENGED {
		return 0, fmt.Errorf("initiative already has an active challenge")
	}

	// Validate and lock DREAM stake
	minStake := params.MinChallengeStake
	if stakedDream.LT(minStake) {
		return 0, fmt.Errorf("insufficient stake: %s, required: %s", stakedDream, minStake)
	}
	if err := k.LockDREAM(ctx, challengerAddr, stakedDream); err != nil {
		return 0, err
	}

	// Get next challenge ID
	challengeID, err := k.ChallengeSeq.Next(ctx)
	if err != nil {
		return 0, err
	}

	// Calculate response deadline
	responseDeadline := sdkCtx.BlockHeight() + (params.ChallengeResponseDeadlineEpochs * params.EpochBlocks)

	// Create challenge
	challenge := types.Challenge{
		Id:               challengeID,
		InitiativeId:     initiativeID,
		Challenger:       challengerAddr.String(),
		Reason:           reason,
		Evidence:         evidence,
		StakedDream:      PtrInt(stakedDream),
		Status:           types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE,
		CreatedAt:        sdkCtx.BlockHeight(),
		ResponseDeadline: responseDeadline,
	}

	// Save challenge
	if err := k.Challenge.Set(ctx, challengeID, challenge); err != nil {
		return 0, err
	}

	// Add to status index for efficient EndBlocker lookups
	if err := k.AddChallengeToStatusIndex(ctx, challenge); err != nil {
		return 0, fmt.Errorf("failed to add challenge to status index: %w", err)
	}

	// Update initiative status to challenged
	initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_CHALLENGED
	if err := k.Initiative.Set(ctx, initiative.Id, initiative); err != nil {
		return 0, err
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"challenge_created",
			sdk.NewAttribute("challenge_id", fmt.Sprintf("%d", challengeID)),
			sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiativeID)),
		),
	)

	return challengeID, nil
}

// RespondToChallenge allows the assignee to respond to a challenge
func (k Keeper) RespondToChallenge(
	ctx context.Context,
	challengeID uint64,
	responderAddr sdk.AccAddress,
	response string,
	evidence []string,
) error {
	// Get the challenge
	challenge, err := k.GetChallenge(ctx, challengeID)
	if err != nil {
		return err
	}

	// Validate challenge status
	if challenge.Status != types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE {
		return fmt.Errorf("challenge is not active")
	}

	// Get initiative
	initiative, err := k.GetInitiative(ctx, challenge.InitiativeId)
	if err != nil {
		return err
	}

	// Verify responder is the assignee
	if initiative.Assignee != responderAddr.String() {
		return types.ErrNotAssignee
	}

	// Triage the challenge
	result, err := k.TriageChallenge(ctx, challengeID, response, evidence)
	if err != nil {
		return err
	}

	// Handle triage result
	switch result {
	case TriageAutoApprove:
		return k.UpholdChallenge(ctx, challengeID)
	case TriageAutoReject:
		return k.RejectChallenge(ctx, challengeID)
	case TriageRouteToJury:
		// Attempt to create jury review
		err := k.CreateJuryReview(ctx, challengeID, response, evidence)
		if err != nil {
			// Check if error is due to insufficient jurors
			if strings.Contains(err.Error(), "insufficient eligible jurors") {
				// Escalate to technical committee instead
				return k.EscalateChallengeToCommittee(ctx, challengeID, response, evidence, "insufficient_jurors")
			}
			return err
		}
		return nil
	default:
		return fmt.Errorf("unknown triage result: %v", result)
	}
}

// TriageResult represents the result of challenge triage
type TriageResult int

const (
	TriageAutoApprove TriageResult = iota
	TriageAutoReject
	TriageRouteToJury
)

// TriageChallenge performs automatic triage of a challenge
func (k Keeper) TriageChallenge(
	ctx context.Context,
	challengeID uint64,
	response string,
	evidence []string,
) (TriageResult, error) {
	_, err := k.GetChallenge(ctx, challengeID)
	if err != nil {
		return 0, err
	}

	// For now, all challenges with responses go to jury
	// In a more sophisticated system, we could implement auto-triage logic:
	// - Auto-approve if assignee admits fault
	// - Auto-reject if challenge is obviously frivolous
	// - Otherwise route to jury

	// Simple triage: if response is empty, auto-uphold
	if response == "" {
		return TriageAutoApprove, nil
	}

	// Otherwise, route to jury for human review
	return TriageRouteToJury, nil
}

// UpholdChallenge upholds a challenge and slashes the assignee
func (k Keeper) UpholdChallenge(ctx context.Context, challengeID uint64) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	challenge, err := k.GetChallenge(ctx, challengeID)
	if err != nil {
		return err
	}

	initiative, err := k.GetInitiative(ctx, challenge.InitiativeId)
	if err != nil {
		return err
	}

	// Slash assignee reputation
	assigneeAddr, err := sdk.AccAddressFromBech32(initiative.Assignee)
	if err != nil {
		return err
	}

	member, err := k.GetMember(ctx, assigneeAddr)
	if err != nil {
		return err
	}

	// Apply severe slash (30% reputation loss)
	slashPenalty := params.SevereSlashPenalty
	for tag, scoreStr := range member.ReputationScores {
		currentScore, err := math.LegacyNewDecFromStr(scoreStr)
		if err != nil {
			continue
		}
		slashed := currentScore.Mul(slashPenalty)
		newScore := currentScore.Sub(slashed)
		if newScore.IsNegative() {
			newScore = math.LegacyZeroDec()
		}
		member.ReputationScores[tag] = newScore.String()
	}

	if err := k.Member.Set(ctx, member.Address, member); err != nil {
		return err
	}

	// Note: We don't call UpdateTrustLevel here since slashing only reduces reputation.
	// Trust levels only increase, never decrease, so no update is needed.

	// Reward challenger: unlock DREAM stake and mint DREAM reward
	challengerAddr, err := sdk.AccAddressFromBech32(challenge.Challenger)
	if err != nil {
		return err
	}

	stakedAmount := DerefInt(challenge.StakedDream)
	rewardRate := params.ChallengerRewardRate
	budgetAmount := DerefInt(initiative.Budget)
	rewardAmount := budgetAmount.ToLegacyDec().Mul(rewardRate).TruncateInt()

	// Unlock the staked DREAM (locked from challengerAddr)
	if err := k.UnlockDREAM(ctx, challengerAddr, stakedAmount); err != nil {
		return err
	}

	// Mint the reward amount (new DREAM for the challenger)
	if rewardAmount.IsPositive() {
		if err := k.MintDREAM(ctx, challengerAddr, rewardAmount); err != nil {
			return err
		}
	}

	// Update challenge status
	oldStatus := challenge.Status
	challenge.Status = types.ChallengeStatus_CHALLENGE_STATUS_UPHELD
	challenge.ResolvedAt = sdkCtx.BlockHeight()
	if err := k.Challenge.Set(ctx, challenge.Id, challenge); err != nil {
		return err
	}

	// Update status index
	_ = k.UpdateChallengeStatusIndex(ctx, oldStatus, challenge.Status, challenge.Id)

	// Update initiative status to rejected (failed challenge)
	initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_REJECTED
	if err := k.Initiative.Set(ctx, initiative.Id, initiative); err != nil {
		return err
	}

	// Return unspent budget to project
	if err := k.ReturnBudget(ctx, initiative.ProjectId, DerefInt(initiative.Budget)); err != nil {
		return err
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"challenge_upheld",
			sdk.NewAttribute("challenge_id", fmt.Sprintf("%d", challengeID)),
			sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", challenge.InitiativeId)),
		),
	)

	return nil
}

// RejectChallenge rejects a challenge and slashes the challenger
func (k Keeper) RejectChallenge(ctx context.Context, challengeID uint64) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	challenge, err := k.GetChallenge(ctx, challengeID)
	if err != nil {
		return err
	}

	initiative, err := k.GetInitiative(ctx, challenge.InitiativeId)
	if err != nil {
		return err
	}

	// Slash challenger's stake (burn the staked DREAM)
	challengerAddr, err := sdk.AccAddressFromBech32(challenge.Challenger)
	if err != nil {
		return err
	}
	stakedAmount := DerefInt(challenge.StakedDream)
	if err := k.BurnDREAM(ctx, challengerAddr, stakedAmount); err != nil {
		return err
	}

	// Update challenge status
	oldStatus := challenge.Status
	challenge.Status = types.ChallengeStatus_CHALLENGE_STATUS_REJECTED
	challenge.ResolvedAt = sdkCtx.BlockHeight()
	if err := k.Challenge.Set(ctx, challenge.Id, challenge); err != nil {
		return err
	}

	// Update status index
	_ = k.UpdateChallengeStatusIndex(ctx, oldStatus, challenge.Status, challenge.Id)

	// Restore initiative status to IN_REVIEW
	// (it was set to CHALLENGED when the challenge was created)
	// Challenge was rejected, so work is valid and ready for completion
	// NOT setting to SUBMITTED to avoid triggering another challenge period
	initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW
	if err := k.Initiative.Set(ctx, initiative.Id, initiative); err != nil {
		return err
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"challenge_rejected",
			sdk.NewAttribute("challenge_id", fmt.Sprintf("%d", challengeID)),
			sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", challenge.InitiativeId)),
		),
	)

	return nil
}

// GetChallenge retrieves a challenge by ID
func (k Keeper) GetChallenge(ctx context.Context, challengeID uint64) (types.Challenge, error) {
	challenge, err := k.Challenge.Get(ctx, challengeID)
	if err != nil {
		return types.Challenge{}, err
	}
	return challenge, nil
}

// SetChallenge stores a challenge
func (k Keeper) SetChallenge(ctx context.Context, challenge types.Challenge) error {
	return k.Challenge.Set(ctx, challenge.Id, challenge)
}

// GetNextChallengeID returns the next challenge ID
func (k Keeper) GetNextChallengeID(ctx context.Context) uint64 {
	id, err := k.ChallengeSeq.Next(ctx)
	if err != nil {
		panic(err)
	}
	return id
}

// SetNextChallengeID sets the next challenge ID (deprecated - sequence auto-increments)
func (k Keeper) SetNextChallengeID(ctx context.Context, id uint64) {
	// Sequence is auto-incremented, no need to set manually
}

// HasActiveChallenges checks if an initiative has any active or in-review challenges.
// Uses the ChallengesByStatus index for efficient lookup instead of a full table scan.
func (k Keeper) HasActiveChallenges(ctx context.Context, initiativeID uint64) (bool, error) {
	// Check ACTIVE challenges via status index
	activeStatuses := []types.ChallengeStatus{
		types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE,
		types.ChallengeStatus_CHALLENGE_STATUS_IN_JURY_REVIEW,
	}

	for _, status := range activeStatuses {
		found := false
		err := k.IterateChallengesByStatus(ctx, status, func(id uint64, challenge types.Challenge) bool {
			if challenge.InitiativeId == initiativeID {
				found = true
				return true // stop iteration
			}
			return false
		})
		if err != nil {
			return false, err
		}
		if found {
			return true, nil
		}
	}

	return false, nil
}

// EscalateChallengeToCommittee escalates a challenge to the technical committee
// when there are insufficient qualified jurors or other exceptional circumstances
func (k Keeper) EscalateChallengeToCommittee(
	ctx context.Context,
	challengeID uint64,
	assigneeResponse string,
	assigneeEvidence []string,
	reason string,
) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	challenge, err := k.GetChallenge(ctx, challengeID)
	if err != nil {
		return err
	}

	// Update challenge status to indicate committee review
	oldStatus := challenge.Status
	challenge.Status = types.ChallengeStatus_CHALLENGE_STATUS_IN_JURY_REVIEW // Reuse status for committee review
	if err := k.Challenge.Set(ctx, challenge.Id, challenge); err != nil {
		return err
	}

	// Update challenge status index
	_ = k.UpdateChallengeStatusIndex(ctx, oldStatus, challenge.Status, challenge.Id)

	// Create ADJUDICATION interim for the technical committee
	authority := k.GetAuthorityString()
	_, err = k.CreateInterimWork(
		ctx,
		types.InterimType_INTERIM_TYPE_ADJUDICATION,
		[]string{authority},
		"technical_operations", // Tag for technical operations committee
		challenge.InitiativeId,
		fmt.Sprintf("Challenge %d escalated: %s. Assignee response: %s", challenge.Id, reason, assigneeResponse),
		types.InterimComplexity_INTERIM_COMPLEXITY_EPIC, // High priority
		sdkCtx.BlockHeight()+params.DefaultReviewPeriodEpochs*params.EpochBlocks,
	)
	if err != nil {
		return err
	}

	// Emit event for escalation
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"challenge_escalated",
			sdk.NewAttribute("challenge_id", fmt.Sprintf("%d", challengeID)),
			sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", challenge.InitiativeId)),
			sdk.NewAttribute("reason", reason),
		),
	)

	return nil
}
