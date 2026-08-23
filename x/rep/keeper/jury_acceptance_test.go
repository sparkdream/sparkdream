package keeper_test

import (
	"slices"
	"testing"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// Jurors are conscripted by sortition, so the seat-vacating consequences only
// stay fair if saying "not me" is free and immediate. Declining costs nothing
// and is never recorded as a no-show; ignoring the summons is what costs the
// seat and counts against the participation rate.

func seatReview(t *testing.T, k keeper.Keeper, ctx sdk.Context, id uint64, jurors ...string) types.JuryReview {
	t.Helper()
	review := types.JuryReview{
		Id: id, Jurors: jurors, Verdict: types.Verdict_VERDICT_PENDING,
		AcceptanceDeadline: ctx.BlockHeight() + 1,
	}
	require.NoError(t, k.JuryReview.Set(ctx, id, review))
	require.NoError(t, k.AddJuryReviewToVerdictIndex(ctx, review))
	require.NoError(t, k.AddJuryReviewToJurorIndex(ctx, review))
	require.NoError(t, k.RecordJurySeating(ctx, jurors))
	return review
}

func TestAcceptJuryDuty(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx

	alice := sdk.AccAddress([]byte("acc-alice-address")).String()
	bob := sdk.AccAddress([]byte("acc-bob-address--")).String()
	seatReview(t, k, ctx, 1, alice, bob)

	require.NoError(t, k.AcceptJuryDuty(ctx, 1, alice))
	review, err := k.GetJuryReview(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, []string{alice}, review.Accepted)

	t.Run("idempotent", func(t *testing.T) {
		require.NoError(t, k.AcceptJuryDuty(ctx, 1, alice))
		review, err := k.GetJuryReview(ctx, 1)
		require.NoError(t, err)
		require.Len(t, review.Accepted, 1, "re-accepting must not pad the list")
	})

	t.Run("not a seated juror", func(t *testing.T) {
		stranger := sdk.AccAddress([]byte("acc-stranger-addr")).String()
		require.ErrorIs(t, k.AcceptJuryDuty(ctx, 1, stranger), types.ErrNotSeatedJuror)
	})

	t.Run("review already resolved", func(t *testing.T) {
		review, err := k.GetJuryReview(ctx, 1)
		require.NoError(t, err)
		review.Verdict = types.Verdict_VERDICT_REJECT_CHALLENGE
		require.NoError(t, k.JuryReview.Set(ctx, 1, review))
		require.ErrorIs(t, k.AcceptJuryDuty(ctx, 1, bob), types.ErrJuryReviewResolved)
	})
}

func TestDeclineJuryDutyIsFreeAndImmediate(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx

	alice := sdk.AccAddress([]byte("dec-alice-address")).String()
	bob := sdk.AccAddress([]byte("dec-bob-address--")).String()
	seatReview(t, k, ctx, 1, alice, bob)

	require.NoError(t, k.DeclineJuryDuty(ctx, 1, alice))

	review, err := k.GetJuryReview(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, []string{bob}, review.Jurors, "the seat is released at once")

	// Declining is not a no-show. Charging it would penalise a juror for the
	// accident of being drawn, which is exactly what accept-or-decline exists
	// to avoid.
	p, err := k.GetJuryParticipation(ctx, alice)
	require.NoError(t, err)
	require.Zero(t, p.TotalTimeouts)
	require.Equal(t, uint64(1), p.TotalDeclined)

	// And the summons leaves their queue.
	seatings := 0
	require.NoError(t, k.IterateJuryReviewsByJuror(ctx, alice, func(uint64, types.JuryReview) bool {
		seatings++
		return false
	}))
	require.Zero(t, seatings)
}

func TestVacatingASeatLowersTheBar(t *testing.T) {
	// Quorum is len(Jurors)/2+1, computed on the seated list. Vacating shrinks
	// it — the opposite of adding jurors, which would raise the bar a stalling
	// jury is already failing to clear.
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx

	jurors := []string{}
	for _, n := range []string{"vac-a-address----", "vac-b-address----", "vac-c-address----", "vac-d-address----", "vac-e-address----"} {
		jurors = append(jurors, sdk.AccAddress([]byte(n)).String())
	}
	seatReview(t, k, ctx, 1, jurors...)

	require.Equal(t, 3, 5/2+1, "five seats need three votes")
	require.NoError(t, k.DeclineJuryDuty(ctx, 1, jurors[0]))
	require.NoError(t, k.DeclineJuryDuty(ctx, 1, jurors[1]))

	review, err := k.GetJuryReview(ctx, 1)
	require.NoError(t, err)
	require.Len(t, review.Jurors, 3)
	require.Equal(t, 2, len(review.Jurors)/2+1, "three seats need only two votes")
}

func TestSweepRedrawsUnansweredSeats(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx

	initID, chalID, _ := challengedInitiative(t, k, ctx)
	_ = chalID

	// A pool wide enough that replacements exist.
	pool := []string{}
	for _, n := range []string{"swp-a-address----", "swp-b-address----", "swp-c-address----",
		"swp-d-address----", "swp-e-address----", "swp-f-address----", "swp-g-address----"} {
		addr := sdk.AccAddress([]byte(n))
		mkFundedMember(t, k, ctx, addr, 0)
		m, err := k.GetMember(ctx, addr)
		require.NoError(t, err)
		m.ReputationScores = map[string]string{"tag": "500.0"}
		require.NoError(t, k.Member.Set(ctx, addr.String(), m))
		pool = append(pool, addr.String())
	}

	seated := pool[:5]
	review := types.JuryReview{
		Id: 99, InitiativeId: initID, Jurors: seated,
		Verdict:            types.Verdict_VERDICT_PENDING,
		AcceptanceDeadline: ctx.BlockHeight() + 10,
	}
	require.NoError(t, k.JuryReview.Set(ctx, review.Id, review))
	require.NoError(t, k.AddJuryReviewToVerdictIndex(ctx, review))
	require.NoError(t, k.AddJuryReviewToJurorIndex(ctx, review))
	require.NoError(t, k.RecordJurySeating(ctx, seated))

	// One answers, two go silent.
	require.NoError(t, k.AcceptJuryDuty(ctx, review.Id, seated[0]))

	// Past the acceptance window.
	swept := ctx.WithBlockHeight(ctx.BlockHeight() + 11)
	require.NoError(t, k.SweepUnansweredJurySeats(swept))

	after, err := k.GetJuryReview(swept, review.Id)
	require.NoError(t, err)
	require.Len(t, after.Jurors, 5, "seats are replaced one-for-one, not added to")
	require.Contains(t, after.Jurors, seated[0], "the juror who answered keeps their seat")
	require.Equal(t, uint32(1), after.RedrawCount)

	// Two of the four silent seats can be vacated before MinSeatedJurors stops
	// the sweep; those two are recorded and replaced.
	vacated := 0
	for _, silent := range seated[1:] {
		if !slices.Contains(after.Jurors, silent) {
			vacated++
			p, err := k.GetJuryParticipation(swept, silent)
			require.NoError(t, err)
			require.Equal(t, uint64(1), p.TotalTimeouts)
		}
	}
	require.Equal(t, 2, vacated, "vacating stops at the MinSeatedJurors floor")
}

func TestRedrawIsBounded(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx

	initID, _, _ := challengedInitiative(t, k, ctx)
	juror := sdk.AccAddress([]byte("bnd-juror-address")).String()

	review := types.JuryReview{
		Id: 7, InitiativeId: initID, Jurors: []string{juror},
		Verdict:            types.Verdict_VERDICT_PENDING,
		AcceptanceDeadline: ctx.BlockHeight() + 1,
		RedrawCount:        types.DefaultMaxJuryRedraws,
	}
	require.NoError(t, k.JuryReview.Set(ctx, review.Id, review))
	require.NoError(t, k.AddJuryReviewToVerdictIndex(ctx, review))

	swept := ctx.WithBlockHeight(ctx.BlockHeight() + 5)
	require.NoError(t, k.SweepUnansweredJurySeats(swept))

	after, err := k.GetJuryReview(swept, review.Id)
	require.NoError(t, err)
	require.Equal(t, types.DefaultMaxJuryRedraws, after.RedrawCount,
		"a review at the redraw cap is left to its deadline tally")
	require.Len(t, after.Jurors, 1, "and its seats are untouched")
}

func TestJurorRewardFloorsForNonInitiativeDisputes(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx

	juror := sdk.AccAddress([]byte("flr-juror-address"))
	mkFundedMember(t, k, ctx, juror, 0)

	// Content challenges and moderation appeals have no initiative budget to
	// scale against.
	review := types.JuryReview{
		Id: 1, ContentChallengeId: 5, Jurors: []string{juror.String()},
		Votes: []*types.JurorVote{{Juror: juror.String()}},
	}
	require.NoError(t, k.RewardJurors(ctx, review))

	got, err := k.GetMember(ctx, juror)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(types.DefaultMinJurorReward).String(), got.DreamBalance.String())
}

