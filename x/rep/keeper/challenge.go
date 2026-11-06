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
	criteriaID ...string,
) (uint64, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0, err
	}
	if err := validateEvidence(evidence); err != nil {
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

	// The assignee cannot challenge their own work: an assignee who has
	// decided to fail could admit fault, win their own challenge, and pocket
	// the challenger reward while only losing reputation they can re-earn.
	// Checked before the stake is locked so a self-challenge costs nothing.
	if k.SameMember(ctx, initiative.Assignee, challengerAddr.String()) {
		return 0, types.ErrSelfChallenge
	}

	// A cited criterion must be one the author pre-committed to. Checked before
	// the stake is locked so a typo costs the challenger nothing.
	citedCriterion := ""
	if len(criteriaID) > 0 {
		citedCriterion = criteriaID[0]
	}
	if err := ValidateCriteriaCitation(initiative, citedCriterion); err != nil {
		return 0, err
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

	// Create challenge. The pre-challenge status is snapshotted so a rejection
	// can put the initiative back exactly where it was: challenges are legal on
	// SUBMITTED work, whose challenge window has not opened yet, and a rejected
	// challenge must not skip that window (see RejectChallenge).
	challenge := types.Challenge{
		Id:                    challengeID,
		InitiativeId:          initiativeID,
		Challenger:            challengerAddr.String(),
		Reason:                reason,
		Evidence:              evidence,
		StakedDream:           PtrInt(stakedDream),
		Status:                types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE,
		CreatedAt:             sdkCtx.BlockHeight(),
		ResponseDeadline:      responseDeadline,
		CriteriaId:            citedCriterion,
		StatusBeforeChallenge: initiative.Status,
	}

	// Save challenge
	if err := k.Challenge.Set(ctx, challengeID, challenge); err != nil {
		return 0, err
	}

	// Add to status index for efficient EndBlocker lookups
	if err := k.AddChallengeToStatusIndex(ctx, challenge); err != nil {
		return 0, fmt.Errorf("failed to add challenge to status index: %w", err)
	}

	// Update initiative status to challenged (UpdateInitiative maintains the
	// by-status index, keeping the SUBMITTED bucket free of challenged work)
	initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_CHALLENGED
	if err := k.UpdateInitiative(ctx, initiative); err != nil {
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
	if err := validateEvidence(evidence); err != nil {
		return err
	}

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
	if !k.CanActAs(ctx, responderAddr.String(), initiative.Assignee) {
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
		// The assignee conceded: no verdict was reached, so the challenger's
		// reward is limited to what the assignee forfeits (REP-E-04).
		return k.UpholdConcededChallenge(ctx, challengeID)
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

// UpholdChallenge upholds a challenge on a jury or committee verdict and
// slashes the assignee.
func (k Keeper) UpholdChallenge(ctx context.Context, challengeID uint64) error {
	return k.upholdChallenge(ctx, challengeID, false)
}

// UpholdConcededChallenge upholds a challenge the assignee conceded (an empty
// response, or no response by the deadline). No one judged the work, so the
// challenger's reward is capped at the self-assign bond the assignee forfeits
// (REP-E-04): a minted reward on a default let two accounts farm 20% of a
// self-declared budget, the assignee simply never answering its partner's
// challenge.
func (k Keeper) UpholdConcededChallenge(ctx context.Context, challengeID uint64) error {
	return k.upholdChallenge(ctx, challengeID, true)
}

// challengerReward is the DREAM minted to a winning challenger:
// budget x challenger_reward_rate, then
//   - nothing when the challenger is an insider of the work (assignee,
//     apprentice, author, project creator) or invited, or was invited by, the
//     assignee: a challenge between related accounts earns no mint;
//   - at most the forfeited self-assign bond when the challenge was conceded
//     rather than decided;
//   - at most what is left of max_initiative_rewards_per_season, which the
//     reward is booked against like every other initiative mint.
func (k Keeper) challengerReward(ctx context.Context, params types.Params, initiative types.Initiative, challenger string, conceded bool) (math.Int, error) {
	reward := DerefInt(initiative.Budget).ToLegacyDec().Mul(params.ChallengerRewardRate).TruncateInt()
	if !reward.IsPositive() {
		return math.ZeroInt(), nil
	}
	if k.IsAffiliatedWithProject(ctx, initiative, challenger) || k.invitationNeighbours(ctx, challenger, initiative.Assignee) {
		return math.ZeroInt(), nil
	}
	if conceded {
		reward = math.MinInt(reward, DerefInt(initiative.SelfAssignBond))
	}
	if !reward.IsPositive() {
		return math.ZeroInt(), nil
	}
	minted, err := k.GetSeasonInitiativeRewardsMinted(ctx)
	if err != nil {
		return math.ZeroInt(), err
	}
	headroom := params.MaxInitiativeRewardsPerSeason.Sub(minted)
	if !headroom.IsPositive() {
		return math.ZeroInt(), nil
	}
	return math.MinInt(reward, headroom), nil
}

// invitationNeighbours reports whether one of two members invited the other.
func (k Keeper) invitationNeighbours(ctx context.Context, a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if ma, err := k.Member.Get(ctx, k.Canonical(ctx, a)); err == nil && ma.InvitedBy != "" && k.SameMember(ctx, ma.InvitedBy, b) {
		return true
	}
	if mb, err := k.Member.Get(ctx, k.Canonical(ctx, b)); err == nil && mb.InvitedBy != "" && k.SameMember(ctx, mb.InvitedBy, a) {
		return true
	}
	return false
}

func (k Keeper) upholdChallenge(ctx context.Context, challengeID uint64, conceded bool) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	challenge, err := k.GetChallenge(ctx, challengeID)
	if err != nil {
		return err
	}

	// Only an unresolved challenge can be upheld. Guards against resolving a
	// VOIDED challenge (parent project cancelled — stake already refunded) or
	// double-resolving an UPHELD/REJECTED one: either would corrupt the
	// challenger's lock accounting and resurrect a terminal initiative.
	if challenge.Status != types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE &&
		challenge.Status != types.ChallengeStatus_CHALLENGE_STATUS_IN_JURY_REVIEW {
		return fmt.Errorf("challenge %d is already resolved (status %s)", challengeID, challenge.Status.String())
	}

	initiative, err := k.GetInitiative(ctx, challenge.InitiativeId)
	if err != nil {
		return err
	}

	// The jury has contradicted the reviewers who passed this work. Charge the
	// approvers their committed bond and record the outcome against every
	// verdict on the round, so the accuracy ring reflects who was right as well
	// as who was wrong. Reviewers who voted to reject are credited.
	if err := k.SlashReviewersOnOverturn(ctx, initiative.Id, initiative.ReviewRound, true); err != nil {
		return fmt.Errorf("failed to settle reviewer bonds on upheld challenge: %w", err)
	}

	// Settle the review bounty. REJECTED is terminal, and without this the
	// funders' DREAM stayed locked for good: a verdict on the round commits the
	// bounty, which bars reclaim. Paid per verdict filed like on every other
	// terminal path — slashing, above, is what answers a wrong verdict.
	if _, err := k.PayReviewBounty(ctx, initiative); err != nil {
		return fmt.Errorf("failed to settle review bounty on upheld challenge: %w", err)
	}
	// The work failed, so the initiative bounty goes back whole.
	if err := k.RefundInitiativeBounty(ctx, initiative.Id, "challenge upheld"); err != nil {
		return fmt.Errorf("failed to refund initiative bounty on upheld challenge: %w", err)
	}
	// The fee is paid per verdict filed on this path too. Withholding it here
	// made the fee contingent on nobody later challenging the work, and cost
	// the reviewers who rejected it — the ones the jury just proved right —
	// as much as those who approved it. The approvers are answered by the
	// slash above, which outweighs their share of the fee.
	if _, err := k.PayReviewFees(ctx, initiative); err != nil {
		return fmt.Errorf("failed to pay review fees on upheld challenge: %w", err)
	}
	// Every round's fee is charged to the returned budget below, including
	// rounds a rejection sent back (paid when they resolved, REP-E-22).
	reviewFees, err := k.ReviewFeesPaid(ctx, initiative.Id)
	if err != nil {
		return fmt.Errorf("failed to total review fees on upheld challenge: %w", err)
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
	rewardAmount, err := k.challengerReward(ctx, params, initiative, challenge.Challenger, conceded)
	if err != nil {
		return err
	}

	// Unlock the staked DREAM (locked from challengerAddr)
	if stakedAmount.IsPositive() {
		if err := k.UnlockDREAM(ctx, challengerAddr, stakedAmount); err != nil {
			return err
		}
	}

	// Mint the reward amount (new DREAM for the challenger), booked against
	// the season's initiative reward cap.
	if rewardAmount.IsPositive() {
		if err := k.MintDREAM(ctx, challengerAddr, rewardAmount); err != nil {
			return err
		}
		if err := k.TrackInitiativeRewardMint(ctx, rewardAmount); err != nil {
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
	k.notifySeason(ctx, "OnChallengeResolved", func(sk types.SeasonKeeper) error {
		return sk.OnChallengeResolved(ctx, challengerAddr, assigneeAddr, true)
	})

	// Update status index
	if err := k.UpdateChallengeStatusIndex(ctx, oldStatus, challenge.Status, challenge.Id); err != nil {
		return err
	}

	// Burn any self-assign bond — the upheld challenge is exactly the
	// failure mode the bond guards against.
	if err := k.BurnSelfAssignBond(ctx, &initiative); err != nil {
		return err
	}

	// Harvest what the stakes accrued while the work was live and return their
	// principal, BEFORE the flip below — stakeAccruing stops paying on a
	// terminal initiative, so settling after it would silently strand every
	// accrued reward. The rejection does not unearn rewards from the period the
	// position was live; slashing is the mechanism for punishing the stake
	// itself, and it is not applied here. The same reasoning returns the
	// principal: the upheld challenge burns the assignee's bond, not the DREAM
	// of the members who backed the work.
	if err := k.releaseInitiativeStakes(ctx, initiative.Id, types.InitiativeStatus_INITIATIVE_STATUS_REJECTED); err != nil {
		return err
	}

	// Update initiative status to rejected (failed challenge). UpdateInitiative
	// maintains the by-status index so the terminal initiative leaves the
	// CHALLENGED bucket.
	initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_REJECTED
	if err := k.UpdateInitiative(ctx, initiative); err != nil {
		return err
	}

	// Return unspent budget to project (skip for permissionless — no pre-allocated
	// budget), net of what review cost, as in CloseInitiative: the project
	// asked for the work to be evaluated and it was.
	project, projErr := k.GetProject(ctx, initiative.ProjectId)
	if projErr == nil && !project.Permissionless {
		returned := DerefInt(initiative.Budget).Sub(reviewFees)
		if returned.IsNegative() {
			returned = math.ZeroInt()
		}
		if err := k.ReturnBudget(ctx, initiative.ProjectId, returned); err != nil {
			return err
		}
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"challenge_upheld",
			sdk.NewAttribute("challenge_id", fmt.Sprintf("%d", challengeID)),
			sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", challenge.InitiativeId)),
			sdk.NewAttribute("challenger_reward", rewardAmount.String()),
			sdk.NewAttribute("conceded", fmt.Sprintf("%t", conceded)),
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

	// Only an unresolved challenge can be rejected — see the matching guard in
	// UpholdChallenge. Without it a late jury tally on a VOIDED challenge would
	// burn the challenger's already-refunded stake (confiscation without a
	// verdict) and flip a CANCELLED initiative back to IN_REVIEW.
	if challenge.Status != types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE &&
		challenge.Status != types.ChallengeStatus_CHALLENGE_STATUS_IN_JURY_REVIEW {
		return fmt.Errorf("challenge %d is already resolved (status %s)", challengeID, challenge.Status.String())
	}

	initiative, err := k.GetInitiative(ctx, challenge.InitiativeId)
	if err != nil {
		return err
	}

	// The jury agreed with the reviewers who passed this work. Credit their
	// accuracy record; their bond stays committed until the initiative itself
	// resolves, since a later challenge could still land.
	if err := k.RecordReviewRoundUpheld(ctx, initiative.Id, initiative.ReviewRound); err != nil {
		return fmt.Errorf("failed to credit reviewers on rejected challenge: %w", err)
	}

	// Slash the challenger's stake: burn the lock itself, so the burn cannot
	// be paid out of another escrow and the locked total stays backed.
	challengerAddr, err := sdk.AccAddressFromBech32(challenge.Challenger)
	if err != nil {
		return err
	}
	stakedAmount := DerefInt(challenge.StakedDream)
	if stakedAmount.IsPositive() {
		if err := k.BurnLockedDREAM(ctx, challengerAddr, stakedAmount); err != nil {
			return err
		}
	}

	// Update challenge status
	oldStatus := challenge.Status
	challenge.Status = types.ChallengeStatus_CHALLENGE_STATUS_REJECTED
	challenge.ResolvedAt = sdkCtx.BlockHeight()
	if err := k.Challenge.Set(ctx, challenge.Id, challenge); err != nil {
		return err
	}

	// Update status index
	if err := k.UpdateChallengeStatusIndex(ctx, oldStatus, challenge.Status, challenge.Id); err != nil {
		return err
	}

	// Restore the initiative to the status it held when the challenge was
	// filed, snapshotted at creation.
	//
	// A challenge filed on IN_REVIEW work restores IN_REVIEW: its challenge
	// window has already run, and the work is valid and ready for completion.
	//
	// A challenge filed on SUBMITTED work restores SUBMITTED — NOT IN_REVIEW.
	// SUBMITTED work has ChallengePeriodEnd 0 (only TransitionToChallengePeriod
	// sets it), so flipping it to IN_REVIEW made the completion sweep's
	// `height >= ChallengePeriodEnd` check vacuously true and paid out in the
	// next block, skipping the entire review + challenge window the completion
	// guard promises. Restoring SUBMITTED hands the initiative back to the
	// ordinary transition, which opens a real window. The snapshot's zero value
	// (OPEN, on chains predating the field) restores IN_REVIEW like the
	// historical behaviour.
	restore := types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW
	if challenge.StatusBeforeChallenge == types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED {
		restore = types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED
	}
	initiative.Status = restore
	if err := k.UpdateInitiative(ctx, initiative); err != nil {
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

// voidActiveChallengesForInitiative terminates every unresolved challenge on
// an initiative without a verdict, refunding each challenger's stake in full.
// Used when the initiative is discarded out from under the dispute — i.e. its
// parent project is cancelled — so neither party is judged: the challenger is
// made whole (no burn, no reward) and any pending jury review is closed
// INCONCLUSIVE so the EndBlocker never tallies a verdict on a dead initiative.
func (k Keeper) voidActiveChallengesForInitiative(ctx context.Context, initiativeID uint64) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Collect first: the callbacks below mutate the status index we walk.
	var toVoid []types.Challenge
	activeStatuses := []types.ChallengeStatus{
		types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE,
		types.ChallengeStatus_CHALLENGE_STATUS_IN_JURY_REVIEW,
	}
	for _, status := range activeStatuses {
		err := k.IterateChallengesByStatus(ctx, status, func(_ uint64, challenge types.Challenge) bool {
			if challenge.InitiativeId == initiativeID {
				toVoid = append(toVoid, challenge)
			}
			return false
		})
		if err != nil {
			return err
		}
	}

	for _, challenge := range toVoid {
		// Refund the challenger's stake in full — the dispute was never
		// adjudicated, so it is neither won (reward) nor lost (burn).
		challengerAddr, err := sdk.AccAddressFromBech32(challenge.Challenger)
		if err != nil {
			return err
		}
		if err := k.UnlockDREAM(ctx, challengerAddr, DerefInt(challenge.StakedDream)); err != nil {
			return err
		}

		oldStatus := challenge.Status
		challenge.Status = types.ChallengeStatus_CHALLENGE_STATUS_VOIDED
		challenge.ResolvedAt = sdkCtx.BlockHeight()
		if err := k.Challenge.Set(ctx, challenge.Id, challenge); err != nil {
			return err
		}
		if err := k.UpdateChallengeStatusIndex(ctx, oldStatus, challenge.Status, challenge.Id); err != nil {
			return err
		}

		// Close any pending jury review for this challenge so the EndBlocker
		// jury resolver skips it. Jurors are rewarded only at tally time, so
		// no per-juror state needs unwinding here.
		if err := k.voidPendingJuryReviewForChallenge(ctx, challenge.Id, "voided: parent project cancelled"); err != nil {
			return err
		}

		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				"challenge_voided",
				sdk.NewAttribute("challenge_id", fmt.Sprintf("%d", challenge.Id)),
				sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiativeID)),
				sdk.NewAttribute("refunded_stake", DerefInt(challenge.StakedDream).String()),
			),
		)
	}

	return nil
}

// voidPendingJuryReviewForChallenge closes every PENDING jury review for a
// challenge being voided, moving each to an INCONCLUSIVE verdict and off the
// PENDING verdict index so the EndBlocker resolver no longer tallies them.
// Normally at most one review exists per challenge, but voiding all matches
// keeps the cleanup complete even if that invariant ever slips.
func (k Keeper) voidPendingJuryReviewForChallenge(ctx context.Context, challengeID uint64, reason string) error {
	// Collect first: the writes below mutate the PENDING index being walked.
	var targets []types.JuryReview
	k.IterateActiveJuryReviews(ctx, func(_ int64, review types.JuryReview) bool {
		// External cases carry ChallengeId 0 but belong to no challenge.
		if review.ChallengeId == challengeID && !isExternalJuryCase(review) {
			targets = append(targets, review)
		}
		return false
	})

	for _, target := range targets {
		oldVerdict := target.Verdict
		target.Verdict = types.Verdict_VERDICT_INCONCLUSIVE
		target.Reasoning = reason
		if err := k.JuryReview.Set(ctx, target.Id, target); err != nil {
			return err
		}
		// A voided review pays no expert witness.
		k.settleExpertTestimonies(ctx, target, false)
		if err := k.UpdateJuryReviewVerdictIndex(ctx, oldVerdict, target.Verdict, target.Id); err != nil {
			return err
		}
	}
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
	if err := k.UpdateChallengeStatusIndex(ctx, oldStatus, challenge.Status, challenge.Id); err != nil {
		return err
	}

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
