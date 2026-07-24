package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// newActiveProject creates and approves a budget-backed project owned by
// creator, returning its ID. The default fixture authorizes the approver.
func newActiveProject(t *testing.T, k keeper.Keeper, ctx sdk.Context, creator sdk.AccAddress) uint64 {
	t.Helper()
	projectID, err := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(100000), math.NewInt(1000), false)
	require.NoError(t, err)
	require.NoError(t, k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(100000), math.NewInt(1000)))
	return projectID
}

func TestCancelProjectCascadesToOpenInitiatives(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	creator := sdk.AccAddress([]byte("creator"))
	projectID := newActiveProject(t, k, ctx, creator)

	// Two OPEN initiatives, budget 100 each -> 200 allocated.
	budget := math.NewInt(100)
	initA, err := k.CreateInitiative(ctx, creator, projectID, "A", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, "", budget)
	require.NoError(t, err)
	initB, err := k.CreateInitiative(ctx, creator, projectID, "B", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, "", budget)
	require.NoError(t, err)

	projBefore, err := k.GetProject(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(200).String(), keeper.DerefInt(projBefore.AllocatedBudget).String())

	// Cancel the project.
	require.NoError(t, k.CancelProject(ctx, projectID, "no longer needed"))

	// Both OPEN initiatives are now CANCELLED.
	for _, id := range []uint64{initA, initB} {
		got, gerr := k.GetInitiative(ctx, id)
		require.NoError(t, gerr)
		require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_CANCELLED, got.Status, "initiative %d", id)
	}

	// Their reserved budget was returned, and the project itself is CANCELLED.
	projAfter, err := k.GetProject(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, types.ProjectStatus_PROJECT_STATUS_CANCELLED, projAfter.Status)
	require.Equal(t, math.ZeroInt().String(), keeper.DerefInt(projAfter.AllocatedBudget).String())
}

// setupSubmittedInitiative returns a project + an initiative advanced to
// SUBMITTED with its required conviction zeroed so it is otherwise completable.
func setupSubmittedInitiative(t *testing.T, f *fixture) (uint64, uint64, sdk.AccAddress) {
	t.Helper()
	k := f.keeper
	ctx := f.ctx

	creator := sdk.AccAddress([]byte("creator"))
	projectID := newActiveProject(t, k, ctx, creator)

	initID, err := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, "", math.NewInt(100))
	require.NoError(t, err)

	assignee := sdk.AccAddress([]byte("assignee"))
	require.NoError(t, k.Member.Set(ctx, assignee.String(), types.Member{
		Address:          assignee.String(),
		DreamBalance:     keeper.PtrInt(math.ZeroInt()),
		StakedDream:      keeper.PtrInt(math.ZeroInt()),
		LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
		LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"tag": "100.0"},
	}))
	require.NoError(t, k.AssignInitiativeToMember(ctx, initID, assignee))
	require.NoError(t, k.SubmitInitiativeWork(ctx, initID, assignee, "uri"))

	initiative, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	initiative.RequiredConviction = keeper.PtrDec(math.LegacyZeroDec())
	require.NoError(t, k.Initiative.Set(ctx, initID, initiative))

	return projectID, initID, assignee
}

func TestCancelProjectTerminatesInFlightInitiative(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	projectID, initID, assignee := setupSubmittedInitiative(t, f)

	// Sanity: before cancellation the initiative is completable.
	can, err := k.CanCompleteInitiative(ctx, initID)
	require.NoError(t, err)
	require.True(t, can)

	projBefore, err := k.GetProject(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(100).String(), keeper.DerefInt(projBefore.AllocatedBudget).String())

	// Cancel the parent project. The cascade force-terminates in-flight work.
	require.NoError(t, k.CancelProject(ctx, projectID, "pivoting"))

	// The SUBMITTED initiative is now CANCELLED, not left in limbo.
	got, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_CANCELLED, got.Status)

	// Its reserved budget was returned to the project.
	projAfter, err := k.GetProject(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, math.ZeroInt().String(), keeper.DerefInt(projAfter.AllocatedBudget).String())

	// Completion can no longer fire (terminal status) and no DREAM was minted.
	can, err = k.CanCompleteInitiative(ctx, initID)
	require.NoError(t, err)
	require.False(t, can)

	member, err := k.GetMember(ctx, assignee)
	require.NoError(t, err)
	require.Equal(t, math.ZeroInt().String(), keeper.DerefInt(member.DreamBalance).String())
}

