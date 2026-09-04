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

// Initiative review: the quality gate conviction cannot be.
//
// Conviction measures whether people wanted the work done, not whether it was
// done. Acceptance criteria gave a challenger a standard to cite; they gave
// nobody a reason to look. Stakers are disqualified from judging twice over —
// they are paid on completion, and backing a proposal is a different judgement
// from evaluating a deliverable, made earlier and often without the expertise.
// A lot-drawn jury per submission would conscript reviewers for undisputed work.
//
// So reviewing is a third role that does nothing else: bonded, accuracy
// measured, paid per verdict filed and never per approval.

// reviewFeeRate, reviewerBondReserveRate and maxReviewRounds read their params
// with a fallback, so a chain whose stored params predate the reviewer role
// degrades to the shipped defaults instead of panicking on a nil Dec.
func (k Keeper) reviewerBondReserveRate(params types.Params) math.LegacyDec {
	if params.ReviewerBondReserveRate.IsNil() || !params.ReviewerBondReserveRate.IsPositive() {
		return math.LegacyNewDecWithPrec(1, 1)
	}
	return params.ReviewerBondReserveRate
}

func (k Keeper) reviewFeeRate(params types.Params) math.LegacyDec {
	if params.ReviewFeeRate.IsNil() || params.ReviewFeeRate.IsNegative() {
		return math.LegacyNewDecWithPrec(5, 2)
	}
	return params.ReviewFeeRate
}

func (k Keeper) maxReviewRounds(params types.Params) uint32 {
	if params.MaxReviewRounds == 0 {
		return 3
	}
	return params.MaxReviewRounds
}

// TierConfigFor resolves the per-tier economics for an initiative. The tier
// switch existed inline in three places already; reviewer pay is the fourth
// caller, so it is worth having once.
func TierConfigFor(params types.Params, tier types.InitiativeTier) types.TierConfig {
	switch tier {
	case types.InitiativeTier_INITIATIVE_TIER_APPRENTICE:
		return params.ApprenticeTier
	case types.InitiativeTier_INITIATIVE_TIER_EXPERT:
		return params.ExpertTier
	case types.InitiativeTier_INITIATIVE_TIER_EPIC:
		return params.EpicTier
	default:
		return params.StandardTier
	}
}

// ReviewRequired reports whether the parent project asks for reviewer sign-off
// at all. min_verifier_count 0 is the genesis default and means conviction-only
// — exactly the behaviour that predates this role, so nothing wedges while the
// reviewer roster is still filling.
func ReviewRequired(params types.Params, initiative types.Initiative, project types.Project) bool {
	return RequiredVerifiersFor(params, initiative, project) > 0
}

// RequiredVerifiersFor is how many approving verdicts this initiative needs.
//
// The MAXIMUM of two independent sources, deliberately:
//
//   - the per-project policy, read from the initiative's own snapshot. The
//     project creator owns that policy and — for self-assigned work — is also
//     the party the gate constrains, so reading it live would let them switch
//     the gate off over work already submitted. As a snapshot it is a floor.
//
//   - the chain-wide review_required_above_budget threshold, read LIVE. Its
//     setter is governance or the Operations Committee, not the party being
//     constrained, so the argument above does not apply — and snapshotting it
//     would mean a committee raising the threshold to respond to a farm in
//     progress could not touch anything already submitted.
//
// Taking the max means neither source can be used to weaken the other: policy
// cannot be relaxed after submission, and the threshold cannot be dodged by
// a project declaring no policy.
func RequiredVerifiersFor(params types.Params, initiative types.Initiative, project types.Project) uint32 {
	required := initiative.RequiredVerifiers
	// A snapshot of 0 on an initiative created before any policy existed still
	// gets the live threshold applied below.
	if project.VerificationPolicy != nil && project.VerificationPolicy.MinVerifierCount > required {
		// Only when the initiative predates its own snapshot being written.
		if initiative.RequiredVerifiers == 0 {
			required = project.VerificationPolicy.MinVerifierCount
		}
	}
	threshold := params.ReviewRequiredAboveBudget
	if !threshold.IsNil() && threshold.IsPositive() && required < 1 {
		if budget := DerefInt(initiative.Budget); !budget.IsNil() && budget.GT(threshold) {
			required = 1
		}
	}
	return required
}

// ReviewerBondForInitiative is what a verdict on this initiative costs in
// committed bond, and what an overturn slashes.
//
// Scaled to the budget rather than flat: a wrong approval on an EPIC initiative
// releases up to 10,000 DREAM and the same risk should not attach to a 100 DREAM
// one. It also self-limits — a reviewer can only hold as many open verdicts as
// their free bond covers.
func (k Keeper) ReviewerBondForInitiative(ctx context.Context, initiative types.Initiative) (math.Int, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return math.ZeroInt(), err
	}
	budget := DerefInt(initiative.Budget)
	if budget.IsNil() || !budget.IsPositive() {
		return math.ZeroInt(), nil
	}
	return k.reviewerBondReserveRate(params).MulInt(budget).TruncateInt(), nil
}

