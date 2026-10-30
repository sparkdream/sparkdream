package keeper

import (
	"context"
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/rep/types"
)

// ExternalJuryCasePrefix marks jury reviews opened by another module through
// CreateSeatedAppeal whose verdict that module reads back itself (x/service's
// "service.slash" and "service.controller_transfer" cases). x/rep only seats,
// tallies and pays the jury; it applies nothing.
const ExternalJuryCasePrefix = "service."

// isExternalJuryCase reports whether review is a CreateSeatedAppeal case.
// Keyed on the case type stored in ChallengerClaim: ChallengeId 0 is a valid
// initiative-challenge id, so it cannot be the discriminator.
func isExternalJuryCase(review types.JuryReview) bool {
	return strings.HasPrefix(review.ChallengerClaim, ExternalJuryCasePrefix)
}

// CreateSeatedAppeal opens a jury case for another module and seats its jury
// immediately, the way gov-action appeals and migration objections do.
// CreateAppealInitiative alone writes a review with no jurors that nothing
// ever tallies, so a case opened that way can never reach a verdict.
//
// The review is added to the PENDING verdict index, so it is tallied as soon
// as the required votes are in (SubmitJurorVote) or, at the latest, at
// deadline (a block height) by ResolveExpiredChallengeJuryReviews; a deadline
// tally below quorum is INCONCLUSIVE. TallyJuryVotes records the verdict,
// charges no-shows and pays jurors, and applies nothing else: the calling
// module reads the verdict through the review.
//
// excludes are the parties to the case (accused, accuser, controller, …);
// they and every address of the same member are kept off the jury. Seating
// is best-effort like the other callers: if no eligible juror exists the
// case still opens and resolves INCONCLUSIVE at its deadline.
func (k Keeper) CreateSeatedAppeal(ctx context.Context, initiativeType string, payload []byte, deadline int64, excludes []string) (uint64, error) {
	if !strings.HasPrefix(initiativeType, ExternalJuryCasePrefix) {
		return 0, fmt.Errorf("seated appeal type %q must start with %q", initiativeType, ExternalJuryCasePrefix)
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	reviewID, err := k.CreateAppealInitiative(ctx, initiativeType, payload, deadline)
	if err != nil {
		return 0, err
	}
	review, err := k.GetJuryReview(ctx, reviewID)
	if err != nil {
		return 0, err
	}

	if jurors, jErr := k.selectModerationAppealJury(ctx, reviewID, excludes); jErr != nil {
		sdkCtx.Logger().Warn("failed to seat external jury case", "review_id", reviewID, "type", initiativeType, "error", jErr)
	} else if len(jurors) > 0 {
		review.Jurors = jurors
		review.RequiredVotes = commontypes.Saturate[uint32](k.appealRequiredVotes(ctx, len(jurors)))
		if err := k.JuryReview.Set(ctx, reviewID, review); err != nil {
			return 0, err
		}
		if err := k.AddJuryReviewToJurorIndex(ctx, review); err != nil {
			sdkCtx.Logger().Warn("failed to index external jury seating", "review_id", reviewID, "error", err)
		} else if err := k.RecordJurySeating(ctx, jurors); err != nil {
			sdkCtx.Logger().Warn("failed to record external jury seating", "review_id", reviewID, "error", err)
		}
	}

	if err := k.AddJuryReviewToVerdictIndex(ctx, review); err != nil {
		return 0, err
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("external_jury_case_seated",
		sdk.NewAttribute("review_id", fmt.Sprintf("%d", reviewID)),
		sdk.NewAttribute("type", initiativeType),
		sdk.NewAttribute("jurors", fmt.Sprintf("%d", len(review.Jurors))),
	))
	return reviewID, nil
}
