package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// ---------------------------------------------------------------------------
// REP-E-21: MsgCompleteProject
// ---------------------------------------------------------------------------

// completeProjectGate makes only `policy` the technical council's Operations
// Committee policy; every address stays an individual committee member (the
// fixture's IsCommitteeMember default), so the tests can tell the two apart.
func completeProjectGate(t *testing.T, f *fixture) (policy, member string) {
	t.Helper()
	var err error
	policy, err = f.addressCodec.BytesToString(sdk.AccAddress([]byte("tech_ops_policy_")))
	require.NoError(t, err)
	member, err = f.addressCodec.BytesToString(sdk.AccAddress([]byte("tech_ops_member_")))
	require.NoError(t, err)
	f.commonsKeeper.IsCouncilAuthorizedFn = func(context.Context, string, string, string) bool { return true }
	f.commonsKeeper.IsCouncilOrCommitteePolicyFn = func(_ context.Context, addr, council, committee string) bool {
		return addr == policy && council == "technical" && committee == "operations"
	}
	return policy, member
}

func projectStatusIndexed(t *testing.T, f *fixture, status types.ProjectStatus, id uint64) bool {
	t.Helper()
	found := false
	require.NoError(t, f.keeper.IterateProjectsByStatus(f.ctx, status, func(pid uint64, _ types.Project) bool {
		if pid == id {
			found = true
			return true
		}
		return false
	}))
	return found
}