// QualifiedReviewer checks that an address may file a verdict on this
// initiative. Returns a descriptive error rather than a bool so the message
// server can tell the caller which gate they failed.
func (k Keeper) QualifiedReviewer(ctx context.Context, initiative types.Initiative, project types.Project, reviewer string) error {
	role, err := k.GetBondedRole(ctx, types.RoleType_ROLE_TYPE_INITIATIVE_REVIEWER, reviewer)
	if err != nil {
		return fmt.Errorf("%w: %s does not hold the initiative-reviewer role", types.ErrUnauthorized, reviewer)
	}
	if role.BondStatus != types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL {
		// RECOVERY, UNBONDING and DEMOTED all mean the bond is not backing new
		// liability, which is exactly what a verdict creates.
		return fmt.Errorf("%w: reviewer role is %s, not NORMAL", types.ErrUnauthorized, role.BondStatus)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if cfg, cErr := k.GetBondedRoleConfig(ctx, types.RoleType_ROLE_TYPE_INITIATIVE_REVIEWER); cErr == nil &&
		cfg.MinAgeBlocks > 0 && sdkCtx.BlockHeight()-role.RegisteredAt < cfg.MinAgeBlocks {
		return fmt.Errorf("%w: reviewer role must be held for %d blocks before reviewing",
			types.ErrUnauthorized, cfg.MinAgeBlocks)
	}

	// Independence. The same affiliates-plus-one-invitation-hop test that gates
	// external conviction, reused rather than reinvented: an insider judging
	// their own commission is the conflict this role exists to remove.
	affiliates := k.InvitationNeighborhoodOf(ctx, InitiativeAffiliates(initiative, project)...)
	if !k.IsStakerExternalTo(ctx, reviewer, affiliates) {
		return fmt.Errorf("%w: %s is affiliated with this initiative", types.ErrConflictOfInterest, reviewer)
	}

	// A staker on the initiative holds conviction on the outcome. Permitting it
	// would reintroduce "paid to say yes" one layer down.
	stakes, sErr := k.GetInitiativeStakes(ctx, initiative.Id)
	if sErr == nil {
		for _, st := range stakes {
			if st.Staker == reviewer {
				return fmt.Errorf("%w: %s has staked on this initiative and may not review it",
					types.ErrConflictOfInterest, reviewer)
			}
		}
	}

	// Reputation bar. Defaults to the initiative tier's own min_reputation —
	// the bar to review tracks the bar to do the work.
	minRep := k.reviewerReputationBar(ctx, initiative, project)
	if minRep.IsPositive() {
		tags := initiative.Tags
		if project.VerificationPolicy == nil || !project.VerificationPolicy.RequiresDomainRep {
			tags = nil // any tag counts
		}
		if !k.reviewerMeetsReputation(ctx, reviewer, tags, minRep) {
			return fmt.Errorf("%w: reviewer needs %s reputation%s",
				types.ErrInsufficientReputation, minRep,
				map[bool]string{true: " in this initiative's tags", false: ""}[len(tags) > 0])
		}
	}
	return nil
}

// reviewerReputationBar is the policy's min_verifier_reputation, or the
// initiative tier's own min_reputation when the policy leaves it unset.
func (k Keeper) reviewerReputationBar(ctx context.Context, initiative types.Initiative, project types.Project) math.LegacyDec {
	if p := project.VerificationPolicy; p != nil && p.MinVerifierReputation != nil &&
		!p.MinVerifierReputation.IsNil() && p.MinVerifierReputation.IsPositive() {
		return *p.MinVerifierReputation
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return math.LegacyZeroDec()
	}
	return TierConfigFor(params, initiative.Tier).MinReputation
}

func (k Keeper) reviewerMeetsReputation(ctx context.Context, addr string, tags []string, minRep math.LegacyDec) bool {
	member, err := k.Member.Get(ctx, addr)
	if err != nil {
		return false
	}
	if len(tags) == 0 {
		total := math.LegacyZeroDec()
		for _, raw := range member.ReputationScores {
			if d, dErr := math.LegacyNewDecFromStr(raw); dErr == nil {
				total = total.Add(d)
			}
		}
		return total.GTE(minRep)
	}
	for _, tag := range tags {
		if d, dErr := math.LegacyNewDecFromStr(member.ReputationScores[tag]); dErr == nil && d.GTE(minRep) {
			return true
		}
	}
	return false
}

// GetInitiativeReviews returns every verdict filed on one round.
func (k Keeper) GetInitiativeReviews(ctx context.Context, initiativeID uint64, round uint32) ([]types.InitiativeReview, error) {
	out := []types.InitiativeReview{}
	rng := collections.NewSuperPrefixedTripleRange[uint64, uint32, string](initiativeID, round)
	err := k.InitiativeReview.Walk(ctx, rng, func(_ collections.Triple[uint64, uint32, string], v types.InitiativeReview) (bool, error) {
		out = append(out, v)
		return false, nil
	})
	return out, err
}

// ReviewGateSatisfied reports whether the current round has collected the
// approvals the project's policy requires.
//
// Review is an *additional* brake: conviction, the challenge window and the
// no-active-challenges rule all still apply. It never substitutes for one.
func (k Keeper) ReviewGateSatisfied(ctx context.Context, params types.Params, initiative types.Initiative, project types.Project) (bool, error) {
	// Snapshotted project policy OR'd with the live chain-wide threshold — see
	// RequiredVerifiersFor for why the two sources are read differently.
	required := RequiredVerifiersFor(params, initiative, project)
	if required == 0 {
		return true, nil
	}
	// The committee resolved this round itself.
	switch initiative.ReviewEscalation {
	case types.ReviewEscalation_REVIEW_ESCALATION_APPROVED,
		types.ReviewEscalation_REVIEW_ESCALATION_PASSED:
		return true, nil
	case types.ReviewEscalation_REVIEW_ESCALATION_REJECTED:
		return false, nil
	}

	reviews, err := k.GetInitiativeReviews(ctx, initiative.Id, initiative.ReviewRound)
	if err != nil {
		return false, err
	}
	approvals := uint32(0)
	creatorApproved := false
	for _, r := range reviews {
		if !r.Approved {
			continue
		}
		approvals++
		if r.Reviewer == project.Creator {
			creatorApproved = true
		}
	}
	if approvals < required {
		return false, nil
	}
	if project.VerificationPolicy != nil && project.VerificationPolicy.RequiresCreatorApproval && !creatorApproved {
		return false, nil
	}
	return true, nil
}

// SubmitInitiativeReview files one reviewer's verdict on the current round.
//
// An approval that meets the gate leaves completion to the ordinary conviction
// and challenge-window path. A rejection returns the work to ASSIGNED so the
// assignee can fix and resubmit — the natural remedy for "not done" — and opens
// a new round, bounded by max_review_rounds.
func (k Keeper) SubmitInitiativeReview(
	ctx context.Context,
	initiativeID uint64,
	reviewer string,
	approved bool,
	criteriaVotes []*types.CriteriaVote,
	comments string,
) error {
	initiative, err := k.GetInitiative(ctx, initiativeID)
	if err != nil {
		return err
	}
	if initiative.Status != types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED &&
		initiative.Status != types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW {
		return fmt.Errorf("%w: initiative must be SUBMITTED or IN_REVIEW to review, got %s",
			types.ErrInvalidInitiativeStatus, initiative.Status)
	}
	project, err := k.GetProject(ctx, initiative.ProjectId)
	if err != nil {
		return err
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	if !ReviewRequired(params, initiative, project) {
		return fmt.Errorf("%w: initiative %d does not require reviewer sign-off",
			types.ErrInvalidRequest, initiative.Id)
	}
	if err := k.QualifiedReviewer(ctx, initiative, project, reviewer); err != nil {
		return err
	}
	// Per-criterion verdicts must answer criteria the initiative declared.
	if err := ValidateCriteriaVotes(initiative, criteriaVotes); err != nil {
		return err
	}

	key := collections.Join3(initiativeID, initiative.ReviewRound, reviewer)
	if _, has := k.InitiativeReview.Get(ctx, key); has == nil {
		return fmt.Errorf("%w: %s has already reviewed round %d of initiative %d",
			types.ErrInvalidRequest, reviewer, initiative.ReviewRound, initiativeID)
	}

	bond, err := k.ReviewerBondForInitiative(ctx, initiative)
	if err != nil {
		return err
	}
	if bond.IsPositive() {
		if err := k.ReserveBond(ctx, types.RoleType_ROLE_TYPE_INITIATIVE_REVIEWER, reviewer, bond); err != nil {
			return fmt.Errorf("failed to reserve reviewer bond: %w", err)
		}
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := k.InitiativeReview.Set(ctx, key, types.InitiativeReview{
		InitiativeId:  initiativeID,
		Round:         initiative.ReviewRound,
		Reviewer:      reviewer,
		Approved:      approved,
		CriteriaVotes: criteriaVotes,
		Comments:      comments,
		CreatedAt:     sdkCtx.BlockTime().Unix(),
		BondReserved:  bond,
	}); err != nil {
		return err
	}
	// From the first verdict the bounty is committed: reviewers commit bond and
	// do the reading on the strength of what was advertised, so a withdrawal
	// after this point would be a bait-and-switch.
	if err := k.MarkReviewBountyCommitted(ctx, initiative.Id); err != nil {
		return err
	}
	if err := k.RecordRoleAction(ctx, types.RoleType_ROLE_TYPE_INITIATIVE_REVIEWER, reviewer, "review"); err != nil {
		return err
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"initiative_reviewed",
		sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiativeID)),
		sdk.NewAttribute("round", fmt.Sprintf("%d", initiative.ReviewRound)),
		sdk.NewAttribute("reviewer", reviewer),
		sdk.NewAttribute("approved", fmt.Sprintf("%t", approved)),
		sdk.NewAttribute("bond_reserved", bond.String()),
	))

	if !approved {
		return k.rejectReviewRound(ctx, initiative, project, reviewer)
	}
	return nil
}

