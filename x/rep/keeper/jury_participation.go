package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Juror service accounting.
//
// Jurors are drawn by lot, cannot be pre-selected by the party under review,
// and are paid a flat participation reward for voting regardless of how they
// vote. That last property is deliberate — it is what keeps a juror indifferent
// to the outcome.
//
// **Ignoring a summons is not punished.** It was, briefly: an unanswered seat
// cost the juror a participation-rate mark and eventually a timed exclusion
// from selection. That made sense while a lost quorum could freeze an
// initiative permanently, but two changes removed the harm it was pricing — the
// adjudication terminal path resolves an inconclusive jury safely, and the
// redraw sweep replaces an unanswered seat within the acceptance window. What
// remains of a no-show is a few hours of delay in a week-long review, and
// pricing that would require every eligible member to monitor the chain
// continuously for an event that reaches them roughly once a year. Under broad
// sortition, non-response is the expected default of a population that never
// volunteered; penalising the default punishes an accident of the draw.
//
// The record is still kept, and still does work — as **selection weight**. A
// juror who never answers is drawn less often, which fixes the pool-efficiency
// problem exclusion was really solving, without taking anything away from
// anyone and without ever removing an address from the lot.
//
// What *is* penalised is abandoning a seat you accepted; see
// RecordJuryNoShows. Accepting is voluntary and declining is free, so an
// accepted-then-abandoned seat is a broken commitment rather than an accident.

// getOrInitJuryParticipation returns the juror's record, or a zeroed one.
func (k Keeper) getOrInitJuryParticipation(ctx context.Context, juror string) types.JuryParticipation {
	p, err := k.JuryParticipation.Get(ctx, juror)
	if err != nil {
		return types.JuryParticipation{Juror: juror}
	}
	return p
}

// RecordJurySeating stamps a seating on every juror drawn for a review. Called
// at the point the jury is written to the review, not inside selection, so a
// jury that is drawn but never seated does not count against anyone.
func (k Keeper) RecordJurySeating(ctx context.Context, jurors []string) error {
	now := sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
	for _, juror := range jurors {
		if juror == "" {
			continue
		}
		p := k.getOrInitJuryParticipation(ctx, juror)
		p.TotalAssigned++
		p.LastAssignedAt = now
		if err := k.JuryParticipation.Set(ctx, juror, p); err != nil {
			return err
		}
	}
	return nil
}

// RecordJuryVote credits a juror for voting.
func (k Keeper) RecordJuryVote(ctx context.Context, juror string) error {
	p := k.getOrInitJuryParticipation(ctx, juror)
	p.TotalVoted++
	return k.JuryParticipation.Set(ctx, juror, p)
}

// recordJuryDecline notes a seat handed back, so it can be excluded from the
// responsiveness denominator and counted as an answer rather than a silence.
func (k Keeper) recordJuryDecline(ctx context.Context, juror string) error {
	p := k.getOrInitJuryParticipation(ctx, juror)
	p.TotalDeclined++
	return k.JuryParticipation.Set(ctx, juror, p)
}

// RecordJuryNoShows records every seated juror who did not vote, and penalises
// the ones who had accepted.
//
// Called from TallyJuryVotes, the single choke point every resolution path
// funnels through — the supermajority-triggered tally, the challenge deadline
// sweep, and the appeal timeout — and from the redraw sweep for seats vacated
// there. Putting it at those two points means a new resolution path cannot
// forget it.
//
// Silence from a juror who never accepted costs nothing but selection weight.
// Silence from one who *did* accept is a broken commitment: they were drawn,
// they were told, declining was free and immediate, and they chose to take the
// seat and then hold it empty until the deadline. That is the case worth
// discouraging, and it is charged in reputation — the currency that qualifies a
// juror in the first place, so the penalty is self-limiting: enough abandoned
// seats and they fall below MinJurorReputation and stop being drawn for that
// tag at all.
func (k Keeper) RecordJuryNoShows(ctx context.Context, juryReview types.JuryReview) error {
	voted := make(map[string]struct{}, len(juryReview.Votes))
	for _, v := range juryReview.Votes {
		if v != nil {
			voted[v.Juror] = struct{}{}
		}
	}
	accepted := make(map[string]struct{}, len(juryReview.Accepted))
	for _, a := range juryReview.Accepted {
		accepted[a] = struct{}{}
	}

	for _, juror := range juryReview.Jurors {
		if juror == "" {
			continue
		}
		if _, ok := voted[juror]; ok {
			continue
		}

		p := k.getOrInitJuryParticipation(ctx, juror)
		p.TotalTimeouts++
		if _, committed := accepted[juror]; committed {
			p.TotalAbandoned++
		}
		if err := k.JuryParticipation.Set(ctx, juror, p); err != nil {
			return err
		}

		if _, committed := accepted[juror]; committed {
			if err := k.penaliseAbandonedSeat(ctx, juryReview, juror); err != nil {
				return err
			}
		}
	}
	return nil
}

