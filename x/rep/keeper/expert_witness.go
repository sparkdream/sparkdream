package keeper

import (
	"context"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/rep/types"
)

// MaxExpertWitnessesPerReview caps how many experts the jurors of one review
// may call. Every invitation opens a paid EXPERT_TESTIMONY interim, so the cap
// bounds what a jury can mint on top of its own fees.
const MaxExpertWitnessesPerReview = 2

// expertTestimonyReferenceType marks an EXPERT_TESTIMONY interim's
// reference_id as a jury review id.
const expertTestimonyReferenceType = "jury_review"

// InviteExpertWitness lets a juror seated on a pending initiative jury review
// call an expert witness, and opens the EXPERT_TESTIMONY interim that pays the
// expert when they testify. The interim's deadline is the review's: an expert
// who never testifies lets it expire unpaid.
//
// Eligibility is expertise without a stake in the outcome: an active
// ESTABLISHED+ member with reputation in one of the initiative's tags, who is
// neither the challenger, anyone affiliated with the initiative (assignee,
// apprentice, creator, project creator) nor a juror on the review.
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
	if _, seated := k.jurorSeatFor(ctx, review, jurorStr); !seated {
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

	challenge, err := k.GetChallenge(ctx, review.ChallengeId)
	if err != nil {
		return 0, err
	}
	parties := []string{challenge.Challenger}
	if project, pErr := k.GetProject(ctx, initiative.ProjectId); pErr == nil {
		parties = append(parties, InitiativeAffiliates(initiative, project)...)
	} else {
		parties = append(parties, initiative.Assignee, initiative.Apprentice, initiative.Creator)
	}
	parties = append(parties, review.Jurors...)
	for _, p := range parties {
		if k.SameMember(ctx, p, expertStr) {
			return 0, errorsmod.Wrapf(types.ErrExpertIneligible, "%s is a party to or juror on the dispute", expertStr)
		}
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

	review.ExpertWitnesses = append(review.ExpertWitnesses, expertStr)
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

// payExpertTestimony completes the EXPERT_TESTIMONY interim opened when the
// expert was invited, paying its budget. Payment never blocks the testimony:
// a failure (the season's interim reward cap reached, say) is discarded with
// its cache context and reported as an event, and the interim stays pending
// until it expires with the review.
func (k Keeper) payExpertTestimony(ctx context.Context, review types.JuryReview, witness string) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	var interimID uint64
	found := false
	k.IteratePendingInterims(ctx, func(_ int64, interim types.Interim) bool {
		if interim.Type != types.InterimType_INTERIM_TYPE_EXPERT_TESTIMONY ||
			interim.ReferenceId != review.Id ||
			interim.ReferenceType != expertTestimonyReferenceType {
			return false
		}
		for _, a := range interim.Assignees {
			if k.SameMember(ctx, a, witness) {
				interimID, found = interim.Id, true
				return true
			}
		}
		return false
	})
	if !found {
		return
	}

	cacheCtx, write := sdkCtx.CacheContext()
	if err := k.CompleteInterimDirectly(cacheCtx, interimID, types.AdjudicationDecision_ADJUDICATION_DECISION_UNSPECIFIED,
		fmt.Sprintf("testimony submitted on jury review %d", review.Id)); err != nil {
		sdkCtx.Logger().Error("failed to pay expert testimony", "jury_review_id", review.Id, "interim_id", interimID, "error", err)
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				"expert_testimony_unpaid",
				sdk.NewAttribute("jury_review_id", fmt.Sprintf("%d", review.Id)),
				sdk.NewAttribute("interim_id", fmt.Sprintf("%d", interimID)),
				sdk.NewAttribute("error", err.Error()),
			),
		)
		return
	}
	write()
}
