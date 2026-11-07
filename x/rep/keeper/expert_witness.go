package keeper

import (
	"context"
	"fmt"
	"slices"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/rep/types"
)

// MaxExpertWitnessesPerReview caps how many experts the jurors of one review
// may call. Every invitation opens an EXPERT_TESTIMONY interim the verdict may
// pay, so the cap bounds what a jury can mint on top of its own fees.
const MaxExpertWitnessesPerReview = 2

// expertTestimonyReferenceType marks an EXPERT_TESTIMONY interim's
// reference_id as a jury review id.
const expertTestimonyReferenceType = "jury_review"

// InviteExpertWitness lets a juror seated on a pending initiative jury review
// call an expert witness, and opens the EXPERT_TESTIMONY interim bound to the
// invitation (JuryReview.expert_interim_ids). The interim is settled when the
// review reaches its verdict: paid if the expert testified, expired otherwise
// (settleExpertTestimonies). A voided review pays nobody.
//
// Eligibility is expertise without a stake in the outcome: an active
// ESTABLISHED+ member with reputation in one of the initiative's tags, who is
// neither the challenger, anyone affiliated with the initiative (assignee,
// apprentice, creator, project creator), a juror on the review nor a juror
// who declined or was vacated from it (juryExclusions), nor an invitation
// neighbour of any of them (excludedAsExpert).
func (k Keeper) InviteExpertWitness(
	ctx context.Context,
	juryReviewID uint64,
	jurorAddr sdk.AccAddress,
	expertAddr sdk.AccAddress,
) (uint64, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	review, err := k.GetJuryReview(ctx, juryReviewID)
	if err != nil {
		return 0, err
	}
	// Content challenges and moderation appeals carry no initiative and so no
	// tags to measure expertise against.
	if review.InitiativeId == 0 || review.ChallengeId == 0 || review.ContentChallengeId != 0 {
		return 0, errorsmod.Wrapf(types.ErrNotInitiativeReview, "jury review %d is not an initiative review", juryReviewID)
	}
	if review.Verdict != types.Verdict_VERDICT_PENDING {
		return 0, errorsmod.Wrapf(types.ErrNotInitiativeReview, "jury review %d already has a verdict (%s)", juryReviewID, review.Verdict)
	}
	if sdkCtx.BlockHeight() >= review.Deadline {
		return 0, errorsmod.Wrapf(types.ErrNotInitiativeReview, "jury review %d is past its deadline", juryReviewID)
	}

	jurorStr := jurorAddr.String()
	inviterSeat, seated := k.jurorSeatFor(ctx, review, jurorStr)
	if !seated {
		return 0, errorsmod.Wrapf(types.ErrNotSeatedJuror, "%s on jury review %d", jurorStr, juryReviewID)
	}
	if len(review.ExpertWitnesses) >= MaxExpertWitnessesPerReview {
		return 0, errorsmod.Wrapf(types.ErrTooManyExpertWitnesses, "limit %d", MaxExpertWitnessesPerReview)
	}

	expertStr := expertAddr.String()
	for _, w := range review.ExpertWitnesses {
		if k.SameMember(ctx, w, expertStr) {
			return 0, errorsmod.Wrapf(types.ErrExpertIneligible, "%s is already an expert witness on jury review %d", expertStr, juryReviewID)
		}
	}

	// Member is keyed by the canonical address, so a tombstoned address fails
	// here as well.
	if !k.IsActiveMember(ctx, expertAddr) {
		return 0, errorsmod.Wrapf(types.ErrExpertIneligible, "%s is not an active member", expertStr)
	}
	trust, err := k.GetTrustLevel(ctx, expertAddr)
	if err != nil {
		return 0, err
	}
	if trust < types.TrustLevel_TRUST_LEVEL_ESTABLISHED {
		return 0, errorsmod.Wrapf(types.ErrExpertIneligible, "%s is %s", expertStr, trust)
	}

	initiative, err := k.GetInitiative(ctx, review.InitiativeId)
	if err != nil {
		return 0, err
	}
	tagRep, err := k.GetReputationForTags(ctx, expertAddr, initiative.Tags)
	if err != nil {
		return 0, err
	}
	if !tagRep.IsPositive() {
		return 0, errorsmod.Wrapf(types.ErrExpertIneligible, "%s has no reputation in the initiative's tags %v", expertStr, initiative.Tags)
	}

	// Anyone a jury draw could not seat on this review is no expert on it
	// either: the challenger, every initiative affiliate, the seated jurors
	// and those who declined or were vacated (REP-E-102). One list, shared
	// with SelectJury, so the two can never drift apart. Their invitation
	// neighbours are refused too (REP-E-103): the inviting juror alone
	// directs this pay.
	if k.excludedAsExpert(ctx, review, expertStr) {
		return 0, errorsmod.Wrapf(types.ErrExpertIneligible, "%s is a party to, juror on, or invitation neighbour of someone in the dispute", expertStr)
	}

	// Also enforces the expert's MaxActiveInterimsPerMember cap.
	interimID, err := k.CreateInterimWork(
		ctx,
		types.InterimType_INTERIM_TYPE_EXPERT_TESTIMONY,
		[]string{expertStr},
		"",
		review.Id,
		expertTestimonyReferenceType,
		types.InterimComplexity_INTERIM_COMPLEXITY_COMPLEX,
		review.Deadline,
	)
	if err != nil {
		return 0, err
	}

	// The four expert_* lists are parallel; alignExpertLists keeps a review
	// written before the inviter/endorser lists existed consistent.
	alignExpertLists(&review)
	review.ExpertWitnesses = append(review.ExpertWitnesses, expertStr)
	review.ExpertInterimIds = append(review.ExpertInterimIds, interimID)
	review.ExpertInviters = append(review.ExpertInviters, inviterSeat)
	review.ExpertEndorsers = append(review.ExpertEndorsers, "")
	if err := k.JuryReview.Set(ctx, review.Id, review); err != nil {
		return 0, err
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"expert_witness_invited",
			sdk.NewAttribute("jury_review_id", fmt.Sprintf("%d", review.Id)),
			sdk.NewAttribute("juror", jurorStr),
			sdk.NewAttribute("expert", expertStr),
			sdk.NewAttribute("interim_id", fmt.Sprintf("%d", interimID)),
		),
	)
	return interimID, nil
}