func TestREPE21_CompleteProjectHappyPath(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ms := keeper.NewMsgServerImpl(k)
	policy, member := completeProjectGate(t, f)

	creator := newStakerMember(t, f, "cp_creator__________", math.NewInt(5_000_000_000))
	staker := newStakerMember(t, f, "cp_staker___________", math.NewInt(5_000_000_000))
	projectID := newActiveProject(t, k, f.ctx, creator)

	// A finished (terminal) initiative does not block completion.
	initID, err := k.CreateInitiative(f.ctx, creator, projectID, "Done", "D", []string{"tag"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(1000))
	require.NoError(t, err)
	require.NoError(t, k.CloseInitiative(f.ctx, initID, "retired"))

	amount := math.NewInt(4_000_000)
	stakeID, err := k.CreateStake(f.ctx, staker, types.StakeTargetType_STAKE_TARGET_PROJECT, projectID, "", amount)
	require.NoError(t, err)
	project, err := k.GetProject(f.ctx, projectID)
	require.NoError(t, err)
	project.SpentBudget = PtrInt(math.NewInt(20_000))
	require.NoError(t, k.UpdateProject(f.ctx, project))

	// A single committee member is refused, and nothing moves.
	_, err = ms.CompleteProject(f.ctx, &types.MsgCompleteProject{Creator: member, ProjectId: projectID})
	require.ErrorIs(t, err, types.ErrUnauthorized)
	still, err := k.GetProject(f.ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, types.ProjectStatus_PROJECT_STATUS_ACTIVE, still.Status)

	earnedBefore := *mustMember(t, f, staker).LifetimeEarned
	mintedBefore, err := k.GetSeasonInitiativeRewardsMinted(f.ctx)
	require.NoError(t, err)

	_, err = ms.CompleteProject(f.ctx, &types.MsgCompleteProject{Creator: policy, ProjectId: projectID})
	require.NoError(t, err)

	done, err := k.GetProject(f.ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, types.ProjectStatus_PROJECT_STATUS_COMPLETED, done.Status, "terminal status")
	require.True(t, projectStatusIndexed(t, f, types.ProjectStatus_PROJECT_STATUS_COMPLETED, projectID))
	require.False(t, projectStatusIndexed(t, f, types.ProjectStatus_PROJECT_STATUS_ACTIVE, projectID))

	bonus := mustMember(t, f, staker).LifetimeEarned.Sub(earnedBefore)
	require.True(t, bonus.IsPositive(), "the external staker receives the completion bonus")
	mintedAfter, err := k.GetSeasonInitiativeRewardsMinted(f.ctx)
	require.NoError(t, err)
	require.True(t, mintedAfter.GT(mintedBefore), "the bonus is booked against the season cap")

	_, err = k.GetStake(f.ctx, stakeID)
	require.Error(t, err, "the project stake is released and deleted")
	require.Equal(t, "0", mustMember(t, f, staker).StakedDream.String())

	// Terminal: a second completion is refused.
	_, err = ms.CompleteProject(f.ctx, &types.MsgCompleteProject{Creator: policy, ProjectId: projectID})
	require.ErrorIs(t, err, types.ErrInvalidProjectStatus)

	// x/gov may complete too.
	other := newActiveProject(t, k, f.ctx, creator)
	gov, err := f.addressCodec.BytesToString(k.GetAuthority())
	require.NoError(t, err)
	_, err = ms.CompleteProject(f.ctx, &types.MsgCompleteProject{Creator: gov, ProjectId: other})
	require.NoError(t, err)
}

func TestREPE21_CompleteProjectRefusals(t *testing.T) {
	t.Run("open initiative", func(t *testing.T) {
		f := initFixture(t)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)
		policy, _ := completeProjectGate(t, f)
		creator := newStakerMember(t, f, "cp_open_creator_____", math.NewInt(5_000_000_000))
		projectID := newActiveProject(t, k, f.ctx, creator)
		_, err := k.CreateInitiative(f.ctx, creator, projectID, "Open", "D", []string{"tag"},
			types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(1000))
		require.NoError(t, err)

		_, err = ms.CompleteProject(f.ctx, &types.MsgCompleteProject{Creator: policy, ProjectId: projectID})
		require.ErrorIs(t, err, types.ErrProjectHasOpenWork)
		p, err := k.GetProject(f.ctx, projectID)
		require.NoError(t, err)
		require.Equal(t, types.ProjectStatus_PROJECT_STATUS_ACTIVE, p.Status)
	})

	t.Run("active challenge", func(t *testing.T) {
		f := initFixture(t)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)
		policy, _ := completeProjectGate(t, f)
		creator := newStakerMember(t, f, "cp_chal_creator_____", math.NewInt(5_000_000_000))
		projectID := newActiveProject(t, k, f.ctx, creator)
		initID, err := k.CreateInitiative(f.ctx, creator, projectID, "Chal", "D", []string{"tag"},
			types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(1000))
		require.NoError(t, err)
		require.NoError(t, k.CloseInitiative(f.ctx, initID, "retired"))
		// A challenge still live against the (terminal) initiative.
		challenge := types.Challenge{
			Id:           77,
			InitiativeId: initID,
			Challenger:   sdk.AccAddress([]byte("cp_challenger_______")).String(),
			Status:       types.ChallengeStatus_CHALLENGE_STATUS_IN_JURY_REVIEW,
		}
		require.NoError(t, k.Challenge.Set(f.ctx, challenge.Id, challenge))
		require.NoError(t, k.AddChallengeToStatusIndex(f.ctx, challenge))

		_, err = ms.CompleteProject(f.ctx, &types.MsgCompleteProject{Creator: policy, ProjectId: projectID})
		require.ErrorIs(t, err, types.ErrProjectHasOpenWork)
	})

	t.Run("not active", func(t *testing.T) {
		f := initFixture(t)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)
		policy, _ := completeProjectGate(t, f)
		creator := newStakerMember(t, f, "cp_state_creator____", math.NewInt(5_000_000_000))

		proposed, err := k.CreateProject(f.ctx, creator, "Prop", "D", []string{"tag"},
			types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(100000), math.ZeroInt(), false)
		require.NoError(t, err)
		_, err = ms.CompleteProject(f.ctx, &types.MsgCompleteProject{Creator: policy, ProjectId: proposed})
		require.ErrorIs(t, err, types.ErrInvalidProjectStatus)

		cancelled := newActiveProject(t, k, f.ctx, creator)
		require.NoError(t, k.CancelProject(f.ctx, cancelled, "retired"))
		_, err = ms.CompleteProject(f.ctx, &types.MsgCompleteProject{Creator: policy, ProjectId: cancelled})
		require.ErrorIs(t, err, types.ErrInvalidProjectStatus)

		_, err = ms.CompleteProject(f.ctx, &types.MsgCompleteProject{Creator: policy, ProjectId: 9999})
		require.ErrorIs(t, err, types.ErrProjectNotFound)
	})

	t.Run("policy of another council", func(t *testing.T) {
		f := initFixture(t)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)
		completeProjectGate(t, f)
		creator := newStakerMember(t, f, "cp_xcouncil_creator_", math.NewInt(5_000_000_000))
		projectID := newActiveProject(t, k, f.ctx, creator)
		commonsPolicy, err := f.addressCodec.BytesToString(sdk.AccAddress([]byte("commons_policy__")))
		require.NoError(t, err)
		_, err = ms.CompleteProject(f.ctx, &types.MsgCompleteProject{Creator: commonsPolicy, ProjectId: projectID})
		require.ErrorIs(t, err, types.ErrUnauthorized)
	})
}