// Declining has to be free in the participation record, not just free of an
// immediate penalty. RecordJurySeating counts a seat the moment the lot draws
// it, so a declined seat left in the denominator makes declining cost exactly
// as much as silence: three declines then one genuine miss reads as 0/4 rather
// than 0/1, and excludes a juror on their first actual lapse.
func TestDecliningDoesNotLoadTheParticipationDenominator(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx

	juror := sdk.AccAddress([]byte("dnm-juror-address")).String()

	for i := uint64(1); i <= 3; i++ {
		seatReview(t, k, ctx, i, juror)
		require.NoError(t, k.DeclineJuryDuty(ctx, i, juror))
	}

	p, err := k.GetJuryParticipation(ctx, juror)
	require.NoError(t, err)
	require.Equal(t, uint64(3), p.TotalAssigned)
	require.Equal(t, uint64(3), p.TotalDeclined)
	require.Zero(t, p.TotalTimeouts, "a decline is not a no-show")

	// One genuine miss. Judged on the one seat they actually held, so the
	// sample is still below MinJuryAssignmentsForRate and nothing triggers.
	seatReview(t, k, ctx, 4, juror)
	require.NoError(t, k.RecordJuryNoShows(ctx, types.JuryReview{Id: 4, Jurors: []string{juror}}))

	p, err = k.GetJuryParticipation(ctx, juror)
	require.NoError(t, err)
	require.Equal(t, uint64(1), p.TotalTimeouts)
	// Answered 3 of 4 (three declines), ignored one — so 0.75. The point is the
	// contrast: had declines counted as silence this would be 0/4, floored to
	// MinJurorSelectionWeight, and declining would cost as much as ignoring.
	require.Equal(t, 0.75, k.JurorResponsivenessWeight(ctx, juror))
	require.Greater(t, k.JurorResponsivenessWeight(ctx, juror), types.DefaultMinJurorSelectionWeight)
}