// alignExpertLists pads expert_interim_ids, expert_inviters and
// expert_endorsers up to the length of expert_witnesses so the four lists
// index together. An invitation with no recorded inviter or endorser is
// simply unendorsed; one with no recorded interim pads with id 0, which
// settleExpertTestimonies matches only against an interim referencing this
// review and assigned to this witness (interim ids start at 0).
func alignExpertLists(review *types.JuryReview) {
	for len(review.ExpertInterimIds) < len(review.ExpertWitnesses) {
		review.ExpertInterimIds = append(review.ExpertInterimIds, 0)
	}
	for len(review.ExpertInviters) < len(review.ExpertWitnesses) {
		review.ExpertInviters = append(review.ExpertInviters, "")
	}
	for len(review.ExpertEndorsers) < len(review.ExpertWitnesses) {
		review.ExpertEndorsers = append(review.ExpertEndorsers, "")
	}
}

// expertEndorsed reports whether the i-th invitation on review has been
// endorsed by a second juror. Out-of-range (a review written before the
// endorser list existed) reads as unendorsed: nothing pays without a second
// juror on record.
func expertEndorsed(review types.JuryReview, i int) bool {
	return i < len(review.ExpertEndorsers) && review.ExpertEndorsers[i] != ""
}

// expertIndex returns the index of the invitation addr holds on review, or -1.
func (k Keeper) expertIndex(ctx context.Context, review types.JuryReview, addr string) int {
	for i, w := range review.ExpertWitnesses {
		if k.SameMember(ctx, w, addr) {
			return i
		}
	}
	return -1
}