// rejectReviewRound sends the work back to the assignee for another round, or
// closes the initiative once max_review_rounds is exhausted.
func (k Keeper) rejectReviewRound(ctx context.Context, initiative types.Initiative, project types.Project, by string) error {
	// Backstop for the deferred callers. The escalation sweep reaches this with
	// an initiative it loaded blocks earlier from a keyset that carries no
	// status, so a round that resolves after the initiative was closed would
	// otherwise be sent back to ASSIGNED — resurrecting work whose budget has
	// already gone back to the project and whose bond has already been
	// released. Every terminal path clears its escalation entry; this catches
	// any that is ever missed.
	if types.IsInitiativeTerminal(initiative.Status) {
		return nil
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	if initiative.ReviewRound+1 >= k.maxReviewRounds(params) {
		// Out of rounds. Close is the clean exit: budget returns to the
		// project, the self-assign bond is released, nothing is minted.
		//
		// Deliberately terminal rather than an unassign back to OPEN. The round
		// cap exists so a bad-faith assignee cannot burn reviewer effort
		// without end, and reopening here would hand the same initiative to a
		// fresh assignee with the counter reset — defeating the cap by the one
		// route it is supposed to close.
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			"initiative_review_exhausted",
			sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiative.Id)),
			sdk.NewAttribute("rounds", fmt.Sprintf("%d", initiative.ReviewRound+1)),
			sdk.NewAttribute("rejected_by", by),
		))
		return k.CloseInitiative(ctx, initiative.Id, "review rejected: no rounds remaining")
	}

	// Drop any escalation flag for the round just closed. Left set, the sweep
	// would skip every later round as "already with the committee" and the new
	// round would silently lose its escalation path — the precise wedge the
	// liveness design exists to prevent.
	if err := k.EscalatedReviews.Remove(ctx, initiative.Id); err != nil {
		return err
	}
	initiative.ReviewRound++
	initiative.ReviewEscalation = types.ReviewEscalation_REVIEW_ESCALATION_NONE
	initiative.ReviewDeadline = 0
	initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED
	initiative.DeliverableUri = ""
	if err := k.UpdateInitiative(ctx, initiative); err != nil {
		return err
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"initiative_review_rejected",
		sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiative.Id)),
		sdk.NewAttribute("rejected_by", by),
		sdk.NewAttribute("next_round", fmt.Sprintf("%d", initiative.ReviewRound)),
		sdk.NewAttribute("rounds_remaining",
			fmt.Sprintf("%d", k.maxReviewRounds(params)-initiative.ReviewRound)),
	))
	return nil
}