func TestCancelProjectVoidsActiveChallengeAndRefundsChallenger(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	projectID, initID, _ := setupSubmittedInitiative(t, f)

	// A challenger with DREAM raises a challenge on the SUBMITTED initiative.
	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	stake := params.MinChallengeStake
	startBalance := stake.Mul(math.NewInt(10))

	challenger := sdk.AccAddress([]byte("challenger-x"))
	require.NoError(t, k.Member.Set(ctx, challenger.String(), types.Member{
		Address:          challenger.String(),
		DreamBalance:     keeper.PtrInt(startBalance),
		StakedDream:      keeper.PtrInt(math.ZeroInt()),
		LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
		LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"tag": "100.0"},
	}))

	challengeID, err := k.CreateChallenge(ctx, challenger, initID, "bad work", []string{"evidence"}, stake)
	require.NoError(t, err)

	// The stake is locked and the initiative is CHALLENGED.
	challenged, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_CHALLENGED, challenged.Status)
	locked, err := k.GetMember(ctx, challenger)
	require.NoError(t, err)
	require.Equal(t, stake.String(), keeper.DerefInt(locked.StakedDream).String())

	// Cancel the project.
	require.NoError(t, k.CancelProject(ctx, projectID, "pivoting"))

	// The initiative is CANCELLED and its challenge VOIDED.
	got, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_CANCELLED, got.Status)

	challenge, err := k.GetChallenge(ctx, challengeID)
	require.NoError(t, err)
	require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_VOIDED, challenge.Status)

	// The challenger's stake was refunded in full: unlocked (StakedDream back
	// to zero) and never burned (DreamBalance unchanged).
	refunded, err := k.GetMember(ctx, challenger)
	require.NoError(t, err)
	require.Equal(t, math.ZeroInt().String(), keeper.DerefInt(refunded.StakedDream).String())
	require.Equal(t, startBalance.String(), keeper.DerefInt(refunded.DreamBalance).String())

	// No active challenge remains on the initiative.
	hasActive, err := k.HasActiveChallenges(ctx, initID)
	require.NoError(t, err)
	require.False(t, hasActive)
}

// TestCancelProjectVoidsChallengeInJuryReview exercises the jury-review void
// path: a challenge that has reached IN_JURY_REVIEW with a PENDING jury review
// must, on project cancel, have its challenge VOIDED, its stake refunded, and
// its jury review closed INCONCLUSIVE and pulled off the pending index so the
// EndBlocker resolver never tallies a verdict on the cancelled initiative.
func TestCancelProjectVoidsChallengeInJuryReview(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	projectID, initID, _ := setupSubmittedInitiative(t, f)

	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	stake := params.MinChallengeStake
	startBalance := stake.Mul(math.NewInt(10))

	challenger := sdk.AccAddress([]byte("jury-challenger"))
	require.NoError(t, k.Member.Set(ctx, challenger.String(), types.Member{
		Address:          challenger.String(),
		DreamBalance:     keeper.PtrInt(startBalance),
		StakedDream:      keeper.PtrInt(math.ZeroInt()),
		LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
		LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"tag": "100.0"},
	}))

	challengeID, err := k.CreateChallenge(ctx, challenger, initID, "bad work", []string{"evidence"}, stake)
	require.NoError(t, err)

	// Advance the challenge to IN_JURY_REVIEW with a PENDING jury review linked
	// to it (constructed directly to avoid depending on jury-selection quorum).
	challenge, err := k.GetChallenge(ctx, challengeID)
	require.NoError(t, err)
	oldStatus := challenge.Status
	challenge.Status = types.ChallengeStatus_CHALLENGE_STATUS_IN_JURY_REVIEW
	require.NoError(t, k.SetChallenge(ctx, challenge))
	require.NoError(t, k.UpdateChallengeStatusIndex(ctx, oldStatus, challenge.Status, challenge.Id))

	reviewID, err := k.JuryReviewSeq.Next(ctx)
	require.NoError(t, err)
	review := types.JuryReview{
		Id:          reviewID,
		ChallengeId: challengeID,
		Verdict:     types.Verdict_VERDICT_PENDING,
		Deadline:    ctx.BlockHeight() + 1000,
	}
	require.NoError(t, k.JuryReview.Set(ctx, reviewID, review))
	require.NoError(t, k.AddJuryReviewToVerdictIndex(ctx, review))

	// Sanity: the review is on the pending index before cancellation.
	var pendingCount int
	k.IterateActiveJuryReviews(ctx, func(_ int64, r types.JuryReview) bool {
		if r.Id == reviewID {
			pendingCount++
		}
		return false
	})
	require.Equal(t, 1, pendingCount)

	// Cancel the project.
	require.NoError(t, k.CancelProject(ctx, projectID, "pivoting"))

	// Challenge VOIDED and challenger fully refunded.
	gotChallenge, err := k.GetChallenge(ctx, challengeID)
	require.NoError(t, err)
	require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_VOIDED, gotChallenge.Status)
	refunded, err := k.GetMember(ctx, challenger)
	require.NoError(t, err)
	require.Equal(t, math.ZeroInt().String(), keeper.DerefInt(refunded.StakedDream).String())
	require.Equal(t, startBalance.String(), keeper.DerefInt(refunded.DreamBalance).String())

	// Jury review closed INCONCLUSIVE and off the pending index.
	gotReview, err := k.GetJuryReview(ctx, reviewID)
	require.NoError(t, err)
	require.Equal(t, types.Verdict_VERDICT_INCONCLUSIVE, gotReview.Verdict)

	pendingCount = 0
	k.IterateActiveJuryReviews(ctx, func(_ int64, r types.JuryReview) bool {
		if r.Id == reviewID {
			pendingCount++
		}
		return false
	})
	require.Equal(t, 0, pendingCount)
}