// penaliseAbandonedSeat deducts reputation from a juror who accepted a summons
// and then let it lapse.
//
// Charged against the tags the dispute is about, so it lands in the same
// domain that qualified them for the seat. A review with no initiative
// (content challenges, moderation appeals) has no tags to charge and is
// skipped rather than charged arbitrarily.
func (k Keeper) penaliseAbandonedSeat(ctx context.Context, juryReview types.JuryReview, juror string) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	penalty := params.AbandonedJurySeatPenalty
	if penalty.IsNil() || !penalty.IsPositive() {
		return nil
	}
	if juryReview.InitiativeId == 0 {
		return nil
	}
	initiative, err := k.GetInitiative(ctx, juryReview.InitiativeId)
	if err != nil || len(initiative.Tags) == 0 {
		return nil
	}
	jurorAddr, err := sdk.AccAddressFromBech32(juror)
	if err != nil {
		return nil
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	for _, tag := range initiative.Tags {
		// Best-effort per tag: a juror who has been zeroed or deactivated since
		// accepting cannot be charged, and that must not fail the tally that is
		// resolving the dispute.
		if err := k.DeductReputation(ctx, jurorAddr, tag, penalty); err != nil {
			sdkCtx.Logger().Debug("could not charge abandoned jury seat",
				"juror", juror, "tag", tag, "error", err)
		}
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"jury_seat_abandoned",
		sdk.NewAttribute("jury_review_id", fmt.Sprintf("%d", juryReview.Id)),
		sdk.NewAttribute("juror", juror),
		sdk.NewAttribute("reputation_penalty", penalty.String()),
	))
	return nil
}

// JurorResponsivenessWeight is the multiplier applied to a juror's selection
// weight, reflecting how often they answer a summons at all — by voting or by
// declining. Both count: a prompt decline frees the seat and is exactly the
// behaviour the lot wants to keep drawing.
//
// This replaces exclusion. A juror who never answers is drawn less often rather
// than removed, so nobody loses eligibility, broad sortition is preserved, and
// an address can always earn its weight back — which an excluded one could not,
// since it would never be drawn again to demonstrate anything.
//
// Below MinJurySeatingsForWeighting seatings there is no meaningful record and
// the juror is drawn at full weight.
func (k Keeper) JurorResponsivenessWeight(ctx context.Context, juror string) float64 {
	p, err := k.JuryParticipation.Get(ctx, juror)
	if err != nil {
		return 1
	}

	// Both knobs are governance/committee tunable, and both were guesses when
	// first written — they are meant to be fitted against observed response
	// rates. Fall back to the shipped defaults rather than to zero if a chain's
	// stored params predate them, since a zero floor would exclude a
	// non-responder in all but name.
	minSeatings := types.DefaultMinJurySeatingsForWeighting
	floor := types.DefaultMinJurorSelectionWeight
	if params, pErr := k.Params.Get(ctx); pErr == nil {
		if params.MinJurySeatingsForWeighting > 0 {
			minSeatings = params.MinJurySeatingsForWeighting
		}
		if !params.MinJurorSelectionWeight.IsNil() && params.MinJurorSelectionWeight.IsPositive() {
			if f, fErr := params.MinJurorSelectionWeight.Float64(); fErr == nil {
				floor = f
			}
		}
	}

	if p.TotalAssigned < minSeatings {
		return 1
	}
	answered := p.TotalVoted + p.TotalDeclined
	if answered >= p.TotalAssigned {
		return 1
	}
	weight := float64(answered) / float64(p.TotalAssigned)
	if weight < floor {
		return floor
	}
	return weight
}

// GetJuryParticipation exposes a juror's record for queries and tests.
func (k Keeper) GetJuryParticipation(ctx context.Context, juror string) (types.JuryParticipation, error) {
	p, err := k.JuryParticipation.Get(ctx, juror)
	if err != nil {
		if err == collections.ErrNotFound {
			return types.JuryParticipation{Juror: juror}, nil
		}
		return types.JuryParticipation{}, err
	}
	return p, nil
}