// ReviewFeePool is the DREAM a round's reviewers share between them.
//
// Factored out of PayReviewFees so the per-season mint cap can be tested
// against it BEFORE any minting happens. The fee is freshly minted, so a gate
// that only counted the completer and treasury shares would let a completion
// through and then mint past the cap it had just checked.
func (k Keeper) ReviewFeePool(ctx context.Context, params types.Params, initiative types.Initiative) math.Int {
	budget := DerefInt(initiative.Budget)
	if budget.IsNil() || !budget.IsPositive() {
		return math.ZeroInt()
	}
	tierMult := TierConfigFor(params, initiative.Tier).RewardMultiplier
	if tierMult.IsNil() || !tierMult.IsPositive() {
		tierMult = math.LegacyOneDec()
	}
	return k.reviewFeeRate(params).Mul(tierMult).MulInt(budget).TruncateInt()
}

// PayReviewFees pays the reviewers who filed a verdict on the round that
// resolved the initiative, and settles their bond.
//
// Paid per verdict filed, never per approval: if approving paid and rejecting
// did not, the role would rebuild the exact bias it exists to remove. Called on
// both terminal paths — completion and close — so the fee does not depend on
// the outcome either.
// Returns the total minted so callers on the close path can reduce what they
// hand back to the project by it.
func (k Keeper) PayReviewFees(ctx context.Context, initiative types.Initiative) (math.Int, error) {
	reviews, err := k.GetInitiativeReviews(ctx, initiative.Id, initiative.ReviewRound)
	if err != nil || len(reviews) == 0 {
		return math.ZeroInt(), err
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return math.ZeroInt(), err
	}
	pool := k.ReviewFeePool(ctx, params, initiative)
	if !pool.IsPositive() {
		return math.ZeroInt(), nil
	}
	share := pool.QuoRaw(int64(len(reviews)))
	if !share.IsPositive() {
		return math.ZeroInt(), nil
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	minted := math.ZeroInt()
	for _, r := range reviews {
		addr, aErr := sdk.AccAddressFromBech32(r.Reviewer)
		if aErr != nil {
			continue
		}
		if err := k.MintDREAM(ctx, addr, share); err != nil {
			return math.ZeroInt(), fmt.Errorf("failed to pay review fee to %s: %w", r.Reviewer, err)
		}
		minted = minted.Add(share)
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			"initiative_review_fee_paid",
			sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiative.Id)),
			sdk.NewAttribute("reviewer", r.Reviewer),
			sdk.NewAttribute("amount", share.String()),
			sdk.NewAttribute("approved", fmt.Sprintf("%t", r.Approved)),
		))
	}
	if minted.IsPositive() {
		if err := k.TrackInitiativeRewardMint(ctx, minted); err != nil {
			return math.ZeroInt(), fmt.Errorf("failed to track review fee mint: %w", err)
		}
	}
	return minted, nil
}

