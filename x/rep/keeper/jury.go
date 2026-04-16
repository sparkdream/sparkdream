package keeper

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/rand"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/rep/types"
)

// CreateJuryReview creates a jury review for a challenge
func (k Keeper) CreateJuryReview(
	ctx context.Context,
	challengeID uint64,
	assigneeResponse string,
	assigneeEvidence []string,
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

	initiative, err := k.GetInitiative(ctx, challenge.InitiativeId)
	if err != nil {
		return err
	}

	// Select jury members
	jurors, err := k.SelectJury(ctx, initiative, params.JurySize)
	if err != nil {
		return err
	}

	// Calculate required votes (supermajority)
	superMajority := params.JurySuperMajority
	requiredVotes := superMajority.MulInt64(int64(len(jurors))).Ceil().TruncateInt().Uint64()

	// Get next jury review ID
	juryReviewID, err := k.JuryReviewSeq.Next(ctx)
	if err != nil {
		return err
	}

	// Create jury review
	juryReview := types.JuryReview{
		Id:                juryReviewID,
		ChallengeId:       challengeID,
		InitiativeId:      challenge.InitiativeId,
		Jurors:            jurors,
		RequiredVotes:     uint32(requiredVotes),
		ExpertWitnesses:   []string{},
		Testimonies:       []*types.ExpertTestimony{},
		ReviewDeliverable: initiative.DeliverableUri,
		ChallengerClaim:   challenge.Reason,
		AssigneeResponse:  assigneeResponse,
		Votes:             []*types.JurorVote{},
		Deadline:          sdkCtx.BlockHeight() + params.DefaultReviewPeriodEpochs*params.EpochBlocks,
		Verdict:           types.Verdict_VERDICT_PENDING,
	}

	// Save jury review
	if err := k.JuryReview.Set(ctx, juryReview.Id, juryReview); err != nil {
		return err
	}

	// Update challenge status
	oldStatus := challenge.Status
	challenge.Status = types.ChallengeStatus_CHALLENGE_STATUS_IN_JURY_REVIEW
	if err := k.Challenge.Set(ctx, challenge.Id, challenge); err != nil {
		return err
	}

	// Update challenge status index
	_ = k.UpdateChallengeStatusIndex(ctx, oldStatus, challenge.Status, challenge.Id)

	// Create JURY_DUTY interim for each juror
	for _, jurorAddr := range jurors {
		_, err := k.CreateInterimWork(
			ctx,
			types.InterimType_INTERIM_TYPE_JURY_DUTY,
			[]string{jurorAddr},
			"", // Committee determined by governance
			challenge.InitiativeId,
			"challenge",
			types.InterimComplexity_INTERIM_COMPLEXITY_STANDARD,
			juryReview.Deadline,
		)
		if err != nil {
			return err
		}
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"jury_review_created",
			sdk.NewAttribute("jury_review_id", fmt.Sprintf("%d", juryReviewID)),
			sdk.NewAttribute("challenge_id", fmt.Sprintf("%d", challengeID)),
			sdk.NewAttribute("juror_count", fmt.Sprintf("%d", len(jurors))),
		),
	)

	return nil
}

