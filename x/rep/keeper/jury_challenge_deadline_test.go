package keeper_test

import (
	"fmt"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// TestCreateJuryReviewIndexesPending verifies the creation-site wiring that
// makes the deadline sweep work in production: CreateJuryReview must register the
// new review in the PENDING verdict index (and exclude the challenger).
func TestCreateJuryReviewIndexesPending(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	params, _ := k.Params.Get(ctx)
	params.JurySize = 5
	params.MinJurorReputation = math.LegacyNewDec(50)
	require.NoError(t, k.Params.Set(ctx, params))

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

	creator := sdk.AccAddress([]byte("cjr-proj-creator-"))
	mkMember(creator.String(), map[string]string{"coding": "100.0"})
	projectID, _ := k.CreateProject(ctx, creator, "CjrProj", "D", []string{"coding"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.NewInt(50000), math.NewInt(5000), false)
	k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("cjr-approver-addr")), math.NewInt(50000), math.NewInt(5000))

	assignee := sdk.AccAddress([]byte("cjr-assignee-aaaa"))
	mkMember(assignee.String(), map[string]string{"coding": "1000.0"})
	initID, _ := k.CreateInitiative(ctx, assignee, projectID, "CjrInit", "D", []string{"coding"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, "", math.NewInt(150))
	k.AssignInitiativeToMember(ctx, initID, assignee)
	k.SubmitInitiativeWork(ctx, initID, assignee, "URI")

	challenger := sdk.AccAddress([]byte("cjr-challenger-aa"))
	mkMember(challenger.String(), map[string]string{"coding": "999.0"}) // high rep, but a party
	k.MintDREAM(ctx, challenger, math.NewInt(1000000000))
	chalID, err := k.CreateChallenge(ctx, challenger, initID, "Bad work", nil, math.NewInt(50000000))
	require.NoError(t, err)

	for i := 0; i < 5; i++ {
		a := sdk.AccAddress([]byte(fmt.Sprintf("%-16.16s", fmt.Sprintf("cjr-juror-%d", i))))
		mkMember(a.String(), map[string]string{"coding": "100.0"})
	}

	require.NoError(t, k.CreateJuryReview(ctx, chalID, "defense", nil))

	// Exactly one PENDING review now exists, it belongs to our challenge, and the
	// challenger was excluded from its seated jury.
	var found *types.JuryReview
	active := 0
	k.IterateActiveJuryReviews(ctx, func(_ int64, r types.JuryReview) bool {
		active++
		if r.ChallengeId == chalID {
			cp := r
			found = &cp
		}
		return false
	})
	require.Equal(t, 1, active, "CreateJuryReview must index the review as PENDING")
	require.NotNil(t, found)
	for _, j := range found.Jurors {
		require.NotEqual(t, challenger.String(), j, "challenger must not be seated")
	}
}

// TestChallengeDeadlineSweep covers ResolveExpiredChallengeJuryReviews: a
// challenge jury review that doesn't reach a verdict by votes before its
// (block-height) deadline is resolved by the EndBlocker sweep — decisively if a
// quorum voted, INCONCLUSIVE otherwise. Mirrors the scaffolding in
// TestTallyJuryVotes (real project/initiative/challenge), then indexes the
// review exactly as CreateJuryReview now does.
func TestChallengeDeadlineSweep(t *testing.T) {
	const reviewDeadline = int64(1000)

	setup := func(t *testing.T, jurorCount int) (*fixture, uint64, uint64, []string) {
		t.Helper()
		f := initFixture(t)
		k := f.keeper
		ctx := f.ctx

		params, _ := k.Params.Get(ctx)
		params.JurySize = 5
		params.JurySuperMajority = math.LegacyNewDecWithPrec(67, 2)
		params.MinJurorReputation = math.LegacyNewDec(50)
		require.NoError(t, k.Params.Set(ctx, params))

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

		projectCreator := sdk.AccAddress([]byte("cds-proj-creator"))
		mkMember(projectCreator.String(), map[string]string{"coding": "100.0"})
		projectID, _ := k.CreateProject(ctx, projectCreator, "CdsProj", "Desc",
			[]string{"coding"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE,
			"technical", math.NewInt(50000), math.NewInt(5000), false)
		k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("cds-approver-addr")), math.NewInt(50000), math.NewInt(5000))

		assignee := sdk.AccAddress([]byte("cds-assignee-aaaa"))
		mkMember(assignee.String(), map[string]string{"coding": "1000.0"})
		initID, _ := k.CreateInitiative(ctx, assignee, projectID, "CdsInit", "D",
			[]string{"coding"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD,
			types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, "", math.NewInt(150))
		k.AssignInitiativeToMember(ctx, initID, assignee)
		k.SubmitInitiativeWork(ctx, initID, assignee, "URI")

		challenger := sdk.AccAddress([]byte("cds-challenger-aa"))
		mkMember(challenger.String(), map[string]string{})
		k.MintDREAM(ctx, challenger, math.NewInt(1000000000))
		chalID, err := k.CreateChallenge(ctx, challenger, initID, "Bad work", nil, math.NewInt(50000000))
		require.NoError(t, err)
		ch, _ := k.GetChallenge(ctx, chalID)
		ch.Status = types.ChallengeStatus_CHALLENGE_STATUS_IN_JURY_REVIEW
		require.NoError(t, k.Challenge.Set(ctx, chalID, ch))

		jurors := make([]string, jurorCount)
		for i := 0; i < jurorCount; i++ {
			a := sdk.AccAddress([]byte(fmt.Sprintf("%-16.16s", fmt.Sprintf("cds-juror-%d", i))))
			mkMember(a.String(), map[string]string{"coding": "100.0"})
			jurors[i] = a.String()
		}

		reqVotes := params.JurySuperMajority.MulInt64(int64(len(jurors))).Ceil().TruncateInt().Uint64()
		jrID, _ := k.JuryReviewSeq.Next(ctx)
		jr := types.JuryReview{
			Id:                jrID,
			ChallengeId:       chalID,
			InitiativeId:      initID,
			Jurors:            jurors,
			RequiredVotes:     uint32(reqVotes),
			ReviewDeliverable: "URI",
			ChallengerClaim:   "Bad work",
			Votes:             []*types.JurorVote{},
			Deadline:          reviewDeadline, // block height
			Verdict:           types.Verdict_VERDICT_PENDING,
		}
		require.NoError(t, k.JuryReview.Set(ctx, jrID, jr))
		// Mirror what CreateJuryReview now does on creation.
		require.NoError(t, k.AddJuryReviewToVerdictIndex(ctx, jr))

		return f, jrID, chalID, jurors
	}

	castVotes := func(t *testing.T, f *fixture, jrID uint64, jurors []string, n int, v types.Verdict) {
		t.Helper()
		for i := 0; i < n; i++ {
			addr, err := sdk.AccAddressFromBech32(jurors[i])
			require.NoError(t, err)
			require.NoError(t, f.keeper.SubmitJurorVote(
				f.ctx, jrID, addr, nil, v, math.LegacyMustNewDecFromStr("0.9"), "vote"))
		}
	}

	countActive := func(f *fixture, ctx sdk.Context) int {
		n := 0
		f.keeper.IterateActiveJuryReviews(ctx, func(_ int64, _ types.JuryReview) bool {
			n++
			return false
		})
		return n
	}

	t.Run("quorum + decisive votes resolve the challenge at the deadline", func(t *testing.T) {
		f, jrID, chalID, jurors := setup(t, 5)

		// 3 of 5 vote uphold: below the supermajority trigger (4 → no early
		// resolution) but at/above the deadline quorum (3) and unanimous.
		castVotes(t, f, jrID, jurors, 3, types.Verdict_VERDICT_UPHOLD_CHALLENGE)

		ch, _ := f.keeper.GetChallenge(f.ctx, chalID)
		require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_IN_JURY_REVIEW, ch.Status,
			"must still be pending before the deadline")
		require.Equal(t, 1, countActive(f, f.ctx), "review must be in the PENDING index")

		// Advance past the (block-height) deadline and run the sweep.
		future := f.ctx.WithBlockHeight(reviewDeadline + 1)
		require.NoError(t, f.keeper.ResolveExpiredChallengeJuryReviews(future))

		jr, _ := f.keeper.GetJuryReview(future, jrID)
		require.Equal(t, types.Verdict_VERDICT_UPHOLD_CHALLENGE, jr.Verdict)
		ch, _ = f.keeper.GetChallenge(future, chalID)
		require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_UPHELD, ch.Status)
		require.Equal(t, 0, countActive(f, future), "resolved review must leave the PENDING index")
	})

	t.Run("sub-quorum votes go INCONCLUSIVE at the deadline", func(t *testing.T) {
		f, jrID, chalID, jurors := setup(t, 5)

		// Only 2 of 5 vote — below the deadline quorum (3).
		castVotes(t, f, jrID, jurors, 2, types.Verdict_VERDICT_UPHOLD_CHALLENGE)

		future := f.ctx.WithBlockHeight(reviewDeadline + 1)
		require.NoError(t, f.keeper.ResolveExpiredChallengeJuryReviews(future))

		jr, _ := f.keeper.GetJuryReview(future, jrID)
		require.Equal(t, types.Verdict_VERDICT_INCONCLUSIVE, jr.Verdict)
		ch, _ := f.keeper.GetChallenge(future, chalID)
		require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_IN_JURY_REVIEW, ch.Status,
			"inconclusive must not decide the challenge")
		require.Equal(t, 0, countActive(f, future), "review must leave the PENDING index even when inconclusive")
	})
}