// SettleReviewBonds releases the bond committed against every verdict on an
// initiative once it is no longer contestable. Idempotent via review.settled.
func (k Keeper) SettleReviewBonds(ctx context.Context, initiativeID uint64) error {
	var keys []collections.Triple[uint64, uint32, string]
	var vals []types.InitiativeReview
	rng := collections.NewPrefixedTripleRange[uint64, uint32, string](initiativeID)
	if err := k.InitiativeReview.Walk(ctx, rng, func(key collections.Triple[uint64, uint32, string], v types.InitiativeReview) (bool, error) {
		if !v.Settled {
			keys = append(keys, key)
			vals = append(vals, v)
		}
		return false, nil
	}); err != nil {
		return err
	}
	for i, v := range vals {
		if v.BondReserved.IsPositive() {
			if err := k.ReleaseBond(ctx, types.RoleType_ROLE_TYPE_INITIATIVE_REVIEWER, v.Reviewer, v.BondReserved); err != nil {
				return err
			}
		}
		v.Settled = true
		if err := k.InitiativeReview.Set(ctx, keys[i], v); err != nil {
			return err
		}
	}
	return nil
}

// SlashReviewersOnOverturn charges the reviewers whose verdict a jury has
// contradicted, and records the outcome against every verdict on the round so
// the accuracy ring reflects who was right as well as who was wrong.
//
// approvedWasWrong distinguishes the two directions: an upheld challenge means
// the approvers passed bad work; a successful rejection appeal means the
// rejecters blocked good work.
func (k Keeper) SlashReviewersOnOverturn(ctx context.Context, initiativeID uint64, round uint32, approvedWasWrong bool) error {
	reviews, err := k.GetInitiativeReviews(ctx, initiativeID, round)
	if err != nil {
		return err
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	for _, r := range reviews {
		wrong := r.Approved == approvedWasWrong
		if err := k.RecordRoleOutcome(ctx, types.RoleType_ROLE_TYPE_INITIATIVE_REVIEWER,
			r.Reviewer, "review", !wrong); err != nil {
			return err
		}
		if !r.BondReserved.IsPositive() {
			continue
		}
		if !wrong {
			// This reviewer was vindicated. Their commitment has to come back:
			// marking the round settled below would otherwise strand it, and
			// stranding the bond of the reviewer who got it *right* is the worst
			// possible incentive to build into the role.
			if err := k.ReleaseBond(ctx, types.RoleType_ROLE_TYPE_INITIATIVE_REVIEWER,
				r.Reviewer, r.BondReserved); err != nil {
				return err
			}
			continue
		}
		if err := k.SlashBond(ctx, types.RoleType_ROLE_TYPE_INITIATIVE_REVIEWER,
			r.Reviewer, r.BondReserved, "initiative review overturned by jury"); err != nil {
			return err
		}
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			"initiative_review_overturned",
			sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiativeID)),
			sdk.NewAttribute("reviewer", r.Reviewer),
			sdk.NewAttribute("slashed", r.BondReserved.String()),
		))
	}
	// This round is now fully accounted for — slashed or released — so mark it
	// settled to keep the release sweep from double-counting it.
	if err := k.markReviewRoundSettled(ctx, initiativeID, round); err != nil {
		return err
	}
	// An upheld challenge leaves the initiative REJECTED, which is terminal but
	// is not the Complete/Abandon path that settles bonds. Release every *other*
	// round's commitments here or they are stranded for good.
	return k.SettleReviewBonds(ctx, initiativeID)
}

