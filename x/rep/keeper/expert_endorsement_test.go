package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

// REP-E-103 (residual): one seated juror can no longer direct expert pay on
// their own. An invitation is an engagement only once a second juror who has
// answered the summons endorses it; until then the expert cannot testify and
// the verdict expires the invitation unpaid.
func TestREPE103_SecondJurorEndorsement(t *testing.T) {
	t.Run("unendorsed expert cannot testify", func(t *testing.T) {
		e := newExpertWitnessFixture(t)
		_, err := e.invite(e.juror, e.expert)
		require.NoError(t, err)
		require.ErrorIs(t, e.testify(e.expert, sampleOpinion, "criteria"), types.ErrExpertNotEndorsed)
	})

	t.Run("the inviter cannot endorse their own invitation", func(t *testing.T) {
		e := newExpertWitnessFixture(t)
		_, err := e.invite(e.juror, e.expert)
		require.NoError(t, err)
		// Even after accepting jury duty the inviter stays one juror.
		require.NoError(t, e.keeper.AcceptJuryDuty(e.ctx, e.reviewID, e.juror.String()))
		require.ErrorIs(t, e.endorse(e.juror, e.expert), types.ErrInvalidEndorsement)
		require.ErrorIs(t, e.testify(e.expert, sampleOpinion, "criteria"), types.ErrExpertNotEndorsed)
	})

	t.Run("a non-juror cannot endorse", func(t *testing.T) {
		e := newExpertWitnessFixture(t)
		_, err := e.invite(e.juror, e.expert)
		require.NoError(t, err)
		for _, who := range []sdk.AccAddress{e.nob, e.challenger, e.assignee, e.expert} {
			require.ErrorIs(t, e.endorse(who, e.expert), types.ErrNotSeatedJuror, who.String())
		}
		require.ErrorIs(t, e.testify(e.expert, sampleOpinion, "criteria"), types.ErrExpertNotEndorsed)
	})

	t.Run("a seated juror who has not answered the summons cannot endorse; a vote counts as answering", func(t *testing.T) {
		e := newExpertWitnessFixture(t)
		k, ctx := e.keeper, e.ctx
		juror3 := sdk.AccAddress([]byte("juror3______________")[:20])
		setExpertMember(t, e.fixture, juror3, types.TrustLevel_TRUST_LEVEL_ESTABLISHED, map[string]string{"tag": "50.0"})
		review, err := k.JuryReview.Get(ctx, e.reviewID)
		require.NoError(t, err)
		review.Jurors = append(review.Jurors, juror3.String())
		review.RequiredVotes = 3 // keep the review open through the vote below
		require.NoError(t, k.JuryReview.Set(ctx, e.reviewID, review))

		_, err = e.invite(e.juror, e.expert)
		require.NoError(t, err)
		require.ErrorIs(t, e.endorse(juror3, e.expert), types.ErrInvalidEndorsement, "seated but silent")

		require.NoError(t, k.SubmitJurorVote(ctx, e.reviewID, juror3, nil, types.Verdict_VERDICT_REJECT_CHALLENGE, math.LegacyMustNewDecFromStr("0.9"), "looks fine"))
		require.NoError(t, e.endorse(juror3, e.expert), "a cast vote is an answered summons")
		review, err = k.JuryReview.Get(ctx, e.reviewID)
		require.NoError(t, err)
		require.Equal(t, []string{juror3.String()}, review.ExpertEndorsers)
	})

	t.Run("an invitation is endorsed once, and only one that exists", func(t *testing.T) {
		e := newExpertWitnessFixture(t)
		require.ErrorIs(t, e.endorse(e.juror2, e.expert), types.ErrInvalidEndorsement, "not invited")
		_, err := e.invite(e.juror, e.expert)
		require.NoError(t, err)
		require.NoError(t, e.endorse(e.juror2, e.expert))
		require.ErrorIs(t, e.endorse(e.juror2, e.expert), types.ErrInvalidEndorsement, "already endorsed")
	})

	t.Run("endorsed expert testifies and is paid at the verdict", func(t *testing.T) {
		e := newExpertWitnessFixture(t)
		k, ctx := e.keeper, e.ctx
		interimID := e.inviteEndorsed(t, e.expert)
		before := e.balance(t, e.expert)

		ctx = ctx.WithEventManager(sdk.NewEventManager())
		e.ctx = ctx
		require.NoError(t, e.testify(e.expert, sampleOpinion, "criteria"))
		require.NoError(t, k.TallyJuryVotes(ctx, e.reviewID))

		params, err := k.Params.Get(ctx)
		require.NoError(t, err)
		require.Equal(t, params.ComplexComplexityBudget.String(), e.balance(t, e.expert).Sub(before).String())
		require.Equal(t, types.InterimStatus_INTERIM_STATUS_COMPLETED, e.interimStatus(t, interimID))
	})

	t.Run("unendorsed invitation is expired unpaid at the verdict", func(t *testing.T) {
		e := newExpertWitnessFixture(t)
		k, ctx := e.keeper, e.ctx
		resp, err := e.invite(e.juror, e.expert)
		require.NoError(t, err)
		before := e.balance(t, e.expert)

		require.NoError(t, k.TallyJuryVotes(ctx, e.reviewID))
		require.Equal(t, types.InterimStatus_INTERIM_STATUS_EXPIRED, e.interimStatus(t, resp.InterimId))
		require.True(t, e.balance(t, e.expert).Equal(before), "nothing paid")
		in, err := k.GetInterim(ctx, resp.InterimId)
		require.NoError(t, err)
		require.Contains(t, in.CompletionNotes, "never endorsed")
	})

	t.Run("a testimony on record never pays an unendorsed invitation", func(t *testing.T) {
		// Belt and braces for the settlement path: a review whose state
		// carries a testimony without an endorsement (never writable through
		// the msg server) still pays nothing.
		e := newExpertWitnessFixture(t)
		k, ctx := e.keeper, e.ctx
		resp, err := e.invite(e.juror, e.expert)
		require.NoError(t, err)
		review, err := k.JuryReview.Get(ctx, e.reviewID)
		require.NoError(t, err)
		review.Testimonies = append(review.Testimonies, &types.ExpertTestimony{Expert: e.expert.String(), Opinion: sampleOpinion})
		require.NoError(t, k.JuryReview.Set(ctx, e.reviewID, review))
		before := e.balance(t, e.expert)
		require.NoError(t, k.TallyJuryVotes(ctx, e.reviewID))
		require.Equal(t, types.InterimStatus_INTERIM_STATUS_EXPIRED, e.interimStatus(t, resp.InterimId))
		require.True(t, e.balance(t, e.expert).Equal(before))
	})

	t.Run("endorsement needs a pending review before its deadline", func(t *testing.T) {
		e := newExpertWitnessFixture(t)
		k := e.keeper
		_, err := e.invite(e.juror, e.expert)
		require.NoError(t, err)
		review, err := k.JuryReview.Get(e.ctx, e.reviewID)
		require.NoError(t, err)

		late := e.ctx.WithBlockHeight(review.Deadline)
		_, err = e.ms.EndorseExpertWitness(late, &types.MsgEndorseExpertWitness{
			Juror: e.juror2.String(), JuryReviewId: e.reviewID, Expert: e.expert.String(),
		})
		require.ErrorIs(t, err, types.ErrNotInitiativeReview, "past the deadline")

		review.Verdict = types.Verdict_VERDICT_REJECT_CHALLENGE
		require.NoError(t, k.JuryReview.Set(e.ctx, e.reviewID, review))
		require.ErrorIs(t, e.endorse(e.juror2, e.expert), types.ErrJuryReviewResolved)
	})

	t.Run("endorsement emits an event naming both jurors", func(t *testing.T) {
		e := newExpertWitnessFixture(t)
		e.ctx = e.ctx.WithEventManager(sdk.NewEventManager())
		_, err := e.invite(e.juror, e.expert)
		require.NoError(t, err)
		require.NoError(t, e.endorse(e.juror2, e.expert))
		var found bool
		for _, ev := range e.ctx.EventManager().Events() {
			if ev.Type != "expert_witness_endorsed" {
				continue
			}
			found = true
			attrs := map[string]string{}
			for _, a := range ev.Attributes {
				attrs[a.Key] = a.Value
			}
			require.Equal(t, e.juror2.String(), attrs["juror"])
			require.Equal(t, e.juror.String(), attrs["inviter"])
			require.Equal(t, e.expert.String(), attrs["expert"])
		}
		require.True(t, found, "expert_witness_endorsed event")
	})
}