// SelectJury selects jury members for a challenge
func (k Keeper) SelectJury(
	ctx context.Context,
	initiative types.Initiative,
	jurySize uint32,
) ([]string, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	// Get all eligible members
	eligibleMembers := []types.Member{}
	minReputation := params.MinJurorReputation

	// Iterate through all members
	err = k.Member.Walk(ctx, nil, func(addr string, member types.Member) (stop bool, err error) {
		// Skip if affiliated with initiative (including project creator)
		if k.IsAffiliatedWithProject(ctx, initiative, addr) {
			return false, nil
		}

		// Check reputation requirement
		hasReputation := false
		for _, tag := range initiative.Tags {
			if scoreStr, ok := member.ReputationScores[tag]; ok {
				score, err := math.LegacyNewDecFromStr(scoreStr)
				if err != nil {
					continue
				}
				if score.GTE(minReputation) {
					hasReputation = true
					break
				}
			}
		}

		if hasReputation {
			eligibleMembers = append(eligibleMembers, member)
		}

		return false, nil
	})
	if err != nil {
		return nil, err
	}

	// Check if we have enough eligible members
	if len(eligibleMembers) < int(jurySize) {
		return nil, fmt.Errorf("insufficient eligible jurors: need %d, have %d", jurySize, len(eligibleMembers))
	}

	// Weighted random selection based on reputation
	selectedJurors := []string{}
	weights := make([]float64, len(eligibleMembers))

	// Calculate weights based on domain reputation
	for i, member := range eligibleMembers {
		totalRep := math.LegacyZeroDec()
		for _, tag := range initiative.Tags {
			if scoreStr, ok := member.ReputationScores[tag]; ok {
				score, err := math.LegacyNewDecFromStr(scoreStr)
				if err != nil {
					continue
				}
				totalRep = totalRep.Add(score)
			}
		}
		weights[i] = totalRep.MustFloat64()
	}

	// Create a deterministic PRNG seeded from block hash + initiative ID.
	// This ensures all validators produce identical jury selections for the
	// same block, preventing consensus failure.
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	appHash := sdkCtx.BlockHeader().AppHash
	var seed int64
	if len(appHash) >= 8 {
		seed = int64(binary.BigEndian.Uint64(appHash[:8])) ^ int64(initiative.Id)
	} else {
		// Fallback for genesis block or test contexts where AppHash is empty
		seed = int64(initiative.Id) ^ sdkCtx.BlockHeight()
	}
	rng := rand.New(rand.NewSource(seed))

	// Perform weighted random selection without replacement
	for i := 0; i < int(jurySize); i++ {
		selected := weightedRandomSelect(rng, weights)
		selectedJurors = append(selectedJurors, eligibleMembers[selected].Address)

		// Remove selected juror from pool
		eligibleMembers = append(eligibleMembers[:selected], eligibleMembers[selected+1:]...)
		weights = append(weights[:selected], weights[selected+1:]...)
	}

	return selectedJurors, nil
}

// weightedRandomSelect performs weighted random selection using a deterministic PRNG.
func weightedRandomSelect(rng *rand.Rand, weights []float64) int {
	total := 0.0
	for _, w := range weights {
		total += w
	}

	if total == 0 {
		// If all weights are zero, use uniform random
		return rng.Intn(len(weights))
	}

	r := rng.Float64() * total
	sum := 0.0
	for i, w := range weights {
		sum += w
		if r <= sum {
			return i
		}
	}

	return len(weights) - 1
}

// SubmitJurorVote records a juror's vote on a challenge
func (k Keeper) SubmitJurorVote(
	ctx context.Context,
	juryReviewID uint64,
	jurorAddr sdk.AccAddress,
	criteriaVotes []*types.CriteriaVote,
	verdict types.Verdict,
	confidence math.LegacyDec,
	reasoning string,
) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	juryReview, err := k.GetJuryReview(ctx, juryReviewID)
	if err != nil {
		return err
	}

	// Verify juror is on the jury
	jurorAddrStr := jurorAddr.String()
	isJuror := false
	for _, j := range juryReview.Jurors {
		if j == jurorAddrStr {
			isJuror = true
			break
		}
	}
	if !isJuror {
		return fmt.Errorf("address is not a juror on this review")
	}

	// Check if juror already voted
	for _, vote := range juryReview.Votes {
		if vote.Juror == jurorAddrStr {
			return fmt.Errorf("juror has already voted")
		}
	}

	// Check deadline
	if sdkCtx.BlockHeight() > juryReview.Deadline {
		return fmt.Errorf("voting deadline has passed")
	}

	// Create vote
	vote := &types.JurorVote{
		Juror:         jurorAddrStr,
		CriteriaVotes: criteriaVotes,
		Verdict:       verdict,
		Confidence:    PtrDec(confidence),
		Reasoning:     reasoning,
		SubmittedAt:   sdkCtx.BlockHeight(),
	}

	// Add vote to jury review
	juryReview.Votes = append(juryReview.Votes, vote)

	// Save jury review
	if err := k.JuryReview.Set(ctx, juryReview.Id, juryReview); err != nil {
		return err
	}

	// Check if we have enough votes to tally
	if uint32(len(juryReview.Votes)) >= juryReview.RequiredVotes {
		if err := k.TallyJuryVotes(ctx, juryReviewID); err != nil {
			return err
		}
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"juror_vote_submitted",
			sdk.NewAttribute("jury_review_id", fmt.Sprintf("%d", juryReviewID)),
			sdk.NewAttribute("juror", jurorAddrStr),
			sdk.NewAttribute("verdict", verdict.String()),
		),
	)

	return nil
}