func (k Keeper) markReviewRoundSettled(ctx context.Context, initiativeID uint64, round uint32) error {
	var keys []collections.Triple[uint64, uint32, string]
	var vals []types.InitiativeReview
	rng := collections.NewSuperPrefixedTripleRange[uint64, uint32, string](initiativeID, round)
	if err := k.InitiativeReview.Walk(ctx, rng, func(key collections.Triple[uint64, uint32, string], v types.InitiativeReview) (bool, error) {
		keys = append(keys, key)
		vals = append(vals, v)
		return false, nil
	}); err != nil {
		return err
	}
	for i, v := range vals {
		v.Settled = true
		if err := k.InitiativeReview.Set(ctx, keys[i], v); err != nil {
			return err
		}
	}
	return nil
}

// ReviewerAddressesFor lists everyone who filed a verdict on a round, for events.
func ReviewerAddressesFor(reviews []types.InitiativeReview) string {
	addrs := make([]string, 0, len(reviews))
	for _, r := range reviews {
		addrs = append(addrs, r.Reviewer)
	}
	return strings.Join(addrs, ",")
}

// isOpsCommitteeAddr adapts the bech32 strings the review messages carry to the
// AccAddress-typed committee check.
func (k Keeper) isOpsCommitteeAddr(ctx context.Context, addr string) bool {
	acc, err := sdk.AccAddressFromBech32(addr)
	if err != nil {
		return false
	}
	return k.IsOperationsCommittee(ctx, acc)
}

// ValidateVerificationPolicy bounds a policy before it is stored.
//
// The windows are clamped rather than rejected: a project may be more
// conservative than the chain default, never less. Without that a permissionless
// project creator could shrink their own contest window toward zero and walk
// past the brake the self-assignment safeguards depend on — which is exactly why
// the override needs no approval to use.
func (k Keeper) ValidateVerificationPolicy(ctx context.Context, policy *types.VerificationPolicy) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	if policy.MinVerifierCount > types.MaxVerifierCount {
		return fmt.Errorf("%w: min_verifier_count %d exceeds the maximum %d",
			types.ErrInvalidRequest, policy.MinVerifierCount, types.MaxVerifierCount)
	}
	if policy.MinVerifierReputation != nil && policy.MinVerifierReputation.IsNegative() {
		return fmt.Errorf("%w: min_verifier_reputation must not be negative", types.ErrInvalidRequest)
	}
	if policy.ReviewPeriodEpochs < params.DefaultReviewPeriodEpochs {
		policy.ReviewPeriodEpochs = params.DefaultReviewPeriodEpochs
	}
	if policy.ChallengePeriodEpochs < params.DefaultChallengePeriodEpochs {
		policy.ChallengePeriodEpochs = params.DefaultChallengePeriodEpochs
	}
	return nil
}

// ResolveReviewEscalation applies the committee's decision to a stalled round.
//
// All three resolutions still run the challenge window: committee approval
// satisfies the reviewer requirement and nothing else, never a bypass around the
// one brake that does not depend on somebody showing up. It writes no
// RoleActivity either — the committee holds no bond and carries no accuracy
// record — and emits its own event so committee-approved completions stay
// auditable.
func (k Keeper) ResolveReviewEscalation(
	ctx context.Context,
	initiativeID uint64,
	resolution types.ReviewEscalation,
	by string,
	reason string,
) error {
	initiative, err := k.GetInitiative(ctx, initiativeID)
	if err != nil {
		return err
	}
	if initiative.ReviewDeadline == 0 {
		return fmt.Errorf("%w: initiative %d has no open review round", types.ErrInvalidRequest, initiativeID)
	}
	if initiative.ReviewEscalation != types.ReviewEscalation_REVIEW_ESCALATION_NONE {
		return fmt.Errorf("%w: review round %d of initiative %d is already resolved",
			types.ErrInvalidRequest, initiative.ReviewRound, initiativeID)
	}
	project, err := k.GetProject(ctx, initiative.ProjectId)
	if err != nil {
		return err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"initiative_review_escalation_resolved",
		sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiativeID)),
		sdk.NewAttribute("round", fmt.Sprintf("%d", initiative.ReviewRound)),
		sdk.NewAttribute("resolution", resolution.String()),
		sdk.NewAttribute("resolved_by", by),
		sdk.NewAttribute("reason", reason),
	))

	if resolution == types.ReviewEscalation_REVIEW_ESCALATION_REJECTED {
		return k.rejectReviewRound(ctx, initiative, project, by)
	}
	initiative.ReviewEscalation = resolution
	return k.UpdateInitiative(ctx, initiative)
}

