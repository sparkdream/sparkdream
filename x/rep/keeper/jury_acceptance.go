package keeper

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Accept-or-decline turns a seat drawn by lot into a commitment.
//
// Jurors are conscripted by sortition — nobody volunteers for a specific
// dispute — so penalising a no-show who never agreed to serve punishes an
// accident of the draw. Declining is therefore free and immediate, and the
// consequences attach to *ignoring* the summons: the seat is vacated, redrawn,
// and the silence counts against the juror's participation rate.
//
// The other half of the value is timing. Waiting for the vote deadline to
// discover a juror is absent wastes the entire review window; an explicit
// decline surfaces it in the acceptance window instead, while there is still
// time to seat somebody who will actually read the work.

// jurorSeatGuard resolves a juror's seat on a review and rejects the calls that
// cannot apply: a review already decided, a juror not seated on it, or one who
// has already voted.
func (k Keeper) jurorSeatGuard(ctx context.Context, juryReviewID uint64, juror string) (types.JuryReview, error) {
	review, err := k.GetJuryReview(ctx, juryReviewID)
	if err != nil {
		return types.JuryReview{}, err
	}
	if review.Verdict != types.Verdict_VERDICT_PENDING {
		return types.JuryReview{}, errorsmod.Wrapf(types.ErrJuryReviewResolved,
			"jury review %d already returned %s", juryReviewID, review.Verdict)
	}
	if !slices.Contains(review.Jurors, juror) {
		return types.JuryReview{}, errorsmod.Wrapf(types.ErrNotSeatedJuror,
			"%s is not seated on jury review %d", juror, juryReviewID)
	}
	for _, v := range review.Votes {
		if v != nil && v.Juror == juror {
			return types.JuryReview{}, errorsmod.Wrapf(types.ErrJurorAlreadyVoted,
				"%s has already voted on jury review %d", juror, juryReviewID)
		}
	}
	return review, nil
}

// AcceptJuryDuty records a juror's commitment to vote on a review.
func (k Keeper) AcceptJuryDuty(ctx context.Context, juryReviewID uint64, juror string) error {
	review, err := k.jurorSeatGuard(ctx, juryReviewID, juror)
	if err != nil {
		return err
	}
	if slices.Contains(review.Accepted, juror) {
		return nil // idempotent
	}

	review.Accepted = append(review.Accepted, juror)
	if err := k.JuryReview.Set(ctx, review.Id, review); err != nil {
		return err
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		"jury_duty_accepted",
		sdk.NewAttribute("jury_review_id", fmt.Sprintf("%d", juryReviewID)),
		sdk.NewAttribute("juror", juror),
	))
	return nil
}

// recomputeRequiredVotes re-derives the supermajority threshold from the seated
// list. Every path that adds or removes a seat must call it: RequiredVotes is
// stored, and leaving it stale after a roster change silently changes what the
// jury can conclude. Before this was shared, only the redraw sweep recomputed —
// so a jury thinned by declines kept the original roster's threshold, which
// blocked the uphold direction while leaving the reject direction reachable by
// a rump. That asymmetry was leftover state, not a decision.
func (k Keeper) recomputeRequiredVotes(ctx context.Context, review *types.JuryReview) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	review.RequiredVotes = uint32(params.JurySuperMajority.
		MulInt64(int64(len(review.Jurors))).Ceil().TruncateInt().Uint64())
	return nil
}

// refillJurySeats tries to draw replacements back up to seatsWanted for an
// initiative-challenge review, and returns the addresses seated.
//
// Only initiative-challenge juries can be refilled: content and appeal juries
// are selected by their own routines against targets this review does not
// carry. A pool too thin to replace is not an error — the review proceeds with
// whoever remains, at the correspondingly lower quorum, which the floor in
// TallyJuryVotes keeps from becoming a rump verdict.
func (k Keeper) refillJurySeats(ctx context.Context, review *types.JuryReview, seatsWanted int, exclude ...string) []string {
	if review.InitiativeId == 0 || len(review.Jurors) >= seatsWanted {
		return nil
	}
	initiative, err := k.GetInitiative(ctx, review.InitiativeId)
	if err != nil {
		return nil
	}
	skip := append(append([]string{}, review.Jurors...), exclude...)
	drawn, err := k.SelectJury(ctx, initiative, uint32(seatsWanted-len(review.Jurors)), skip...)
	if err != nil {
		return nil
	}
	return drawn
}

// maxJuryRedraws reads the replacement-round cap, falling back to the shipped
// default if a chain's stored params predate the field.
func (k Keeper) maxJuryRedraws(ctx context.Context) uint32 {
	params, err := k.Params.Get(ctx)
	if err != nil || params.MaxJuryRedraws == 0 {
		return types.DefaultMaxJuryRedraws
	}
	return params.MaxJuryRedraws
}

