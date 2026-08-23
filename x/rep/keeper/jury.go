package keeper

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/rand"
	"strings"

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

	// Select jury members, excluding the challenger so they can't sit on the jury
	// for their own challenge (the accused side is already excluded via
	// IsAffiliatedWithProject).
	jurors, err := k.SelectJury(ctx, initiative, params.JurySize, challenge.Challenger)
	if err != nil {
		return err
	}
	// A jury below the floor cannot return a verdict (TallyJuryVotes would hold
	// it at INCONCLUSIVE forever), so seating one would strand the challenge
	// until its deadline. Escalate to the committee instead — the same route a
	// wholly empty pool takes, which is why this reuses that error phrasing.
	if len(jurors) < types.MinSeatedJurors {
		return fmt.Errorf("insufficient eligible jurors: need %d, have %d",
			types.MinSeatedJurors, len(jurors))
	}

	// Calculate required votes (supermajority)
	superMajority := params.JurySuperMajority
	requiredVotes := superMajority.MulInt64(int64(len(jurors))).Ceil().TruncateInt().Uint64()

	// Get next jury review ID
	juryReviewID, err := k.JuryReviewSeq.Next(ctx)
	if err != nil {
		return err
	}

	if err := k.RecordJurySeating(ctx, jurors); err != nil {
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
		// Seats must be answered well before the vote deadline, so an unanswered
		// one can be redrawn while there is still time to review the work.
		AcceptanceDeadline: sdkCtx.BlockHeight() + k.juryAcceptanceWindowBlocks(params),
		Verdict:            types.Verdict_VERDICT_PENDING,
	}

	// Save jury review
	if err := k.JuryReview.Set(ctx, juryReview.Id, juryReview); err != nil {
		return err
	}
	// Index as PENDING so the deadline sweep can find it if jurors don't reach a
	// verdict via votes before the review's (block-height) deadline.
	if err := k.AddJuryReviewToJurorIndex(ctx, juryReview); err != nil {
		return err
	}

	if err := k.AddJuryReviewToVerdictIndex(ctx, juryReview); err != nil {
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
			// The seated addresses, so a juror's client can act on a plain event
			// filter instead of fetching every review to check whether it is
			// theirs. Jury duty pays StandardComplexityBudget and is easy to
			// miss; an unnoticed summons is the main cause of a lost quorum.
			sdk.NewAttribute("jurors", strings.Join(jurors, ",")),
			sdk.NewAttribute("deadline", fmt.Sprintf("%d", juryReview.Deadline)),
		),
	)

	return nil
}

// juryAcceptanceWindowBlocks derives the acceptance window from the review
// period rather than a fixed block count, so it scales with whatever epoch
// configuration a network runs. Clamped into [1, reviewPeriod-1]: the window
// has to be at least one block to be answerable at all, and strictly shorter
// than the review period or the sweep could never fire before the vote deadline
// — which is exactly what a fixed 1200-block constant did on short-period
// networks.
func (k Keeper) juryAcceptanceWindowBlocks(params types.Params) int64 {
	reviewPeriod := params.DefaultReviewPeriodEpochs * params.EpochBlocks
	if reviewPeriod <= 1 {
		return 1
	}
	window := params.JuryAcceptanceWindowRatio.MulInt64(reviewPeriod).TruncateInt64()
	if window < 1 {
		window = 1
	}
	if window >= reviewPeriod {
		window = reviewPeriod - 1
	}
	return window
}