// SubmitExpertTestimony records expert testimony for a challenge
func (k Keeper) SubmitExpertTestimony(
	ctx context.Context,
	juryReviewID uint64,
	expertAddr sdk.AccAddress,
	opinion string,
	reasoning string,
) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	juryReview, err := k.GetJuryReview(ctx, juryReviewID)
	if err != nil {
		return err
	}

	// Verify expert is on the witness list
	expertAddrStr := expertAddr.String()
	isExpert := false
	for _, e := range juryReview.ExpertWitnesses {
		if e == expertAddrStr {
			isExpert = true
			break
		}
	}
	if !isExpert {
		return fmt.Errorf("address is not an expert witness on this review")
	}

	// Create testimony
	testimony := &types.ExpertTestimony{
		Expert:      expertAddrStr,
		Opinion:     opinion,
		Reasoning:   reasoning,
		SubmittedAt: sdkCtx.BlockHeight(),
	}

	// Add testimony to jury review
	juryReview.Testimonies = append(juryReview.Testimonies, testimony)

	// Save jury review
	if err := k.JuryReview.Set(ctx, juryReview.Id, juryReview); err != nil {
		return err
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"expert_testimony_submitted",
			sdk.NewAttribute("jury_review_id", fmt.Sprintf("%d", juryReviewID)),
			sdk.NewAttribute("expert", expertAddrStr),
		),
	)

	return nil
}

// TallyJuryVotes tallies the jury votes and determines the final verdict
func (k Keeper) TallyJuryVotes(ctx context.Context, juryReviewID uint64) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	juryReview, err := k.GetJuryReview(ctx, juryReviewID)
	if err != nil {
		return err
	}

	// Count votes
	upholdVotes := 0
	rejectVotes := 0
	totalVotes := len(juryReview.Votes)

	for _, vote := range juryReview.Votes {
		switch vote.Verdict {
		case types.Verdict_VERDICT_UPHOLD_CHALLENGE:
			upholdVotes++
		case types.Verdict_VERDICT_REJECT_CHALLENGE:
			rejectVotes++
		}
	}

	// Determine verdict based on supermajority
	superMajority := params.JurySuperMajority
	requiredSupermajority := superMajority.MulInt64(int64(totalVotes)).Ceil().TruncateInt().Uint64()

	var finalVerdict types.Verdict
	if upholdVotes >= int(requiredSupermajority) {
		finalVerdict = types.Verdict_VERDICT_UPHOLD_CHALLENGE
	} else if rejectVotes > totalVotes/2 {
		finalVerdict = types.Verdict_VERDICT_REJECT_CHALLENGE
	} else {
		finalVerdict = types.Verdict_VERDICT_INCONCLUSIVE
	}

	// Update jury review
	juryReview.Verdict = finalVerdict

	// Consolidate reasoning from all votes
	consolidatedReasoning := ""
	for i, vote := range juryReview.Votes {
		if i > 0 {
			consolidatedReasoning += "\n---\n"
		}
		consolidatedReasoning += fmt.Sprintf("Juror %d: %s", i+1, vote.Reasoning)
	}
	juryReview.Reasoning = consolidatedReasoning

	if err := k.JuryReview.Set(ctx, juryReview.Id, juryReview); err != nil {
		return err
	}

	// Content challenge resolution (dispatched when ContentChallengeId > 0)
	if juryReview.ContentChallengeId > 0 {
		switch finalVerdict {
		case types.Verdict_VERDICT_UPHOLD_CHALLENGE:
			if err := k.UpholdContentChallenge(ctx, juryReview.ContentChallengeId); err != nil {
				return err
			}
		case types.Verdict_VERDICT_REJECT_CHALLENGE:
			if err := k.RejectContentChallenge(ctx, juryReview.ContentChallengeId); err != nil {
				return err
			}
		case types.Verdict_VERDICT_INCONCLUSIVE:
			if err := k.ResolveInconclusiveContentChallenge(ctx, juryReview.ContentChallengeId); err != nil {
				return err
			}
		}
		// Reward jurors for participating
		return k.RewardJurors(ctx, juryReview)
	}

	// Initiative challenge resolution
	challenge, err := k.GetChallenge(ctx, juryReview.ChallengeId)
	if err != nil {
		return err
	}

	switch finalVerdict {
	case types.Verdict_VERDICT_UPHOLD_CHALLENGE:
		if err := k.UpholdChallenge(ctx, challenge.Id); err != nil {
			return err
		}
	case types.Verdict_VERDICT_REJECT_CHALLENGE:
		if err := k.RejectChallenge(ctx, challenge.Id); err != nil {
			return err
		}
	case types.Verdict_VERDICT_INCONCLUSIVE:
		// Escalate to Operations Committee (Technical Council)
		// We create a special ADJUDICATION interim assigned to the committee (effectively)
		// Since we can't assign to a group directly in current Interim model (it takes strings which are usually member addresses),
		// we will assign to the module account (or leave empty if valid) and tag it for the committee.
		// For MVP, we assign to the module authority (gov module) as a placeholder for "Community Review".

		authority := k.GetAuthorityString()
		_, err := k.CreateInterimWork(
			ctx,
			types.InterimType_INTERIM_TYPE_ADJUDICATION,
			[]string{authority},
			"technical_operations", // Tag for committee
			challenge.InitiativeId,
			fmt.Sprintf("Inconclusive jury for challenge %d. Requires manual adjudication.", challenge.Id),
			types.InterimComplexity_INTERIM_COMPLEXITY_EPIC, // High priority/complexity
			sdk.UnwrapSDKContext(ctx).BlockHeight()+params.DefaultReviewPeriodEpochs*params.EpochBlocks,
		)
		if err != nil {
			return err
		}

		// Emit event for escalation
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				"challenge_escalated",
				sdk.NewAttribute("challenge_id", fmt.Sprintf("%d", challenge.Id)),
				sdk.NewAttribute("reason", "jury_inconclusive"),
			),
		)
	}

	// Reward jurors for participating
	if err := k.RewardJurors(ctx, juryReview); err != nil {
		return err
	}

	return nil
}