// ---------------------------------------------------------------------------
// REP-E-22: every review round's reviewers are paid exactly once
// ---------------------------------------------------------------------------

func earned(t *testing.T, f *fixture, addr sdk.AccAddress) math.Int {
	t.Helper()
	m, err := f.keeper.Member.Get(f.ctx, addr.String())
	require.NoError(t, err)
	return *m.LifetimeEarned
}

func roundFeeShare(t *testing.T, rf *reviewFixture) math.Int {
	t.Helper()
	initiative, err := rf.f.keeper.GetInitiative(rf.f.ctx, rf.initiative)
	require.NoError(t, err)
	pool := rf.f.keeper.ReviewFeePool(rf.f.ctx, gateParams(t, rf.f.keeper, rf.f.ctx), initiative)
	require.True(t, pool.IsPositive(), "precondition: the review fee is on")
	return pool // one verdict per round in these tests
}

func feePaidOn(t *testing.T, rf *reviewFixture, round uint32, reviewer sdk.AccAddress) math.Int {
	t.Helper()
	r, err := rf.f.keeper.InitiativeReview.Get(rf.f.ctx, collections.Join3(rf.initiative, round, reviewer.String()))
	require.NoError(t, err)
	if r.FeePaid.IsNil() {
		return math.ZeroInt()
	}
	return r.FeePaid
}

func TestREPE22_RejectResubmitCompletePaysBothRoundsOnce(t *testing.T) {
	rf := setupReview(t, 1)
	k, ctx := rf.f.keeper, rf.f.ctx
	second := sdk.AccAddress([]byte("rv-reviewer-two--"))
	mkReviewMember(t, k, ctx, second, "500.0")
	bondReviewer(t, k, ctx, second, 100_000_000)
	share := roundFeeShare(t, rf)

	firstBefore, secondBefore := earned(t, rf.f, rf.reviewer), earned(t, rf.f, second)

	// Round 0: rejected. Its reviewer is paid now, before the round advances.
	require.NoError(t, k.SubmitInitiativeReview(ctx, rf.initiative, rf.reviewer.String(), false, nil, "not done"))
	require.Equal(t, share.String(), earned(t, rf.f, rf.reviewer).Sub(firstBefore).String())
	require.Equal(t, share.String(), feePaidOn(t, rf, 0, rf.reviewer).String())

	// Round 1: resubmitted and approved, then completed.
	require.NoError(t, k.SubmitInitiativeWork(ctx, rf.initiative, rf.assignee, "ipfs://work-v2"))
	require.NoError(t, k.SubmitInitiativeReview(ctx, rf.initiative, second.String(), true, nil, "fixed"))
	init, err := k.GetInitiative(ctx, rf.initiative)
	require.NoError(t, err)
	init.CurrentConviction = PtrDec(DerefDec(init.RequiredConviction).Mul(math.LegacyNewDec(3)))
	init.ExternalConviction = PtrDec(DerefDec(init.RequiredConviction).Mul(math.LegacyNewDec(3)))
	require.NoError(t, k.UpdateInitiative(ctx, init))
	advanceToCompletable(t, k, ctx, rf.initiative)
	require.NoError(t, k.CompleteInitiative(ctx, rf.initiative))

	require.Equal(t, share.String(), earned(t, rf.f, rf.reviewer).Sub(firstBefore).String(),
		"the rejecting round is paid once, not again at completion")
	require.Equal(t, share.String(), earned(t, rf.f, second).Sub(secondBefore).String(),
		"the resolving round is paid at completion")
	require.Equal(t, share.String(), feePaidOn(t, rf, 1, second).String())

	total, err := k.ReviewFeesPaid(ctx, rf.initiative)
	require.NoError(t, err)
	require.Equal(t, share.MulRaw(2).String(), total.String())

	// A further settlement call on any round pays nothing.
	final, err := k.GetInitiative(ctx, rf.initiative)
	require.NoError(t, err)
	again, err := k.PayReviewFees(ctx, final)
	require.NoError(t, err)
	require.True(t, again.IsZero())
	final.ReviewRound = 0
	again, err = k.PayReviewFees(ctx, final)
	require.NoError(t, err)
	require.True(t, again.IsZero())
}