// jurorAnsweredSummons reports whether seat has accepted jury duty on review
// or already voted on it: the two ways a seat drawn by lot becomes a
// commitment (the definition the redraw sweep uses for an answered seat).
func jurorAnsweredSummons(review types.JuryReview, seat string) bool {
	if slices.Contains(review.Accepted, seat) {
		return true
	}
	for _, v := range review.Votes {
		if v != nil && v.Juror == seat {
			return true
		}
	}
	return false
}

// EndorseExpertWitness records a second juror's endorsement of a pending
// expert invitation (REP-E-103). The endorser must be seated on the review,
// have answered the summons (accepted or voted), and not be the juror who
// issued the invitation; an invitation is endorsed at most once. Until
// endorsed the expert cannot testify and the invitation is never paid, so one
// juror alone can never direct expert pay. The endorsement must land while
// the review is still PENDING and before its deadline, like the invitation.
func (k Keeper) EndorseExpertWitness(
	ctx context.Context,
	juryReviewID uint64,
	jurorAddr sdk.AccAddress,
	expertAddr sdk.AccAddress,
) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	review, err := k.GetJuryReview(ctx, juryReviewID)
	if err != nil {
		return err
	}
	if review.Verdict != types.Verdict_VERDICT_PENDING {
		return errorsmod.Wrapf(types.ErrJuryReviewResolved, "jury review %d already returned %s", juryReviewID, review.Verdict)
	}
	if sdkCtx.BlockHeight() >= review.Deadline {
		return errorsmod.Wrapf(types.ErrNotInitiativeReview, "jury review %d is past its deadline", juryReviewID)
	}

	jurorStr := jurorAddr.String()
	seat, seated := k.jurorSeatFor(ctx, review, jurorStr)
	if !seated {
		return errorsmod.Wrapf(types.ErrNotSeatedJuror, "%s on jury review %d", jurorStr, juryReviewID)
	}
	if !jurorAnsweredSummons(review, seat) {
		return errorsmod.Wrapf(types.ErrInvalidEndorsement, "%s has not accepted jury duty on review %d", jurorStr, juryReviewID)
	}

	expertStr := expertAddr.String()
	i := k.expertIndex(ctx, review, expertStr)
	if i < 0 {
		return errorsmod.Wrapf(types.ErrInvalidEndorsement, "%s is not an invited expert on jury review %d", expertStr, juryReviewID)
	}
	alignExpertLists(&review)
	if review.ExpertEndorsers[i] != "" {
		return errorsmod.Wrapf(types.ErrInvalidEndorsement, "expert %s on jury review %d is already endorsed", expertStr, juryReviewID)
	}
	// Seat addresses compare directly (both are review.Jurors entries), but a
	// member can hold only one seat per review, so SameMember is the safe form.
	inviter := review.ExpertInviters[i]
	if inviter == "" {
		return errorsmod.Wrapf(types.ErrInvalidEndorsement, "expert %s on jury review %d has no recorded inviter", expertStr, juryReviewID)
	}
	if k.SameMember(ctx, inviter, seat) {
		return errorsmod.Wrapf(types.ErrInvalidEndorsement, "%s invited expert %s and cannot endorse their own invitation", jurorStr, expertStr)
	}

	review.ExpertEndorsers[i] = seat
	if err := k.JuryReview.Set(ctx, review.Id, review); err != nil {
		return err
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"expert_witness_endorsed",
			sdk.NewAttribute("jury_review_id", fmt.Sprintf("%d", review.Id)),
			sdk.NewAttribute("juror", jurorStr),
			sdk.NewAttribute("inviter", review.ExpertInviters[i]),
			sdk.NewAttribute("expert", review.ExpertWitnesses[i]),
		),
	)
	return nil
}

