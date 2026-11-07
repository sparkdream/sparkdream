package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

type expertWitnessFixture struct {
	*fixture
	ms                                           types.MsgServer
	reviewID                                     uint64
	creator, assignee, challenger, juror, juror2 sdk.AccAddress
	expert, expert2, expert3, novice, nob        sdk.AccAddress
}

func setExpertMember(t *testing.T, f *fixture, addr sdk.AccAddress, trust types.TrustLevel, scores map[string]string) {
	t.Helper()
	require.NoError(t, f.keeper.Member.Set(f.ctx, addr.String(), types.Member{
		Address:          addr.String(),
		DreamBalance:     keeper.PtrInt(math.ZeroInt()),
		StakedDream:      keeper.PtrInt(math.ZeroInt()),
		LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
		LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
		Status:           types.MemberStatus_MEMBER_STATUS_ACTIVE,
		TrustLevel:       trust,
		ReputationScores: scores,
	}))
}

// newExpertWitnessFixture seats two jurors (juror invites, juror2 has
// accepted jury duty and endorses) on a pending jury review of a challenged
// tagged initiative and registers candidate experts.
func newExpertWitnessFixture(t *testing.T) *expertWitnessFixture {
	t.Helper()
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx
	addr := func(s string) sdk.AccAddress { return sdk.AccAddress([]byte(s + "____________________")[:20]) }
	e := &expertWitnessFixture{
		fixture: f, ms: keeper.NewMsgServerImpl(k),
		creator: addr("creator"), assignee: addr("assignee"), challenger: addr("challenger"), juror: addr("juror"), juror2: addr("juror2"),
		expert: addr("expert"), expert2: addr("expert2"), expert3: addr("expert3"), novice: addr("novice"), nob: addr("outsider"),
	}
	tagged := map[string]string{"tag": "100.0"}
	for _, a := range []sdk.AccAddress{e.creator, e.assignee, e.challenger, e.juror, e.juror2, e.expert, e.expert2, e.expert3} {
		setExpertMember(t, f, a, types.TrustLevel_TRUST_LEVEL_ESTABLISHED, tagged)
	}
	setExpertMember(t, f, e.novice, types.TrustLevel_TRUST_LEVEL_PROVISIONAL, tagged)
	setExpertMember(t, f, e.nob, types.TrustLevel_TRUST_LEVEL_TRUSTED, map[string]string{"aurora": "100.0"})

	projectID, err := k.CreateProject(ctx, e.creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
	require.NoError(t, err)
	require.NoError(t, k.ApproveProject(ctx, projectID, addr("approver"), math.NewInt(10000), math.NewInt(1000)))
	initID, err := k.CreateInitiative(ctx, e.creator, projectID, "Task", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(100))
	require.NoError(t, err)
	require.NoError(t, k.AssignInitiativeToMember(ctx, initID, e.assignee))
	require.NoError(t, k.SubmitInitiativeWork(ctx, initID, e.assignee, "uri"))

	require.NoError(t, k.Challenge.Set(ctx, 7, types.Challenge{
		Id: 7, InitiativeId: initID, Challenger: e.challenger.String(),
		Status: types.ChallengeStatus_CHALLENGE_STATUS_IN_JURY_REVIEW, StakedDream: keeper.PtrInt(math.NewInt(100)),
	}))
	e.reviewID = 9
	require.NoError(t, k.JuryReview.Set(ctx, e.reviewID, types.JuryReview{
		Id: e.reviewID, ChallengeId: 7, InitiativeId: initID,
		Jurors: []string{e.juror.String(), e.juror2.String()}, RequiredVotes: 1,
		Accepted: []string{e.juror2.String()},
		Deadline: sdk.UnwrapSDKContext(ctx).BlockHeight() + 100,
		Verdict:  types.Verdict_VERDICT_PENDING,
	}))
	return e
}

func (e *expertWitnessFixture) invite(juror, expert sdk.AccAddress) (*types.MsgInviteExpertWitnessResponse, error) {
	return e.ms.InviteExpertWitness(e.ctx, &types.MsgInviteExpertWitness{
		Juror: juror.String(), JuryReviewId: e.reviewID, Expert: expert.String(),
	})
}

func (e *expertWitnessFixture) endorse(juror, expert sdk.AccAddress) error {
	_, err := e.ms.EndorseExpertWitness(e.ctx, &types.MsgEndorseExpertWitness{
		Juror: juror.String(), JuryReviewId: e.reviewID, Expert: expert.String(),
	})
	return err
}

// inviteEndorsed is the two-juror path: juror invites, juror2 endorses.
func (e *expertWitnessFixture) inviteEndorsed(t *testing.T, expert sdk.AccAddress) uint64 {
	t.Helper()
	resp, err := e.invite(e.juror, expert)
	require.NoError(t, err)
	require.NoError(t, e.endorse(e.juror2, expert))
	return resp.InterimId
}

// A seated juror calls an eligible expert: the expert joins the witness list
// and an EXPERT_TESTIMONY interim is opened for them, due with the review.
// The verdict completes the interim of an expert who testified and pays its
// COMPLEX budget.
func TestInviteExpertWitnessAndPayOnTestimony(t *testing.T) {
	e := newExpertWitnessFixture(t)
	k, ctx := e.keeper, e.ctx

	resp, err := e.invite(e.juror, e.expert)
	require.NoError(t, err)
	require.NoError(t, e.endorse(e.juror2, e.expert))

	review, err := k.JuryReview.Get(ctx, e.reviewID)
	require.NoError(t, err)
	require.Equal(t, []string{e.expert.String()}, review.ExpertWitnesses)
	require.Equal(t, []string{e.juror.String()}, review.ExpertInviters)
	require.Equal(t, []string{e.juror2.String()}, review.ExpertEndorsers)

	interim, err := k.GetInterim(ctx, resp.InterimId)
	require.NoError(t, err)
	require.Equal(t, types.InterimType_INTERIM_TYPE_EXPERT_TESTIMONY, interim.Type)
	require.Equal(t, []string{e.expert.String()}, interim.Assignees)
	require.Equal(t, e.reviewID, interim.ReferenceId)
	require.Equal(t, review.Deadline, interim.Deadline)
	require.Equal(t, types.InterimStatus_INTERIM_STATUS_PENDING, interim.Status)

	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	before, err := k.Member.Get(ctx, e.expert.String())
	require.NoError(t, err)

	_, err = e.ms.SubmitExpertTestimony(ctx, &types.MsgSubmitExpertTestimony{
		Expert: e.expert.String(), JuryReviewId: e.reviewID, Opinion: sampleOpinion, Reasoning: "meets the criteria",
	})
	require.NoError(t, err)
	require.NoError(t, k.TallyJuryVotes(ctx, e.reviewID))

	interim, err = k.GetInterim(ctx, resp.InterimId)
	require.NoError(t, err)
	require.Equal(t, types.InterimStatus_INTERIM_STATUS_COMPLETED, interim.Status)
	after, err := k.Member.Get(ctx, e.expert.String())
	require.NoError(t, err)
	require.Equal(t, params.ComplexComplexityBudget.String(), after.DreamBalance.Sub(*before.DreamBalance).String(), "paid the COMPLEX budget")
}

func TestInviteExpertWitnessRules(t *testing.T) {
	t.Run("only a seated juror invites", func(t *testing.T) {
		e := newExpertWitnessFixture(t)
		_, err := e.invite(e.nob, e.expert)
		require.ErrorIs(t, err, types.ErrNotSeatedJuror)
	})
	t.Run("expert needs ESTABLISHED trust", func(t *testing.T) {
		e := newExpertWitnessFixture(t)
		_, err := e.invite(e.juror, e.novice)
		require.ErrorIs(t, err, types.ErrExpertIneligible)
	})
	t.Run("expert needs reputation in the initiative's tags", func(t *testing.T) {
		e := newExpertWitnessFixture(t)
		_, err := e.invite(e.juror, e.nob)
		require.ErrorIs(t, err, types.ErrExpertIneligible)
	})
	t.Run("parties and jurors cannot testify", func(t *testing.T) {
		e := newExpertWitnessFixture(t)
		for _, p := range []sdk.AccAddress{e.challenger, e.assignee, e.creator, e.juror} {
			_, err := e.invite(e.juror, p)
			require.ErrorIs(t, err, types.ErrExpertIneligible, p.String())
		}
	})
	t.Run("no duplicates, at most MaxExpertWitnessesPerReview", func(t *testing.T) {
		e := newExpertWitnessFixture(t)
		_, err := e.invite(e.juror, e.expert)
		require.NoError(t, err)
		_, err = e.invite(e.juror, e.expert)
		require.ErrorIs(t, err, types.ErrExpertIneligible, "already listed")
		require.Equal(t, 2, keeper.MaxExpertWitnessesPerReview)
		_, err = e.invite(e.juror, e.expert2)
		require.NoError(t, err)
		_, err = e.invite(e.juror, e.expert3)
		require.ErrorIs(t, err, types.ErrTooManyExpertWitnesses)
	})
	t.Run("review must be a pending initiative review", func(t *testing.T) {
		e := newExpertWitnessFixture(t)
		review, err := e.keeper.JuryReview.Get(e.ctx, e.reviewID)
		require.NoError(t, err)
		review.Verdict = types.Verdict_VERDICT_REJECT_CHALLENGE
		require.NoError(t, e.keeper.JuryReview.Set(e.ctx, e.reviewID, review))
		_, err = e.invite(e.juror, e.expert)
		require.ErrorIs(t, err, types.ErrNotInitiativeReview, "resolved")

		review.Verdict = types.Verdict_VERDICT_PENDING
		review.ContentChallengeId = 3
		require.NoError(t, e.keeper.JuryReview.Set(e.ctx, e.reviewID, review))
		_, err = e.invite(e.juror, e.expert)
		require.ErrorIs(t, err, types.ErrNotInitiativeReview, "content challenge")
	})
	t.Run("an uncalled expert's interim expires with the review", func(t *testing.T) {
		e := newExpertWitnessFixture(t)
		resp, err := e.invite(e.juror, e.expert)
		require.NoError(t, err)
		review, err := e.keeper.JuryReview.Get(e.ctx, e.reviewID)
		require.NoError(t, err)
		require.NoError(t, e.keeper.ExpireInterim(e.ctx, resp.InterimId))
		interim, err := e.keeper.GetInterim(e.ctx, resp.InterimId)
		require.NoError(t, err)
		require.Equal(t, types.InterimStatus_INTERIM_STATUS_EXPIRED, interim.Status)
		require.Equal(t, review.Deadline, interim.Deadline)
	})
}

// A review written before expert_interim_ids existed lists a witness with no
// interim. A new invitation pads the list first, so the new expert's interim
// sits at the new expert's index and the verdict settles it, instead of
// pairing it with the older witness.
func TestExpertInterimIdsAlignedOnLegacyReview(t *testing.T) {
	e := newExpertWitnessFixture(t)
	k, ctx := e.keeper, e.ctx
	review, err := k.JuryReview.Get(ctx, e.reviewID)
	require.NoError(t, err)
	review.ExpertWitnesses = []string{e.expert3.String()}
	require.NoError(t, k.JuryReview.Set(ctx, e.reviewID, review))

	interimID := e.inviteEndorsed(t, e.expert)
	review, err = k.JuryReview.Get(ctx, e.reviewID)
	require.NoError(t, err)
	require.Equal(t, []string{e.expert3.String(), e.expert.String()}, review.ExpertWitnesses)
	require.Equal(t, []uint64{0, interimID}, review.ExpertInterimIds)

	_, err = e.ms.SubmitExpertTestimony(ctx, &types.MsgSubmitExpertTestimony{
		Expert: e.expert.String(), JuryReviewId: e.reviewID, Opinion: sampleOpinion, Reasoning: "meets the criteria",
	})
	require.NoError(t, err)
	require.NoError(t, k.TallyJuryVotes(ctx, e.reviewID))
	interim, err := k.GetInterim(ctx, interimID)
	require.NoError(t, err)
	require.Equal(t, types.InterimStatus_INTERIM_STATUS_COMPLETED, interim.Status, "settled as the new expert's interim")
}
