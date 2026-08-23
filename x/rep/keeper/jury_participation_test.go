package keeper_test

import (
	"testing"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// Ignoring a summons is deliberately not penalised. The adjudication terminal
// path resolves an inconclusive jury safely and the redraw sweep replaces an
// unanswered seat within the acceptance window, so a no-show costs a few hours
// of a week-long review. Pricing that would oblige every eligible member to
// monitor the chain for an event that reaches them roughly once a year.
//
// The record instead feeds selection weight, and the one penalised case is
// accepting a summons and then abandoning it.

func recordRound(t *testing.T, k keeper.Keeper, ctx sdk.Context, jurors, voters []string) {
	t.Helper()
	require.NoError(t, k.RecordJurySeating(ctx, jurors))
	for _, v := range voters {
		require.NoError(t, k.RecordJuryVote(ctx, v))
	}
	votes := make([]*types.JurorVote, 0, len(voters))
	for _, v := range voters {
		votes = append(votes, &types.JurorVote{Juror: v})
	}
	require.NoError(t, k.RecordJuryNoShows(ctx, types.JuryReview{Id: 1, Jurors: jurors, Votes: votes}))
}

func TestJurorParticipationIsRecorded(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx

	present := sdk.AccAddress([]byte("jp-present-juror-")).String()
	absent := sdk.AccAddress([]byte("jp-absent-juror--")).String()

	recordRound(t, k, ctx, []string{present, absent}, []string{present})

	p, err := k.GetJuryParticipation(ctx, present)
	require.NoError(t, err)
	require.Equal(t, uint64(1), p.TotalAssigned)
	require.Equal(t, uint64(1), p.TotalVoted)
	require.Zero(t, p.TotalTimeouts)

	a, err := k.GetJuryParticipation(ctx, absent)
	require.NoError(t, err)
	require.Equal(t, uint64(1), a.TotalAssigned)
	require.Zero(t, a.TotalVoted)
	require.Equal(t, uint64(1), a.TotalTimeouts)
	require.Zero(t, a.TotalAbandoned, "they never accepted, so nothing was broken")
}

func TestIgnoringSummonsesIsNotPenalised(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx

	juror := sdk.AccAddress([]byte("jp-silent-juror--"))
	mkFundedMember(t, k, ctx, juror, 0)
	before, err := k.GetMember(ctx, juror)
	require.NoError(t, err)

	for i := 0; i < 6; i++ {
		recordRound(t, k, ctx, []string{juror.String()}, nil)
	}

	p, err := k.GetJuryParticipation(ctx, juror.String())
	require.NoError(t, err)
	require.Equal(t, uint64(6), p.TotalTimeouts, "the record is kept")

	after, err := k.GetMember(ctx, juror)
	require.NoError(t, err)
	require.Equal(t, before.ReputationScores["tag"], after.ReputationScores["tag"],
		"but no reputation is charged for silence")
	require.Equal(t, before.DreamBalance.String(), after.DreamBalance.String())
}

func TestResponsivenessDiscountsSelectionWeightWithoutExcluding(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx

	responsive := sdk.AccAddress([]byte("jp-responsive----")).String()
	silent := sdk.AccAddress([]byte("jp-unresponsive--")).String()
	fresh := sdk.AccAddress([]byte("jp-fresh-juror---")).String()

	for i := 0; i < 5; i++ {
		recordRound(t, k, ctx, []string{responsive}, []string{responsive})
		recordRound(t, k, ctx, []string{silent}, nil)
	}

	require.Equal(t, 1.0, k.JurorResponsivenessWeight(ctx, responsive))
	require.Equal(t, 1.0, k.JurorResponsivenessWeight(ctx, fresh),
		"no record yet means full weight — one missed summons is not evidence")

	got := k.JurorResponsivenessWeight(ctx, silent)
	require.Less(t, got, 1.0, "a juror who never answers is drawn less often")
	require.GreaterOrEqual(t, got, types.DefaultMinJurorSelectionWeight,
		"but never to zero — that would be exclusion in all but name, with no way back")
}

func TestDeclinesCountAsAnswering(t *testing.T) {
	// A prompt decline frees the seat, which is exactly the behaviour the lot
	// wants to keep drawing. It must not depress selection weight.
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx

	juror := sdk.AccAddress([]byte("jp-decliner------")).String()
	for i := uint64(1); i <= 5; i++ {
		seatReview(t, k, ctx, i, juror)
		require.NoError(t, k.DeclineJuryDuty(ctx, i, juror))
	}

	require.Equal(t, 1.0, k.JurorResponsivenessWeight(ctx, juror))
}

func TestAbandonedSeatIsPenalisedInReputation(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx

	initID, _, _ := challengedInitiative(t, k, ctx)

	committed := sdk.AccAddress([]byte("jp-committed-jrr-"))
	silent := sdk.AccAddress([]byte("jp-never-said----"))
	for _, a := range []sdk.AccAddress{committed, silent} {
		mkFundedMember(t, k, ctx, a, 0)
	}

	review := types.JuryReview{
		Id: 42, InitiativeId: initID,
		Jurors:   []string{committed.String(), silent.String()},
		Accepted: []string{committed.String()},
	}
	require.NoError(t, k.RecordJuryNoShows(ctx, review))

	params, err := k.Params.Get(ctx)
	require.NoError(t, err)

	// The juror who took the seat and held it empty is charged. Declining was
	// free and immediate, so this is a broken commitment, not bad luck.
	c, err := k.GetMember(ctx, committed)
	require.NoError(t, err)
	expected := math.LegacyNewDec(100).Sub(params.AbandonedJurySeatPenalty)
	require.Equal(t, expected.String(), c.ReputationScores["tag"])

	cp, err := k.GetJuryParticipation(ctx, committed.String())
	require.NoError(t, err)
	require.Equal(t, uint64(1), cp.TotalAbandoned)

	// The one who never answered is not.
	sm, err := k.GetMember(ctx, silent)
	require.NoError(t, err)
	require.Equal(t, "100.0", sm.ReputationScores["tag"])
	sp, err := k.GetJuryParticipation(ctx, silent.String())
	require.NoError(t, err)
	require.Zero(t, sp.TotalAbandoned)
}

func TestAbandonmentPenaltyCanBeDisabled(t *testing.T) {
	params := types.DefaultParams()
	params.AbandonedJurySeatPenalty = math.LegacyZeroDec()
	f := initFixture(t, WithCustomParams(params))
	k, ctx := f.keeper, f.ctx

	initID, _, _ := challengedInitiative(t, k, ctx)
	juror := sdk.AccAddress([]byte("jp-disabled-pen--"))
	mkFundedMember(t, k, ctx, juror, 0)

	require.NoError(t, k.RecordJuryNoShows(ctx, types.JuryReview{
		Id: 1, InitiativeId: initID,
		Jurors: []string{juror.String()}, Accepted: []string{juror.String()},
	}))

	m, err := k.GetMember(ctx, juror)
	require.NoError(t, err)
	require.Equal(t, "100.0", m.ReputationScores["tag"])
}