func TestREPE22_RoundExhaustionPaysEveryRoundAndChargesProject(t *testing.T) {
	rf := setupReview(t, 1)
	k, ctx := rf.f.keeper, rf.f.ctx
	share := roundFeeShare(t, rf)
	params := gateParams(t, k, ctx)
	before := earned(t, rf.f, rf.reviewer)
	projectBefore, err := k.GetProject(ctx, rf.projectID)
	require.NoError(t, err)
	allocatedBefore := keeper.DerefInt(projectBefore.AllocatedBudget)

	rounds := 0
	for {
		require.NoError(t, k.SubmitInitiativeReview(ctx, rf.initiative, rf.reviewer.String(), false, nil, "still not done"))
		rounds++
		cur, gErr := k.GetInitiative(ctx, rf.initiative)
		require.NoError(t, gErr)
		if cur.Status == types.InitiativeStatus_INITIATIVE_STATUS_CLOSED {
			break
		}
		require.Less(t, rounds, int(params.MaxReviewRounds))
		require.NoError(t, k.SubmitInitiativeWork(ctx, rf.initiative, rf.assignee, "ipfs://again"))
	}
	require.Equal(t, int(params.MaxReviewRounds), rounds)

	paid := share.MulRaw(int64(rounds))
	require.Equal(t, paid.String(), earned(t, rf.f, rf.reviewer).Sub(before).String(),
		"each rejected round is paid exactly once, the last one on the close")
	total, err := k.ReviewFeesPaid(ctx, rf.initiative)
	require.NoError(t, err)
	require.Equal(t, paid.String(), total.String())

	projectAfter, err := k.GetProject(ctx, rf.projectID)
	require.NoError(t, err)
	returned := allocatedBefore.Sub(keeper.DerefInt(projectAfter.AllocatedBudget))
	require.Equal(t, math.NewInt(1000).Sub(paid).String(), returned.String(),
		"the returned budget is net of every round's fee")
}

func TestREPE22_ProjectCancellationChargesEveryRound(t *testing.T) {
	rf := setupReview(t, 1)
	k, ctx := rf.f.keeper, rf.f.ctx
	second := sdk.AccAddress([]byte("rv-reviewer-cxl--"))
	mkReviewMember(t, k, ctx, second, "500.0")
	bondReviewer(t, k, ctx, second, 100_000_000)
	share := roundFeeShare(t, rf)
	firstBefore, secondBefore := earned(t, rf.f, rf.reviewer), earned(t, rf.f, second)

	require.NoError(t, k.SubmitInitiativeReview(ctx, rf.initiative, rf.reviewer.String(), false, nil, "not done"))
	require.NoError(t, k.SubmitInitiativeWork(ctx, rf.initiative, rf.assignee, "ipfs://work-v2"))
	require.NoError(t, k.SubmitInitiativeReview(ctx, rf.initiative, second.String(), true, nil, "fine"))

	require.NoError(t, k.CancelProject(ctx, rf.projectID, "retired"))

	require.Equal(t, share.String(), earned(t, rf.f, rf.reviewer).Sub(firstBefore).String())
	require.Equal(t, share.String(), earned(t, rf.f, second).Sub(secondBefore).String())
	project, err := k.GetProject(ctx, rf.projectID)
	require.NoError(t, err)
	require.Equal(t, share.MulRaw(2).String(), keeper.DerefInt(project.AllocatedBudget).String(),
		"only the two rounds' fees stay charged to the project")
}

func TestREPE22_RejectionNeverBlockedByTheCap(t *testing.T) {
	rf := setupReview(t, 1)
	k, ctx := rf.f.keeper, rf.f.ctx
	params := gateParams(t, k, ctx)
	params.MaxInitiativeRewardsPerSeason = math.ZeroInt()
	require.NoError(t, k.Params.Set(ctx, params))
	before := earned(t, rf.f, rf.reviewer)

	require.NoError(t, k.SubmitInitiativeReview(ctx, rf.initiative, rf.reviewer.String(), false, nil, "not done"),
		"a reviewer can always reject")
	cur, err := k.GetInitiative(ctx, rf.initiative)
	require.NoError(t, err)
	require.Equal(t, uint32(1), cur.ReviewRound)
	require.True(t, earned(t, rf.f, rf.reviewer).Equal(before), "over the cap the round's fee is forfeited")
	require.True(t, feePaidOn(t, rf, 0, rf.reviewer).IsZero())

	forfeited := false
	for _, ev := range ctx.EventManager().Events() {
		if ev.Type == "initiative_review_fee_forfeited" {
			forfeited = true
		}
	}
	require.True(t, forfeited)
}