// TestCompletionGuardBlocksCancelledProjectPayout exercises the defence-in-depth
// completion guard directly: even if a non-terminal initiative somehow sits
// under a CANCELLED project (the cascade normally prevents this), no DREAM can
// be minted. The project status is forced to CANCELLED without the cascade so
// the SUBMITTED initiative survives to reach the guard.
func TestCompletionGuardBlocksCancelledProjectPayout(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	projectID, initID, assignee := setupSubmittedInitiative(t, f)

	// Force the parent project CANCELLED directly (bypassing the cascade) so the
	// initiative remains SUBMITTED under a cancelled project.
	project, err := k.GetProject(ctx, projectID)
	require.NoError(t, err)
	project.Status = types.ProjectStatus_PROJECT_STATUS_CANCELLED
	require.NoError(t, k.Project.Set(ctx, projectID, project))

	got, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED, got.Status)

	// The EndBlocker path is silently blocked...
	can, err := k.CanCompleteInitiative(ctx, initID)
	require.NoError(t, err)
	require.False(t, can)

	// ...and the direct/manual path errors clearly.
	err = k.CompleteInitiative(ctx, initID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "parent project")
	require.Contains(t, err.Error(), "is cancelled")

	// No DREAM minted to the assignee.
	member, err := k.GetMember(ctx, assignee)
	require.NoError(t, err)
	require.Equal(t, math.ZeroInt().String(), keeper.DerefInt(member.DreamBalance).String())
}

// initiativeInStatusBucket reports whether the by-status index bucket for the
// given status contains the initiative id.
func initiativeInStatusBucket(t *testing.T, k keeper.Keeper, ctx sdk.Context, status types.InitiativeStatus, id uint64) bool {
	t.Helper()
	found := false
	require.NoError(t, k.IterateInitiativesByStatus(ctx, status, func(gotID uint64) bool {
		if gotID == id {
			found = true
			return true
		}
		return false
	}))
	return found
}

// TestVoidedChallengeIsSealed locks in the post-void guards: once a project
// cancel voids a challenge (and closes its jury review INCONCLUSIVE), late
// juror votes are rejected and the challenge can never be upheld or rejected —
// so the refunded challenger cannot be double-refunded or retro-punished, and
// the CANCELLED initiative cannot be resurrected by a stale jury tally.
func TestVoidedChallengeIsSealed(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	projectID, initID, _ := setupSubmittedInitiative(t, f)

	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	stake := params.MinChallengeStake
	startBalance := stake.Mul(math.NewInt(10))

	challenger := sdk.AccAddress([]byte("sealed-challenger"))
	require.NoError(t, k.Member.Set(ctx, challenger.String(), types.Member{
		Address:          challenger.String(),
		DreamBalance:     keeper.PtrInt(startBalance),
		StakedDream:      keeper.PtrInt(math.ZeroInt()),
		LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
		LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"tag": "100.0"},
	}))

	challengeID, err := k.CreateChallenge(ctx, challenger, initID, "bad work", []string{"evidence"}, stake)
	require.NoError(t, err)

	// Advance to IN_JURY_REVIEW with a PENDING review carrying a juror and a
	// future deadline — the exact state a late vote would arrive in.
	challenge, err := k.GetChallenge(ctx, challengeID)
	require.NoError(t, err)
	oldStatus := challenge.Status
	challenge.Status = types.ChallengeStatus_CHALLENGE_STATUS_IN_JURY_REVIEW
	require.NoError(t, k.SetChallenge(ctx, challenge))
	require.NoError(t, k.UpdateChallengeStatusIndex(ctx, oldStatus, challenge.Status, challenge.Id))

	juror := sdk.AccAddress([]byte("late-juror"))
	reviewID, err := k.JuryReviewSeq.Next(ctx)
	require.NoError(t, err)
	review := types.JuryReview{
		Id:            reviewID,
		ChallengeId:   challengeID,
		Jurors:        []string{juror.String()},
		RequiredVotes: 1,
		Verdict:       types.Verdict_VERDICT_PENDING,
		Deadline:      ctx.BlockHeight() + 1000,
	}
	require.NoError(t, k.JuryReview.Set(ctx, reviewID, review))
	require.NoError(t, k.AddJuryReviewToVerdictIndex(ctx, review))

	// Void via project cancel.
	require.NoError(t, k.CancelProject(ctx, projectID, "pivoting"))

	// A late juror vote on the voided review is rejected outright (it would
	// otherwise immediately tally, RequiredVotes being 1).
	err = k.SubmitJurorVote(ctx, reviewID, juror, nil,
		types.Verdict_VERDICT_REJECT_CHALLENGE, math.LegacyOneDec(), "too late")
	require.Error(t, err)
	require.Contains(t, err.Error(), "already resolved")

	// Direct resolution of the voided challenge is rejected on both branches.
	err = k.UpholdChallenge(ctx, challengeID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "already resolved")
	err = k.RejectChallenge(ctx, challengeID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "already resolved")

	// Nothing moved: challenge stays VOIDED, initiative stays CANCELLED, and
	// the challenger keeps the full refund (nothing locked, nothing burned).
	gotChallenge, err := k.GetChallenge(ctx, challengeID)
	require.NoError(t, err)
	require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_VOIDED, gotChallenge.Status)

	gotInit, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_CANCELLED, gotInit.Status)

	member, err := k.GetMember(ctx, challenger)
	require.NoError(t, err)
	require.Equal(t, math.ZeroInt().String(), keeper.DerefInt(member.StakedDream).String())
	require.Equal(t, startBalance.String(), keeper.DerefInt(member.DreamBalance).String())
}