// SelectJury selects jury members for a challenge. excludeAddrs are removed from
// the candidate pool in addition to members affiliated with the initiative —
// callers pass the challenger so a party to the dispute can never judge it
// (mirroring SelectContentJury and selectModerationAppealJury).
func (k Keeper) SelectJury(
	ctx context.Context,
	initiative types.Initiative,
	jurySize uint32,
	excludeAddrs ...string,
) ([]string, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	excludeSet := make(map[string]struct{}, len(excludeAddrs))
	for _, a := range excludeAddrs {
		if a != "" {
			excludeSet[a] = struct{}{}
		}
	}

	// Get all eligible members
	eligibleMembers := []types.Member{}
	minReputation := params.MinJurorReputation

	// Iterate through all members
	err = k.Member.Walk(ctx, nil, func(addr string, member types.Member) (stop bool, err error) {
		// Skip parties to the dispute (e.g. the challenger).
		if _, skip := excludeSet[addr]; skip {
			return false, nil
		}
		// Skip if affiliated with initiative (assignee / apprentice / author /
		// project creator — see InitiativeAffiliates).
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

	// Shrink to fit rather than refusing outright. This used to be all-or-nothing,
	// which meant a pool one juror short of jury_size produced *no* jury at all
	// and escalated to the committee — a worse outcome than a slightly smaller
	// jury, and a sharp cliff whenever jury_size is raised.
	//
	// Safe now that TallyJuryVotes floors quorum: a short jury cannot return a
	// rump verdict, so seating what is available costs nothing. It also matches
	// the moderation-appeal and content-challenge selection routines, which have
	// always seated what the pool allowed.
	//
	// Callers decide whether the result is enough. CreateJuryReview requires
	// MinSeatedJurors and escalates below it; replacement draws take whatever
	// they get. An empty pool is still an error, so the escalation path that
	// matches on this message keeps working.
	if len(eligibleMembers) == 0 {
		return nil, fmt.Errorf("insufficient eligible jurors: need %d, have 0", jurySize)
	}
	if len(eligibleMembers) < int(jurySize) {
		jurySize = uint32(len(eligibleMembers))
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
		// Weight by domain reputation, discounted by how often this member
		// actually answers a summons. This is what replaced timed exclusion:
		// a juror who never answers is drawn less often rather than removed, so
		// nobody loses eligibility, broad sortition survives, and an address can
		// always earn its weight back — which an excluded one never could,
		// having stopped being drawn at all.
		weights[i] = totalRep.MustFloat64() * k.JurorResponsivenessWeight(ctx, member.Address)
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

// selectModerationAppealJury selects jurors for a moderation appeal. Unlike
// SelectJury it is not scoped to an initiative's tags (an appeal has no
// initiative): eligibility is "member with any tag reputation >=
// MinJurorReputation", weight is the member's total reputation across all tags,
// and the parties to the dispute (appellant, accused/sentinel, action target)
// are excluded. Selection is best-effort: if fewer than JurySize eligible
// members exist, it returns as many as it can (possibly zero), so appeal
// creation never fails for lack of jurors — such appeals fall back to committee
// resolution (MsgResolveGovActionAppeal) or timeout.
//
// Deterministic: seeded from the block AppHash XOR the jury review id so all
// validators select identically.
func (k Keeper) selectModerationAppealJury(ctx context.Context, juryReviewID uint64, excludes []string) ([]string, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	minReputation := params.MinJurorReputation

	excludeSet := make(map[string]struct{}, len(excludes))
	for _, e := range excludes {
		if e != "" {
			excludeSet[e] = struct{}{}
		}
	}

	var (
		eligible []string
		weights  []float64
	)
	err = k.Member.Walk(ctx, nil, func(addr string, member types.Member) (stop bool, err error) {
		if _, skip := excludeSet[addr]; skip {
			return false, nil
		}
		qualifies := false
		totalRep := math.LegacyZeroDec()
		for _, scoreStr := range member.ReputationScores {
			score, perr := math.LegacyNewDecFromStr(scoreStr)
			if perr != nil {
				continue
			}
			totalRep = totalRep.Add(score)
			if score.GTE(minReputation) {
				qualifies = true
			}
		}
		if qualifies {
			eligible = append(eligible, addr)
			weights = append(weights, totalRep.MustFloat64()*k.JurorResponsivenessWeight(ctx, addr))
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	size := int(params.JurySize)
	if len(eligible) < size {
		size = len(eligible)
	}
	if size == 0 {
		return []string{}, nil
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	appHash := sdkCtx.BlockHeader().AppHash
	var seed int64
	if len(appHash) >= 8 {
		seed = int64(binary.BigEndian.Uint64(appHash[:8])) ^ int64(juryReviewID)
	} else {
		seed = int64(juryReviewID) ^ sdkCtx.BlockHeight()
	}
	rng := rand.New(rand.NewSource(seed))

	selected := make([]string, 0, size)
	for i := 0; i < size; i++ {
		idx := weightedRandomSelect(rng, weights)
		selected = append(selected, eligible[idx])
		eligible = append(eligible[:idx], eligible[idx+1:]...)
		weights = append(weights[:idx], weights[idx+1:]...)
	}
	return selected, nil
}

// appealRequiredVotes returns the supermajority vote count required to resolve
// an appeal seated with jurySize jurors (ceil(JurySuperMajority * jurySize)),
// minimum 1. Mirrors the supermajority math used for challenge jury reviews.
func (k Keeper) appealRequiredVotes(ctx context.Context, jurySize int) uint64 {
	params, err := k.Params.Get(ctx)
	if err != nil || jurySize <= 0 {
		return 1
	}
	req := params.JurySuperMajority.MulInt64(int64(jurySize)).Ceil().TruncateInt().Uint64()
	if req == 0 {
		req = 1
	}
	return req
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

	// Only PENDING reviews accept votes. A review that already carries a
	// verdict — tallied normally, or closed INCONCLUSIVE because its challenge
	// was voided (parent project cancelled) — is finished; without this guard a
	// late vote could re-trigger TallyJuryVotes and resolve a voided challenge.
	if juryReview.Verdict != types.Verdict_VERDICT_PENDING {
		return fmt.Errorf("jury review %d is already resolved (verdict %s)", juryReviewID, juryReview.Verdict.String())
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

	// A juror's per-item verdicts must answer criteria the initiative actually
	// declared. Until acceptance_criteria existed there was nothing to resolve
	// these ids against, which is what left CriteriaVote decorative.
	// InitiativeId is zero for content challenges and moderation appeals, which
	// have no initiative and therefore no criteria to answer.
	if len(criteriaVotes) > 0 && juryReview.InitiativeId != 0 {
		initiative, iErr := k.GetInitiative(ctx, juryReview.InitiativeId)
		if iErr != nil {
			return iErr
		}
		if err := ValidateCriteriaVotes(initiative, criteriaVotes); err != nil {
			return err
		}
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

	// Credit participation before any tally runs. The vote that reaches the
	// supermajority triggers TallyJuryVotes inline, and the tally charges every
	// seated juror who has not voted — so crediting afterwards would book the
	// deciding juror as a no-show on their own vote.
	if err := k.RecordJuryVote(ctx, jurorAddrStr); err != nil {
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

	// Quorum guard: a tally is only decisive if a majority of the SEATED jury
	// actually voted. Below that bar — including zero votes — the result is
	// INCONCLUSIVE, so neither juror inaction nor a tiny minority can resolve a
	// dispute. The vote-triggered path always clears this bar (it fires at the
	// supermajority RequiredVotes); the guard matters for deadline tallies
	// (ResolveExpiredChallengeJuryReviews for challenges, TimeoutExpiredAppeals
	// for appeals), where participation may be partial.
	quorum := len(juryReview.Jurors)/2 + 1
	// Floor the decision threshold, never the roster. Quorum is computed on the
	// seated list, so a jury thinned by declines or vacancies lowers its own bar
	// to decide — at one seated juror the quorum is one, and that juror alone
	// satisfies `rejectVotes > totalVotes/2`, rejecting a challenge and burning
	// the challenger's stake single-handedly.
	//
	// The fix is not to refuse the departure. Jurors are conscripted by
	// sortition and handing a seat back has to stay free, or the abandoned-seat
	// penalty loses its justification and honest jurors are pushed into silence
	// (which costs them selection weight, where a decline does not). Instead a
	// roster below MinSeatedJurors loses the power to decide: it cannot reach
	// quorum, returns INCONCLUSIVE, and takes the terminal path its review type
	// already has — adjudication interim for initiative challenges,
	// ResolveInconclusiveContentChallenge for content, TimeoutExpiredAppeals for
	// appeals. No verdict is ever binding on fewer than two concurring jurors.
	if minQuorum := types.MinSeatedJurors/2 + 1; quorum < minQuorum {
		quorum = minQuorum
	}
	var finalVerdict types.Verdict
	switch {
	case totalVotes < quorum:
		finalVerdict = types.Verdict_VERDICT_INCONCLUSIVE
	case upholdVotes >= int(requiredSupermajority):
		finalVerdict = types.Verdict_VERDICT_UPHOLD_CHALLENGE
	case rejectVotes > totalVotes/2:
		finalVerdict = types.Verdict_VERDICT_REJECT_CHALLENGE
	default:
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

	// Charge the jurors who never voted. This is the one point every resolution
	// path passes through — the supermajority-triggered tally, the challenge
	// deadline sweep, and the appeal timeout all land here — so the accounting
	// cannot be forgotten by a future caller, and it runs exactly once per
	// review. Deliberately placed before the resolution dispatch below, which
	// returns early on several branches.
	if err := k.RecordJuryNoShows(ctx, juryReview); err != nil {
		return err
	}

	// Moderation-appeal resolution (dispatched on the appeal-type string stored
	// in ChallengerClaim). Appeals carry no ContentChallengeId/ChallengeId, so
	// this must intercept before the challenge-resolution paths below.
	//
	//   UPHOLD_CHALLENGE  -> appellant (the challenger) wins -> action OVERTURNED
	//   REJECT_CHALLENGE  -> appellant loses               -> action UPHELD
	//   INCONCLUSIVE      -> leave the appeal PENDING; TimeoutExpiredAppeals
	//                        gives it a terminal TIMEOUT (no party penalized).
	if juryReview.ChallengerClaim == govActionAppealInitiativeType {
		if appeal, ok := k.findGovActionAppealByInitiative(ctx, juryReview.Id); ok {
			var verdict types.GovAppealStatus
			switch finalVerdict {
			case types.Verdict_VERDICT_UPHOLD_CHALLENGE:
				verdict = types.GovAppealStatus_GOV_APPEAL_STATUS_OVERTURNED
			case types.Verdict_VERDICT_REJECT_CHALLENGE:
				verdict = types.GovAppealStatus_GOV_APPEAL_STATUS_UPHELD
			default:
				verdict = types.GovAppealStatus_GOV_APPEAL_STATUS_UNSPECIFIED
			}
			if verdict != types.GovAppealStatus_GOV_APPEAL_STATUS_UNSPECIFIED {
				if err := k.applyGovActionAppealVerdict(ctx, appeal.Id, verdict,
					fmt.Sprintf("jury:%d", juryReview.Id), "jury verdict"); err != nil {
					sdk.UnwrapSDKContext(ctx).Logger().Error(
						"failed to apply jury verdict to gov action appeal",
						"appeal_id", appeal.Id, "jury_review_id", juryReview.Id, "error", err)
				}
			}
		}
		// Reward jurors for participating, then stop — appeals are not challenges.
		return k.RewardJurors(ctx, juryReview)
	}

	// Initiative-challenge review resolved: move it out of the PENDING verdict
	// index so the deadline sweep (ResolveExpiredChallengeJuryReviews) stops
	// revisiting it. ONLY initiative challenges are indexed: appeals returned
	// above, and content challenges are deliberately NOT in the sweep (they have
	// their own response-deadline path and resolving them at the short jury
	// deadline regressed e2e behavior — see content_challenge.go).
	if juryReview.ContentChallengeId == 0 {
		if err := k.UpdateJuryReviewVerdictIndex(ctx, types.Verdict_VERDICT_PENDING, finalVerdict, juryReview.Id); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("failed to update jury verdict index",
				"review_id", juryReview.Id, "error", err)
		}
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

// jurorRewardPerSeat is what one juror is paid for voting on a review.
//
// Scaled to the amount in dispute, split across the seats: settling a dispute
// should cost a fraction of what is in dispute, not several times it. Paying a
// flat StandardComplexityBudget meant a challenge over a 100 DREAM APPRENTICE
// initiative minted 750 DREAM in juror fees.
//
// It is a fixed per-seat share rather than a pool split among whoever turned
// up, so a juror never earns more because their colleagues stayed home — the
// unclaimed shares are simply never minted.
//
// Content challenges and moderation appeals carry no initiative budget to scale
// against and fall back to the floor.
func (k Keeper) jurorRewardPerSeat(ctx context.Context, juryReview types.JuryReview) math.Int {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return math.NewInt(types.DefaultMinJurorReward)
	}
	floor := params.MinJurorReward
	if floor.IsNil() {
		floor = math.NewInt(types.DefaultMinJurorReward)
	}

	seats := int64(len(juryReview.Jurors))
	if seats <= 0 {
		return floor
	}

	rate := params.JurorRewardRate
	if rate.IsNil() || !rate.IsPositive() {
		return floor
	}

	if juryReview.InitiativeId == 0 {
		return floor
	}
	initiative, err := k.GetInitiative(ctx, juryReview.InitiativeId)
	if err != nil {
		return floor
	}

	perSeat := DerefInt(initiative.Budget).ToLegacyDec().Mul(rate).QuoInt64(seats).TruncateInt()
	if perSeat.LT(floor) {
		return floor
	}
	// Never pay more per juror than a standard piece of committee work; a very
	// large dispute should not mint an unbounded jury fee.
	if perSeat.GT(params.StandardComplexityBudget) {
		return params.StandardComplexityBudget
	}
	return perSeat
}

// RewardJurors rewards jurors for their participation
func (k Keeper) RewardJurors(ctx context.Context, juryReview types.JuryReview) error {
	reward := k.jurorRewardPerSeat(ctx, juryReview)

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
			if err := k.MintDREAM(ctx, jurorAddr, reward); err != nil {
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

// maxJuryDeadlinesPerBlock bounds the per-block deadline sweep work.
const maxJuryDeadlinesPerBlock = 50

// ResolveExpiredChallengeJuryReviews tallies INITIATIVE-challenge jury reviews
// whose (block-height) deadline has passed but which never reached a verdict by
// votes. It walks the PENDING verdict index (which only ever contains
// initiative-challenge reviews — appeals and content challenges are not indexed),
// collects the due ids, then tallies each: TallyJuryVotes applies the quorum +
// supermajority rules, resolves the underlying challenge (or escalates on
// INCONCLUSIVE), and moves the review out of the PENDING index.
//
// Appeals (timestamp deadline — see TimeoutExpiredAppeals) and content
// challenges (own response-deadline path — see content_challenge.go) are
// deliberately NOT handled here.
func (k Keeper) ResolveExpiredChallengeJuryReviews(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	height := sdkCtx.BlockHeight()

	// Collect first, then tally — TallyJuryVotes mutates the PENDING index, so we
	// must not tally while walking it.
	var due []uint64
	k.IterateActiveJuryReviews(ctx, func(_ int64, review types.JuryReview) bool {
		if len(due) >= maxJuryDeadlinesPerBlock {
			return true // stop
		}
		if review.Deadline > 0 && height >= review.Deadline {
			due = append(due, review.Id)
		}
		return false
	})

	for _, id := range due {
		if err := k.TallyJuryVotes(ctx, id); err != nil {
			sdkCtx.Logger().Error("failed to tally expired jury review", "review_id", id, "error", err)
		}
	}
	return nil
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