// ---------------------------------------------------------------------------
// REP-E-23: juror votes must take a side with confidence in [0, 1]
// ---------------------------------------------------------------------------

func TestREPE23_JurorVoteDomain(t *testing.T) {
	setup := func(t *testing.T) (*fixture, uint64, sdk.AccAddress, string) {
		f := initFixture(t)
		juror := sdk.AccAddress([]byte("jv_juror_one________"))
		other := sdk.AccAddress([]byte("jv_juror_two________"))
		review := types.JuryReview{
			Id:            41,
			Jurors:        []string{juror.String(), other.String()},
			RequiredVotes: 1, // a single stored vote would trigger the tally
			Deadline:      f.ctx.BlockHeight() + 1000,
			Verdict:       types.Verdict_VERDICT_PENDING,
		}
		require.NoError(t, f.keeper.JuryReview.Set(f.ctx, review.Id, review))
		return f, review.Id, juror, juror.String()
	}

	half := math.LegacyNewDecWithPrec(5, 1)
	cases := []struct {
		name       string
		verdict    types.Verdict
		confidence math.LegacyDec
	}{
		{"pending", types.Verdict_VERDICT_PENDING, half},
		{"inconclusive", types.Verdict_VERDICT_INCONCLUSIVE, half},
		{"unknown enum", types.Verdict(99), half},
		{"negative confidence", types.Verdict_VERDICT_UPHOLD_CHALLENGE, math.LegacyNewDec(-1)},
		{"confidence above one", types.Verdict_VERDICT_REJECT_CHALLENGE, math.LegacyNewDec(2)},
		{"pending with confidence two", types.Verdict_VERDICT_PENDING, math.LegacyNewDec(2)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, reviewID, juror, jurorStr := setup(t)
			ms := keeper.NewMsgServerImpl(f.keeper)

			_, err := ms.SubmitJurorVote(f.ctx, &types.MsgSubmitJurorVote{
				Juror: jurorStr, JuryReviewId: reviewID, Verdict: tc.verdict,
				Confidence: keeper.PtrDec(tc.confidence), Reasoning: "x",
			})
			require.ErrorIs(t, err, types.ErrInvalidJurorVote, "msg server")

			err = f.keeper.SubmitJurorVote(f.ctx, reviewID, juror, nil, tc.verdict, tc.confidence, "x")
			require.ErrorIs(t, err, types.ErrInvalidJurorVote, "keeper")

			// Nothing recorded: no vote, no tally, no participation credit.
			review, err := f.keeper.GetJuryReview(f.ctx, reviewID)
			require.NoError(t, err)
			require.Empty(t, review.Votes)
			require.Equal(t, types.Verdict_VERDICT_PENDING, review.Verdict, "a malformed vote must not trigger the tally")
			has, err := f.keeper.JuryParticipation.Has(f.ctx, jurorStr)
			require.NoError(t, err)
			require.False(t, has, "a malformed vote must not count as having voted")
		})
	}

	t.Run("bounds are inclusive", func(t *testing.T) {
		require.NoError(t, types.ValidateJurorVote(types.Verdict_VERDICT_UPHOLD_CHALLENGE, math.LegacyZeroDec()))
		require.NoError(t, types.ValidateJurorVote(types.Verdict_VERDICT_REJECT_CHALLENGE, math.LegacyOneDec()))
		require.ErrorIs(t, types.ValidateJurorVote(types.Verdict_VERDICT_REJECT_CHALLENGE, math.LegacyDec{}), types.ErrInvalidJurorVote)
	})

	t.Run("a deciding vote is still recorded", func(t *testing.T) {
		f, reviewID, _, jurorStr := setup(t)
		review, err := f.keeper.GetJuryReview(f.ctx, reviewID)
		require.NoError(t, err)
		review.RequiredVotes = 2 // keep the tally out of this check
		require.NoError(t, f.keeper.JuryReview.Set(f.ctx, reviewID, review))
		ms := keeper.NewMsgServerImpl(f.keeper)
		_, err = ms.SubmitJurorVote(f.ctx, &types.MsgSubmitJurorVote{
			Juror: jurorStr, JuryReviewId: reviewID, Verdict: types.Verdict_VERDICT_UPHOLD_CHALLENGE,
			Confidence: keeper.PtrDec(math.LegacyOneDec()), Reasoning: "x",
		})
		require.NoError(t, err)
		review, err = f.keeper.GetJuryReview(f.ctx, reviewID)
		require.NoError(t, err)
		require.Len(t, review.Votes, 1)
	})
}