// DeclineJuryDuty releases a seat so it can be redrawn.
//
// Declining costs the juror nothing and is not recorded as a no-show. That is
// the point: making it free is what keeps the seat-vacating consequences fair
// for jurors who never asked to be drawn, and an early decline is strictly more
// useful to the dispute than a silent absence.
func (k Keeper) DeclineJuryDuty(ctx context.Context, juryReviewID uint64, juror string) error {
	review, err := k.jurorSeatGuard(ctx, juryReviewID, juror)
	if err != nil {
		return err
	}

	seatsWanted := len(review.Jurors)

	if err := k.vacateJurySeat(ctx, &review, juror); err != nil {
		return err
	}

	// A decline is the earliest vacancy signal there is, and therefore the one
	// with the most review time left to act on. Refill immediately rather than
	// waiting for the acceptance sweep, which only runs after the acceptance
	// deadline and cannot vacate at all when jury_size equals MinSeatedJurors.
	replacements := k.refillJurySeats(ctx, &review, seatsWanted, juror)
	review.Jurors = append(review.Jurors, replacements...)
	for _, addr := range replacements {
		if err := k.JuryReviewsByJuror.Set(ctx, collections.Join(addr, review.Id)); err != nil {
			return err
		}
	}
	if len(replacements) > 0 {
		if err := k.RecordJurySeating(ctx, replacements); err != nil {
			return err
		}
	}

	// The seated list changed either way, so the supermajority threshold has to
	// follow it.
	if err := k.recomputeRequiredVotes(ctx, &review); err != nil {
		return err
	}
	if err := k.JuryReview.Set(ctx, review.Id, review); err != nil {
		return err
	}
	// Take the seat back out of the participation denominator. RecordJurySeating
	// counted it when the lot drew them; leaving it counted would make a decline
	// cost the same as silence.
	if err := k.recordJuryDecline(ctx, juror); err != nil {
		return err
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		"jury_duty_declined",
		sdk.NewAttribute("jury_review_id", fmt.Sprintf("%d", juryReviewID)),
		sdk.NewAttribute("juror", juror),
		sdk.NewAttribute("replacements", strings.Join(replacements, ",")),
		sdk.NewAttribute("seated", fmt.Sprintf("%d", len(review.Jurors))),
	))
	return nil
}

// vacateJurySeat removes a juror from a review in memory and drops their
// discovery-index entry. The caller persists the review.
//
// Note this shrinks Jurors, which is the list quorum and RequiredVotes are
// computed from — vacating a seat therefore *lowers* the bar rather than
// raising it, which is the opposite of what adding jurors would do.
func (k Keeper) vacateJurySeat(ctx context.Context, review *types.JuryReview, juror string) error {
	if idx := slices.Index(review.Jurors, juror); idx >= 0 {
		review.Jurors = slices.Delete(slices.Clone(review.Jurors), idx, idx+1)
	}
	if idx := slices.Index(review.Accepted, juror); idx >= 0 {
		review.Accepted = slices.Delete(slices.Clone(review.Accepted), idx, idx+1)
	}
	return k.RemoveJuryReviewFromJurorIndex(ctx, juror, review.Id)
}

// SweepUnansweredJurySeats vacates seats nobody answered and redraws them.
//
// This is the counterpart to the acceptance window: a juror who neither
// accepted nor declined by acceptance_deadline loses the seat, it is recorded
// against their participation rate, and a replacement is drawn.
//
// **Replacement, not reinforcement.** Quorum is len(Jurors)/2+1 and
// RequiredVotes is supermajority x len(Jurors), both computed from the seated
// list — so *adding* jurors to a stalling jury raises the bar it is already
// failing to clear. Seats are swapped one-for-one instead, holding the jury
// size (and therefore quorum) fixed.
//
// Bounded twice over: MaxJuryRedraws rounds per review, and
// maxJuryDeadlinesPerBlock reviews per block.
func (k Keeper) SweepUnansweredJurySeats(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	height := sdkCtx.BlockHeight()

	// Collect first: redrawing mutates the by-juror index and the review, and
	// TallyJuryVotes-style mid-walk mutation is the bug this module keeps
	// re-learning.
	var due []uint64
	k.IterateActiveJuryReviews(ctx, func(_ int64, review types.JuryReview) bool {
		if len(due) >= maxJuryDeadlinesPerBlock {
			return true
		}
		if review.AcceptanceDeadline > 0 && height >= review.AcceptanceDeadline &&
			review.RedrawCount < k.maxJuryRedraws(ctx) {
			due = append(due, review.Id)
		}
		return false
	})

	for _, id := range due {
		if err := k.redrawUnansweredSeats(ctx, id); err != nil {
			sdkCtx.Logger().Error("failed to redraw jury seats", "jury_review_id", id, "error", err)
		}
	}
	return nil
}