// settleExpertTestimonies closes the EXPERT_TESTIMONY interims review's
// invitations opened, by id (REP-E-101, REP-R-105: never a walk over pending
// interims). Called once, when the review leaves PENDING:
//
//   - pay true (TallyJuryVotes, any verdict): an endorsed expert who
//     testified is paid their interim's budget; one who did not, or whose
//     invitation no second juror endorsed, has it expired.
//   - pay false (the review was voided): every interim is expired unpaid.
//
// Payment never blocks the verdict: each settlement runs in its own cache
// context, and a failure (the season's interim reward cap reached, say) is
// reported as an expert_testimony_unpaid event and the interim expired.
func (k Keeper) settleExpertTestimonies(ctx context.Context, review types.JuryReview, pay bool) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	for i, witness := range review.ExpertWitnesses {
		if i >= len(review.ExpertInterimIds) {
			break
		}
		interimID := review.ExpertInterimIds[i]
		interim, err := k.GetInterim(ctx, interimID)
		if err != nil || interim.Type != types.InterimType_INTERIM_TYPE_EXPERT_TESTIMONY ||
			interim.ReferenceType != expertTestimonyReferenceType || interim.ReferenceId != review.Id ||
			!slices.ContainsFunc(interim.Assignees, func(a string) bool { return k.SameMember(ctx, a, witness) }) ||
			(interim.Status != types.InterimStatus_INTERIM_STATUS_PENDING &&
				interim.Status != types.InterimStatus_INTERIM_STATUS_IN_PROGRESS) {
			// Abandoned by the expert, or closed by their zeroing.
			continue
		}
		// SubmitExpertTestimony refuses an unendorsed expert, so an endorsed
		// check here is belt and braces: nothing pays on one juror's word.
		endorsed := expertEndorsed(review, i)
		testified := false
		for _, t := range review.Testimonies {
			if k.SameMember(ctx, t.Expert, witness) {
				testified = true
				break
			}
		}

		if pay && endorsed && testified {
			cacheCtx, write := sdkCtx.CacheContext()
			err := k.CompleteInterimDirectly(cacheCtx, interimID, types.AdjudicationDecision_ADJUDICATION_DECISION_UNSPECIFIED,
				fmt.Sprintf("testimony on jury review %d (verdict %s)", review.Id, review.Verdict))
			if err == nil {
				write()
				continue
			}
			sdkCtx.Logger().Error("failed to pay expert testimony", "jury_review_id", review.Id, "interim_id", interimID, "error", err)
			sdkCtx.EventManager().EmitEvent(
				sdk.NewEvent(
					"expert_testimony_unpaid",
					sdk.NewAttribute("jury_review_id", fmt.Sprintf("%d", review.Id)),
					sdk.NewAttribute("interim_id", fmt.Sprintf("%d", interimID)),
					sdk.NewAttribute("error", err.Error()),
				),
			)
		}

		interim.Status = types.InterimStatus_INTERIM_STATUS_EXPIRED
		switch {
		case !pay:
			interim.CompletionNotes = fmt.Sprintf("jury review %d voided", review.Id)
		case !endorsed:
			interim.CompletionNotes = fmt.Sprintf("invitation on jury review %d was never endorsed by a second juror", review.Id)
		case !testified:
			interim.CompletionNotes = fmt.Sprintf("no testimony before the verdict on jury review %d", review.Id)
		default:
			interim.CompletionNotes = fmt.Sprintf("testimony on jury review %d could not be paid", review.Id)
		}
		if err := k.UpdateInterim(ctx, interim); err != nil {
			sdkCtx.Logger().Error("failed to expire expert interim", "jury_review_id", review.Id, "interim_id", interimID, "error", err)
		}
	}
}

// expertInterimAwaitingVerdict reports whether interim is an invitation's
// EXPERT_TESTIMONY interim whose review is still PENDING. The EndBlocker's
// deadline expiry skips these: the review settles them, and its deadline
// tally may land in a later block than the interim deadline (the per-block
// tally cap), which would otherwise expire a testified expert unpaid.
func (k Keeper) expertInterimAwaitingVerdict(ctx context.Context, interim types.Interim) bool {
	if interim.Type != types.InterimType_INTERIM_TYPE_EXPERT_TESTIMONY ||
		interim.ReferenceType != expertTestimonyReferenceType {
		return false
	}
	review, err := k.JuryReview.Get(ctx, interim.ReferenceId)
	if err != nil || review.Verdict != types.Verdict_VERDICT_PENDING {
		return false
	}
	return slices.Contains(review.ExpertInterimIds, interim.Id)
}