// Genesis refuses an expert list shape the handlers can never write: lists
// longer than expert_witnesses, or an endorsement by the inviter.
func TestGenesisExpertEndorsementShape(t *testing.T) {
	base := func() *types.GenesisState {
		gs := types.DefaultGenesis()
		gs.JuryReviewCount = 2
		gs.JuryReviewList = []types.JuryReview{{
			Id: 1, ExpertWitnesses: []string{"e"}, ExpertInterimIds: []uint64{1},
			ExpertInviters: []string{"j1"}, ExpertEndorsers: []string{"j2"},
		}}
		return gs
	}
	require.NoError(t, base().Validate())

	gs := base()
	gs.JuryReviewList[0].ExpertEndorsers = []string{"j1"}
	require.Error(t, gs.Validate(), "endorsed by the inviter")

	gs = base()
	gs.JuryReviewList[0].ExpertInviters = nil
	require.Error(t, gs.Validate(), "endorsed without an inviter")

	gs = base()
	gs.JuryReviewList[0].ExpertEndorsers = []string{"j2", "j3"}
	require.Error(t, gs.Validate(), "more endorsers than witnesses")

	gs = base()
	gs.JuryReviewList[0].ExpertEndorsers = nil
	gs.JuryReviewList[0].ExpertInviters = nil
	require.NoError(t, gs.Validate(), "older record without the lists")
}