// TestChallengeResolutionMaintainsInitiativeStatusIndex locks in the index fix:
// CreateChallenge and RejectChallenge move the initiative between by-status
// buckets instead of leaving stale entries behind.
func TestChallengeResolutionMaintainsInitiativeStatusIndex(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	_, initID, _ := setupSubmittedInitiative(t, f)
	require.True(t, initiativeInStatusBucket(t, k, ctx, types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED, initID))

	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	stake := params.MinChallengeStake

	challenger := sdk.AccAddress([]byte("idx-challenger"))
	require.NoError(t, k.Member.Set(ctx, challenger.String(), types.Member{
		Address:          challenger.String(),
		DreamBalance:     keeper.PtrInt(stake.Mul(math.NewInt(10))),
		StakedDream:      keeper.PtrInt(math.ZeroInt()),
		LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
		LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"tag": "100.0"},
	}))

	// CreateChallenge: SUBMITTED bucket -> CHALLENGED bucket.
	challengeID, err := k.CreateChallenge(ctx, challenger, initID, "bad work", []string{"evidence"}, stake)
	require.NoError(t, err)
	require.False(t, initiativeInStatusBucket(t, k, ctx, types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED, initID))
	require.True(t, initiativeInStatusBucket(t, k, ctx, types.InitiativeStatus_INITIATIVE_STATUS_CHALLENGED, initID))

	// RejectChallenge: CHALLENGED bucket -> IN_REVIEW bucket.
	require.NoError(t, k.RejectChallenge(ctx, challengeID))
	require.False(t, initiativeInStatusBucket(t, k, ctx, types.InitiativeStatus_INITIATIVE_STATUS_CHALLENGED, initID))
	require.True(t, initiativeInStatusBucket(t, k, ctx, types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW, initID))

	// UpholdChallenge: CHALLENGED bucket -> REJECTED bucket (fresh initiative;
	// the burn of the reject path above consumed the challenger's stake, so
	// re-fund before staking again).
	_, initID2, _ := setupSubmittedInitiative(t, f)
	member, err := k.GetMember(ctx, challenger)
	require.NoError(t, err)
	member.DreamBalance = keeper.PtrInt(keeper.DerefInt(member.DreamBalance).Add(stake.Mul(math.NewInt(10))))
	require.NoError(t, k.Member.Set(ctx, challenger.String(), member))

	challengeID2, err := k.CreateChallenge(ctx, challenger, initID2, "bad work too", []string{"evidence"}, stake)
	require.NoError(t, err)
	require.True(t, initiativeInStatusBucket(t, k, ctx, types.InitiativeStatus_INITIATIVE_STATUS_CHALLENGED, initID2))

	require.NoError(t, k.UpholdChallenge(ctx, challengeID2))
	require.False(t, initiativeInStatusBucket(t, k, ctx, types.InitiativeStatus_INITIATIVE_STATUS_CHALLENGED, initID2))
	require.True(t, initiativeInStatusBucket(t, k, ctx, types.InitiativeStatus_INITIATIVE_STATUS_REJECTED, initID2))
}