func TestSerialDeclinerKeepsFullStanding(t *testing.T) {
	// A juror who hands every seat back is behaving correctly — the seat is
	// redrawn and nobody is stalled — so they keep full selection weight.
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx

	juror := sdk.AccAddress([]byte("srl-juror-address")).String()
	for i := uint64(1); i <= 10; i++ {
		seatReview(t, k, ctx, i, juror)
		require.NoError(t, k.DeclineJuryDuty(ctx, i, juror))
	}

	p, err := k.GetJuryParticipation(ctx, juror)
	require.NoError(t, err)
	require.Equal(t, uint64(10), p.TotalDeclined)
	require.Zero(t, p.TotalTimeouts)
	require.Equal(t, 1.0, k.JurorResponsivenessWeight(ctx, juror))
}

// Quorum is len(jurors)/2+1, so an unguarded sweep could vacate a five-seat
// jury down to one — giving that juror a quorum of one, enough to uphold a
// challenge and burn the assignee's bond alone.
func TestVacatingStopsAtTheMinimumJury(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx

	// No eligible pool, so no replacements can be drawn and every vacated seat
	// shrinks the jury.
	initID, _, _ := challengedInitiative(t, k, ctx)

	jurors := []string{}
	for _, n := range []string{"mns-a-address----", "mns-b-address----", "mns-c-address----", "mns-d-address----", "mns-e-address----"} {
		jurors = append(jurors, sdk.AccAddress([]byte(n)).String())
	}
	review := types.JuryReview{
		Id: 55, InitiativeId: initID, Jurors: jurors,
		Verdict:            types.Verdict_VERDICT_PENDING,
		AcceptanceDeadline: ctx.BlockHeight() + 1,
	}
	require.NoError(t, k.JuryReview.Set(ctx, review.Id, review))
	require.NoError(t, k.AddJuryReviewToVerdictIndex(ctx, review))
	require.NoError(t, k.RecordJurySeating(ctx, jurors))

	// Nobody answers at all.
	swept := ctx.WithBlockHeight(ctx.BlockHeight() + 5)
	require.NoError(t, k.SweepUnansweredJurySeats(swept))

	after, err := k.GetJuryReview(swept, review.Id)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(after.Jurors), types.MinSeatedJurors,
		"a jury must never shrink to a rump that can decide alone")
	require.GreaterOrEqual(t, len(after.Jurors)/2+1, 2,
		"quorum must stay above one")
}