// RecordReviewRoundUpheld credits every verdict on a round whose challenge the
// jury rejected — the reviewers were right.
//
// Deliberately does not settle the bond: a rejected challenge does not end the
// initiative, and a later challenge could still land, so the commitment stands
// until the initiative itself resolves.
func (k Keeper) RecordReviewRoundUpheld(ctx context.Context, initiativeID uint64, round uint32) error {
	reviews, err := k.GetInitiativeReviews(ctx, initiativeID, round)
	if err != nil {
		return err
	}
	for _, r := range reviews {
		if err := k.RecordRoleOutcome(ctx, types.RoleType_ROLE_TYPE_INITIATIVE_REVIEWER,
			r.Reviewer, "review", r.Approved); err != nil {
			return err
		}
	}
	return nil
}

// maxReviewEscalationsPerBlock bounds the sweep's per-block work, in the same
// spirit as maxJuryDeadlinesPerBlock: a value set too high is itself a liveness
// risk, so it is a compile-time constant rather than a governance knob.
const maxReviewEscalationsPerBlock = 50

// SweepReviewDeadlines escalates review rounds that hit their deadline without
// meeting the gate.
//
// The escalation only *flags* the round for the committee; it does not decide
// anything. The committee then approves, rejects, or passes — and if it never
// acts, the escalation-timeout sweep below resolves to PASSED. This module has
// already been bitten once by an escalation that expired and touched nothing,
// freezing an initiative permanently with the challenger's stake, every staker's
// conviction and the assignee's bond locked inside it. Silence must never wedge
// an initiative, and silence must never mint.
func (k Keeper) SweepReviewDeadlines(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	height := sdkCtx.BlockHeight()

	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	type due struct {
		initiative types.Initiative
		project    types.Project
	}
	var pending []due
	var adopt []types.Initiative

	if err := k.IterateInitiativesByStatuses(ctx, []types.InitiativeStatus{
		types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED,
		types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW,
	}, func(_ uint64, initiative types.Initiative) bool {
		if len(pending) >= maxReviewEscalationsPerBlock {
			return true
		}
		if initiative.ReviewEscalation != types.ReviewEscalation_REVIEW_ESCALATION_NONE {
			return false
		}
		// Whether a gate applies is decided first, so an ungated initiative is
		// never adopted below and a gated one is never skipped for want of a
		// deadline.
		project, pErr := k.GetProject(ctx, initiative.ProjectId)
		if pErr != nil || !ReviewRequired(params, initiative, project) {
			return false
		}
		satisfied, sErr := k.ReviewGateSatisfied(ctx, params, initiative, project)
		if sErr != nil || satisfied {
			return false
		}
		// A deadline of zero on a GATED initiative means it was submitted while
		// ungated and has since come under the gate — the chain-wide threshold
		// was lowered, or the project policy arrived late. Skipping it would
		// leave it unable to complete and invisible to the escalation path: a
		// permanent wedge introduced by the gate itself. Adopt it instead by
		// opening a review window now, so it gets a full window under the rules
		// that now apply rather than one that expired before anyone knew.
		if initiative.ReviewDeadline == 0 {
			adopt = append(adopt, initiative)
			return false
		}
		if height < initiative.ReviewDeadline {
			return false
		}
		// Already with the committee: its window is what the deadline now
		// measures, and letting this branch fire again would keep extending it
		// forever instead of ever timing out.
		if escalated, eErr := k.EscalatedReviews.Has(ctx, initiative.Id); eErr == nil && escalated {
			return false
		}
		pending = append(pending, due{initiative: initiative, project: project})
		return false
	}); err != nil {
		return err
	}

	// Open a window for initiatives that came under the gate after submission.
	// Done before the escalation pass so an adopted initiative starts its
	// window rather than being escalated in the same block.
	for _, initiative := range adopt {
		initiative.ReviewDeadline = height + (params.DefaultReviewPeriodEpochs * params.EpochBlocks)
		initiative.ReviewEscalation = types.ReviewEscalation_REVIEW_ESCALATION_NONE
		if err := k.UpdateInitiative(ctx, initiative); err != nil {
			return err
		}
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			"initiative_review_window_opened",
			sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiative.Id)),
			sdk.NewAttribute("reason", "gate_applied_after_submission"),
			sdk.NewAttribute("review_deadline", fmt.Sprintf("%d", initiative.ReviewDeadline)),
		))
	}

	// Collect before mutating: the loop above walks the status index and the
	// escalation below writes the initiative back into it.
	for _, d := range pending {
		// Give the committee its own window, then resolve to PASSED on silence.
		d.initiative.ReviewDeadline = height + (params.DefaultReviewPeriodEpochs * params.EpochBlocks)
		d.initiative.ReviewEscalation = types.ReviewEscalation_REVIEW_ESCALATION_NONE
		if err := k.UpdateInitiative(ctx, d.initiative); err != nil {
			return err
		}
		if err := k.EscalatedReviews.Set(ctx, d.initiative.Id); err != nil {
			return err
		}
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			"initiative_review_escalated",
			sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", d.initiative.Id)),
			sdk.NewAttribute("round", fmt.Sprintf("%d", d.initiative.ReviewRound)),
			sdk.NewAttribute("committee_deadline", fmt.Sprintf("%d", d.initiative.ReviewDeadline)),
		))
	}

	return k.resolveSilentEscalations(ctx, height)
}

