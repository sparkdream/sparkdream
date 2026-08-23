package keeper_test

import (
	"fmt"
	"testing"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// A jury shrinks whenever a conscripted juror hands the seat back, and quorum is
// computed on the seated list — so without a floor the roster lowers its own bar
// to decide. At one seated juror the quorum is one, and that juror alone
// satisfies `rejectVotes > totalVotes/2`, rejecting a challenge and burning the
// challenger's stake single-handedly.
//
// The floor guarded the sweep (where seats are taken for silence) and not
// DeclineJuryDuty (where they are given back), which is the path the design
// actively encourages as free. These tests pin the fix from the other end: the
// decline always succeeds, and the *verdict* is what a thinned roster loses.

// seatChallengeReview builds a real project / initiative / challenge and seats a
// jury of the given size on it, so TallyJuryVotes can run its full resolution
// path rather than tripping over a missing challenge record.
func seatChallengeReview(t *testing.T, f *fixture, tag string, jurorCount int) (uint64, []string) {
	t.Helper()
	k, ctx := f.keeper, f.ctx

	mkMember := func(addr string, rep map[string]string) {
		require.NoError(t, k.Member.Set(ctx, addr, types.Member{
			Address:          addr,
			DreamBalance:     keeper.PtrInt(math.ZeroInt()),
			StakedDream:      keeper.PtrInt(math.ZeroInt()),
			LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
			LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
			ReputationScores: rep,
		}))
	}

	creator := sdk.AccAddress([]byte(tag + "-creator---"))
	mkMember(creator.String(), map[string]string{"coding": "100.0"})
	projectID, err := k.CreateProject(ctx, creator, "QF", "Desc", []string{"coding"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.NewInt(50000), math.NewInt(5000), false)
	require.NoError(t, err)
	require.NoError(t, k.ApproveProject(ctx, projectID,
		sdk.AccAddress([]byte(tag+"-approver--")), math.NewInt(50000), math.NewInt(5000)))

	assignee := sdk.AccAddress([]byte(tag + "-assignee--"))
	mkMember(assignee.String(), map[string]string{"coding": "1000.0"})
	initID, err := k.CreateInitiative(ctx, assignee, projectID, "QFInit", "D",
		[]string{"coding"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(150))
	require.NoError(t, err)
	require.NoError(t, k.MintDREAM(ctx, assignee, math.NewInt(1000000)))
	require.NoError(t, k.AssignInitiativeToMember(ctx, initID, assignee))
	require.NoError(t, k.SubmitInitiativeWork(ctx, initID, assignee, "URI"))

	challenger := sdk.AccAddress([]byte(tag + "-challengr-"))
	mkMember(challenger.String(), map[string]string{})
	require.NoError(t, k.MintDREAM(ctx, challenger, math.NewInt(1000000000)))
	chalID, err := k.CreateChallenge(ctx, challenger, initID, "Bad work", nil, math.NewInt(50000000))
	require.NoError(t, err)
	ch, err := k.GetChallenge(ctx, chalID)
	require.NoError(t, err)
	ch.Status = types.ChallengeStatus_CHALLENGE_STATUS_IN_JURY_REVIEW
	require.NoError(t, k.Challenge.Set(ctx, chalID, ch))

	jurors := make([]string, jurorCount)
	for i := 0; i < jurorCount; i++ {
		a := sdk.AccAddress([]byte(fmt.Sprintf("%-17.17s", fmt.Sprintf("%s-juror-%d", tag, i))))
		mkMember(a.String(), map[string]string{"coding": "100.0"})
		jurors[i] = a.String()
	}

	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	jrID, err := k.JuryReviewSeq.Next(ctx)
	require.NoError(t, err)
	jr := types.JuryReview{
		Id:            jrID,
		ChallengeId:   chalID,
		InitiativeId:  initID,
		Jurors:        jurors,
		RequiredVotes: uint32(params.JurySuperMajority.MulInt64(int64(jurorCount)).Ceil().TruncateInt().Uint64()),
		Votes:         []*types.JurorVote{},
		Deadline:      ctx.BlockHeight() + 1000,
		Verdict:       types.Verdict_VERDICT_PENDING,
	}
	require.NoError(t, k.JuryReview.Set(ctx, jrID, jr))
	require.NoError(t, k.AddJuryReviewToVerdictIndex(ctx, jr))
	require.NoError(t, k.AddJuryReviewToJurorIndex(ctx, jr))
	require.NoError(t, k.RecordJurySeating(ctx, jurors))
	return jrID, jurors
}

func voteAll(t *testing.T, f *fixture, id uint64, jurors []string, v types.Verdict) {
	t.Helper()
	review, err := f.keeper.GetJuryReview(f.ctx, id)
	require.NoError(t, err)
	for _, j := range jurors {
		review.Votes = append(review.Votes, &types.JurorVote{
			Juror: j, Verdict: v, Reasoning: "r",
		})
	}
	require.NoError(t, f.keeper.JuryReview.Set(f.ctx, id, review))
}

func TestThinnedJuryCannotReturnAVerdict(t *testing.T) {
	f := initFixture(t)

	// One seated juror voting to reject. Before the floor this cleared a quorum
	// of one and rejected the challenge outright, burning the challenger's
	// stake on a single voice.
	id, jurors := seatChallengeReview(t, f, "qfa", 1)
	voteAll(t, f, id, jurors, types.Verdict_VERDICT_REJECT_CHALLENGE)

	require.NoError(t, f.keeper.TallyJuryVotes(f.ctx, id))

	after, err := f.keeper.GetJuryReview(f.ctx, id)
	require.NoError(t, err)
	require.Equal(t, types.Verdict_VERDICT_INCONCLUSIVE, after.Verdict,
		"a single juror must never carry a verdict; the review takes its terminal path instead")
}

func TestTwoConcurringJurorsStillDecide(t *testing.T) {
	f := initFixture(t)

	// The floor blocks a rump, not every small jury. Two jurors who both vote
	// are a real quorum — a thin eligible pool must still resolve disputes, or
	// challenges on a young chain would never conclude.
	id, jurors := seatChallengeReview(t, f, "qfb", 2)
	voteAll(t, f, id, jurors, types.Verdict_VERDICT_REJECT_CHALLENGE)

	require.NoError(t, f.keeper.TallyJuryVotes(f.ctx, id))

	after, err := f.keeper.GetJuryReview(f.ctx, id)
	require.NoError(t, err)
	require.Equal(t, types.Verdict_VERDICT_REJECT_CHALLENGE, after.Verdict,
		"two concurring jurors clear the floor")
}

func TestDecliningIsNeverRefused(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx

	// Refusing a decline to protect quorum would push an honest juror into
	// silence — which costs them selection weight, where a decline does not —
	// and would collapse the justification for the abandoned-seat penalty.
	// Declining stays unconditional however small the jury gets.
	a := sdk.AccAddress([]byte("qf-dec-a--------")).String()
	b := sdk.AccAddress([]byte("qf-dec-b--------")).String()
	c := sdk.AccAddress([]byte("qf-dec-c--------")).String()
	seatReview(t, k, ctx, 902, a, b, c)

	require.NoError(t, k.DeclineJuryDuty(ctx, 902, a))
	require.NoError(t, k.DeclineJuryDuty(ctx, 902, b))
	require.NoError(t, k.DeclineJuryDuty(ctx, 902, c),
		"the last juror may still hand the seat back")

	after, err := k.GetJuryReview(ctx, 902)
	require.NoError(t, err)
	require.Empty(t, after.Jurors)

	// And every decline is recorded as answering, not as a no-show.
	for _, addr := range []string{a, b, c} {
		p, err := k.JuryParticipation.Get(ctx, addr)
		require.NoError(t, err)
		require.Equal(t, uint64(1), p.TotalDeclined, "decline recorded for %s", addr)
		require.Zero(t, p.TotalTimeouts, "a decline is never a timeout for %s", addr)
	}
}

func TestDeclineRecomputesRequiredVotes(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx

	// RequiredVotes is stored. Leaving it stale after a roster change silently
	// changed what the jury could conclude: a jury thinned by declines kept the
	// original supermajority, which blocked the uphold direction while leaving
	// reject reachable by a rump.
	a := sdk.AccAddress([]byte("qf-rv-a---------")).String()
	b := sdk.AccAddress([]byte("qf-rv-b---------")).String()
	c := sdk.AccAddress([]byte("qf-rv-c---------")).String()
	review := seatReview(t, k, ctx, 903, a, b, c)

	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	review.RequiredVotes = uint32(params.JurySuperMajority.MulInt64(3).Ceil().TruncateInt().Uint64())
	require.NoError(t, k.JuryReview.Set(ctx, review.Id, review))
	before := review.RequiredVotes

	require.NoError(t, k.DeclineJuryDuty(ctx, 903, a))

	after, err := k.GetJuryReview(ctx, 903)
	require.NoError(t, err)
	expected := uint32(params.JurySuperMajority.
		MulInt64(int64(len(after.Jurors))).Ceil().TruncateInt().Uint64())
	require.Equal(t, expected, after.RequiredVotes,
		"required votes must track the seated list, not the roster it was drawn with")
	require.NotEqual(t, before, after.RequiredVotes,
		"this jury shrank, so the threshold should have moved")
}