func (k Keeper) redrawUnansweredSeats(ctx context.Context, juryReviewID uint64) error {
	review, err := k.GetJuryReview(ctx, juryReviewID)
	if err != nil {
		return err
	}
	if review.Verdict != types.Verdict_VERDICT_PENDING {
		return nil
	}

	answered := make(map[string]struct{}, len(review.Accepted)+len(review.Votes))
	for _, a := range review.Accepted {
		answered[a] = struct{}{}
	}
	for _, v := range review.Votes {
		if v != nil {
			answered[v.Juror] = struct{}{}
		}
	}

	var silent []string
	for _, juror := range review.Jurors {
		if _, ok := answered[juror]; !ok {
			silent = append(silent, juror)
		}
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if len(silent) == 0 {
		// Everyone answered. Stand the acceptance clock down so the sweep stops
		// revisiting this review every block until it resolves.
		review.AcceptanceDeadline = 0
		return k.JuryReview.Set(ctx, review.Id, review)
	}

	seatsWanted := len(review.Jurors)

	// Vacate down to MinSeatedJurors and no further. If replacements cannot be
	// drawn, every vacated seat shrinks the jury — and quorum with it — so an
	// unguarded sweep could leave a single juror holding a quorum of one, able
	// to uphold a challenge and burn the assignee's bond alone. Beyond the
	// floor the silent jurors keep their seats, quorum holds, and the review
	// falls through to its deadline tally rather than being decided by a rump.
	vacatable := len(review.Jurors) - types.MinSeatedJurors
	if vacatable < 0 {
		vacatable = 0
	}
	if len(silent) > vacatable {
		silent = silent[:vacatable]
	}
	if len(silent) == 0 {
		review.AcceptanceDeadline = 0
		return k.JuryReview.Set(ctx, review.Id, review)
	}

	vacating := make(map[string]struct{}, len(silent))
	for _, juror := range silent {
		vacating[juror] = struct{}{}
	}
	keep := make([]string, 0, len(review.Jurors))
	for _, juror := range review.Jurors {
		if _, ok := vacating[juror]; !ok {
			keep = append(keep, juror)
		}
	}
	// Book the silence against the seats actually vacated. RecordJuryNoShows
	// reads the seated list at tally time, so a juror un-seated here would
	// otherwise escape the record entirely — vacancy and record go together.
	// Jurors left seated by the floor above are charged at the tally instead.
	if err := k.recordSilentSeats(ctx, review, silent); err != nil {
		return err
	}
	for _, juror := range silent {
		if err := k.RemoveJuryReviewFromJurorIndex(ctx, juror, review.Id); err != nil {
			return err
		}
	}
	review.Jurors = keep

	// Only initiative-challenge juries can be redrawn: content and appeal
	// juries are selected by their own routines against targets this one does
	// not have. Their seats are simply vacated, which lowers quorum rather than
	// stranding it against absent jurors.
	replacements := []string{}
	if review.InitiativeId != 0 {
		if initiative, iErr := k.GetInitiative(ctx, review.InitiativeId); iErr == nil {
			exclude := append(append([]string{}, keep...), silent...)
			drawn, sErr := k.SelectJury(ctx, initiative, uint32(seatsWanted-len(keep)), exclude...)
			if sErr == nil {
				replacements = drawn
			}
			// A pool too thin to replace is not an error: the review proceeds
			// with the jurors who answered, at the correspondingly lower quorum.
		}
	}
	review.Jurors = append(review.Jurors, replacements...)

	if len(replacements) > 0 {
		if err := k.RecordJurySeating(ctx, replacements); err != nil {
			return err
		}
		for _, juror := range replacements {
			if err := k.JuryReviewsByJuror.Set(ctx, collections.Join(juror, review.Id)); err != nil {
				return err
			}
		}
	}

	review.RedrawCount++
	acceptParams, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	review.AcceptanceDeadline = sdkCtx.BlockHeight() + k.juryAcceptanceWindowBlocks(acceptParams)
	if review.RedrawCount >= k.maxJuryRedraws(ctx) {
		review.AcceptanceDeadline = 0
	}
	// Quorum and RequiredVotes track the seated list, so recompute the latter
	// against whatever the jury now is.
	if err := k.recomputeRequiredVotes(ctx, &review); err != nil {
		return err
	}

	if err := k.JuryReview.Set(ctx, review.Id, review); err != nil {
		return err
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"jury_seats_redrawn",
		sdk.NewAttribute("jury_review_id", fmt.Sprintf("%d", review.Id)),
		sdk.NewAttribute("vacated", fmt.Sprintf("%d", len(silent))),
		sdk.NewAttribute("replacements", fmt.Sprintf("%d", len(replacements))),
		sdk.NewAttribute("seated", fmt.Sprintf("%d", len(review.Jurors))),
		sdk.NewAttribute("redraw_count", fmt.Sprintf("%d", review.RedrawCount)),
	))
	return nil
}

// recordSilentSeats books a no-show against each juror who let the acceptance
// window lapse without a word, applying the same participation floor and
// exclusion the tally path uses.
func (k Keeper) recordSilentSeats(ctx context.Context, review types.JuryReview, silent []string) error {
	stub := types.JuryReview{Id: review.Id, Jurors: silent}
	return k.RecordJuryNoShows(ctx, stub)
}