// resolveSilentEscalations applies the default the committee declined to make.
//
// PASSED, never APPROVED: silence must not mint. The initiative proceeds on
// conviction alone and the challenge window still runs, so the work remains
// contestable by anyone willing to bond against it.
func (k Keeper) resolveSilentEscalations(ctx context.Context, height int64) error {
	var expired []uint64
	if err := k.EscalatedReviews.Walk(ctx, nil, func(id uint64) (bool, error) {
		if len(expired) >= maxReviewEscalationsPerBlock {
			return true, nil
		}
		initiative, err := k.GetInitiative(ctx, id)
		if err != nil {
			expired = append(expired, id)
			return false, nil
		}
		if initiative.ReviewEscalation != types.ReviewEscalation_REVIEW_ESCALATION_NONE ||
			(initiative.ReviewDeadline > 0 && height >= initiative.ReviewDeadline) {
			expired = append(expired, id)
		}
		return false, nil
	}); err != nil {
		return err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	for _, id := range expired {
		if err := k.EscalatedReviews.Remove(ctx, id); err != nil {
			return err
		}
		initiative, err := k.GetInitiative(ctx, id)
		if err != nil {
			continue
		}
		if initiative.ReviewEscalation != types.ReviewEscalation_REVIEW_ESCALATION_NONE {
			continue // the committee acted in time
		}

		// Nobody reviewed and the committee did not act. This used to resolve
		// to PASSED, which let a gated initiative complete and mint with no
		// verdict on it at all — the review gate could be waited out. It now
		// rejects the round instead: the assignee resubmits and gets another
		// window (bounded by max_review_rounds), and when the rounds run out
		// rejectReviewRound closes cleanly — budget returned, bond released,
		// nothing minted.
		//
		// Silence must never mint. It must also never wedge, which is why the
		// terminal state is a close rather than an indefinite hold.
		project, pErr := k.GetProject(ctx, initiative.ProjectId)
		if pErr != nil {
			sdkCtx.Logger().Error("review escalation timeout: project missing",
				"initiative_id", id, "project_id", initiative.ProjectId, "error", pErr)
			continue
		}
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			"initiative_review_escalation_timeout",
			sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", id)),
			sdk.NewAttribute("resolution", "round_rejected"),
			sdk.NewAttribute("round", fmt.Sprintf("%d", initiative.ReviewRound)),
		))
		if err := k.rejectReviewRound(ctx, initiative, project, "escalation timeout: no verdict filed"); err != nil {
			return err
		}
	}
	return nil
}

// HasReviewedInitiative reports whether an address has filed a verdict on any
// round of an initiative.
//
// Used by CreateStake to close the reverse of the conflict QualifiedReviewer
// blocks: reviewing and then staking is the same conflict as staking and then
// reviewing, just acquired in the other order.
func (k Keeper) HasReviewedInitiative(ctx context.Context, initiativeID uint64, addr string) (bool, error) {
	found := false
	rng := collections.NewPrefixedTripleRange[uint64, uint32, string](initiativeID)
	err := k.InitiativeReview.Walk(ctx, rng, func(_ collections.Triple[uint64, uint32, string], v types.InitiativeReview) (bool, error) {
		if v.Reviewer == addr {
			found = true
			return true, nil
		}
		return false, nil
	})
	return found, err
}