// RewardJurors rewards jurors for their participation
func (k Keeper) RewardJurors(ctx context.Context, juryReview types.JuryReview) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	// Jurors receive their interim compensation
	standardBudget := params.StandardComplexityBudget

	for _, jurorAddrStr := range juryReview.Jurors {
		jurorAddr, err := sdk.AccAddressFromBech32(jurorAddrStr)
		if err != nil {
			continue
		}

		// Check if juror voted
		voted := false
		for _, vote := range juryReview.Votes {
			if vote.Juror == jurorAddrStr {
				voted = true
				break
			}
		}

		// Only reward jurors who voted
		if voted {
			if err := k.MintDREAM(ctx, jurorAddr, standardBudget); err != nil {
				return err
			}
		}
	}

	return nil
}

// GetJuryReview retrieves a jury review by ID
func (k Keeper) GetJuryReview(ctx context.Context, juryReviewID uint64) (types.JuryReview, error) {
	var juryReview types.JuryReview
	found, err := k.JuryReview.Get(ctx, juryReviewID)
	if err != nil {
		return juryReview, err
	}
	return found, nil
}

// CreateAppealInitiative creates a special initiative for jury-based appeal resolution.
// This is used by other modules (e.g., x/forum) to create appeals that require jury review.
// initiativeType: type of appeal (e.g., "moderation_appeal", "sentinel_appeal")
// payload: JSON-encoded appeal data containing case details
// deadline: block height by which the appeal must be resolved
// Returns the appeal (initiative) ID or error.
func (k Keeper) CreateAppealInitiative(ctx context.Context, initiativeType string, payload []byte, deadline int64) (uint64, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get params: %w", err)
	}

	// Get next appeal ID (using JuryReview sequence since appeals are jury-resolved)
	appealID, err := k.JuryReviewSeq.Next(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get next appeal ID: %w", err)
	}

	// Create a special jury review for the appeal
	// Appeals don't have an initiative or challenge, so we use 0 for those fields
	juryReview := types.JuryReview{
		Id:                appealID,
		ChallengeId:       0, // No challenge - this is an external appeal
		InitiativeId:      0, // No initiative - this is an external appeal
		Jurors:            []string{},
		RequiredVotes:     uint32(params.JurySize),
		ExpertWitnesses:   []string{},
		Testimonies:       []*types.ExpertTestimony{},
		ReviewDeliverable: string(payload), // Store appeal payload
		ChallengerClaim:   initiativeType,  // Store appeal type
		AssigneeResponse:  "",
		Votes:             []*types.JurorVote{},
		Deadline:          deadline,
		Verdict:           types.Verdict_VERDICT_PENDING,
	}

	// For appeals, we'll select jurors when voting begins (deferred jury selection)
	// This allows time for the appeal to be reviewed before jury is selected

	// Save jury review
	if err := k.JuryReview.Set(ctx, juryReview.Id, juryReview); err != nil {
		return 0, fmt.Errorf("failed to save appeal jury review: %w", err)
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"appeal_initiative_created",
			sdk.NewAttribute("appeal_id", fmt.Sprintf("%d", appealID)),
			sdk.NewAttribute("type", initiativeType),
			sdk.NewAttribute("deadline", fmt.Sprintf("%d", deadline)),
		),
	)

	return appealID, nil
}
