package keeper_test

import (
	"context"
	"fmt"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// Regression coverage for the resolution-hardening changes: every path that
// resolves an initiative challenge must either commit in full or leave the
// dispute retryable, and a rejected challenge must never skip the challenge
// window of work that was contested before that window opened.

// juryDeadlineReview seats a hand-crafted PENDING jury review (indexed exactly
// as CreateJuryReview does) with 3 of 5 uphold votes cast — decisive at the
// deadline tally, but below the supermajority trigger that would have resolved
// it by votes. Mirrors the scaffolding of TestChallengeDeadlineSweep.
func juryDeadlineReview(t *testing.T, f *fixture, reviewDeadline int64) (jrID, chalID, initID uint64, assignee sdk.AccAddress) {
	t.Helper()
	k, ctx := f.keeper, f.ctx

	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
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

	projectCreator := sdk.AccAddress([]byte("rh-proj-creator--"))
	mkMember(projectCreator.String(), map[string]string{"coding": "100.0"})
	projectID, err := k.CreateProject(ctx, projectCreator, "RhProj", "Desc",
		[]string{"coding"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE,
		"technical", math.NewInt(50000), math.NewInt(5000), false)
	require.NoError(t, err)
	require.NoError(t, k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("rh-approver-addr")),
		math.NewInt(50000), math.NewInt(5000)))

	assignee = sdk.AccAddress([]byte("rh-assignee-aaaa"))
	mkMember(assignee.String(), map[string]string{"coding": "1000.0"})
	initID, err = k.CreateInitiative(ctx, assignee, projectID, "RhInit", "D",
		[]string{"coding"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(150))
	require.NoError(t, err)
	k.MintDREAM(ctx, assignee, math.NewInt(1000000))
	require.NoError(t, k.AssignInitiativeToMember(ctx, initID, assignee))
	require.NoError(t, k.SubmitInitiativeWork(ctx, initID, assignee, "URI"))

	challenger := sdk.AccAddress([]byte("rh-challenger-aa"))
	mkMember(challenger.String(), map[string]string{})
	k.MintDREAM(ctx, challenger, math.NewInt(1000000000))
	chalID, err = k.CreateChallenge(ctx, challenger, initID, "Bad work", nil, math.NewInt(50000000))
	require.NoError(t, err)
	ch, err := k.GetChallenge(ctx, chalID)
	require.NoError(t, err)
	ch.Status = types.ChallengeStatus_CHALLENGE_STATUS_IN_JURY_REVIEW
	require.NoError(t, k.Challenge.Set(ctx, chalID, ch))
	require.NoError(t, k.UpdateChallengeStatusIndex(ctx,
		types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE, ch.Status, chalID))

	jurors := make([]string, 5)
	for i := range jurors {
		a := sdk.AccAddress([]byte(fmt.Sprintf("%-16.16s", fmt.Sprintf("rh-juror-%d", i))))
		mkMember(a.String(), map[string]string{"coding": "100.0"})
		jurors[i] = a.String()
	}

	reqVotes := params.JurySuperMajority.MulInt64(int64(len(jurors))).Ceil().TruncateInt().Uint64()
	jrID, err = k.JuryReviewSeq.Next(ctx)
	require.NoError(t, err)
	jr := types.JuryReview{
		Id:                jrID,
		ChallengeId:       chalID,
		InitiativeId:      initID,
		Jurors:            jurors,
		RequiredVotes:     uint32(reqVotes),
		ReviewDeliverable: "URI",
		ChallengerClaim:   "Bad work",
		Votes:             []*types.JurorVote{},
		Deadline:          reviewDeadline,
		Verdict:           types.Verdict_VERDICT_PENDING,
	}
	require.NoError(t, k.JuryReview.Set(ctx, jrID, jr))
	require.NoError(t, k.AddJuryReviewToVerdictIndex(ctx, jr))

	// 3 of 5 uphold: below the supermajority trigger (4), at the deadline
	// quorum (3) and unanimous — decisive at the deadline tally.
	for i := 0; i < 3; i++ {
		addr, err := sdk.AccAddressFromBech32(jurors[i])
		require.NoError(t, err)
		require.NoError(t, k.SubmitJurorVote(ctx, jrID, addr, nil,
			types.Verdict_VERDICT_UPHOLD_CHALLENGE, math.LegacyMustNewDecFromStr("0.9"), "vote"))
	}
	return jrID, chalID, initID, assignee
}

// TestDeadlineSweepRetriesFailedTally: TallyJuryVotes de-indexes the review
// before it resolves the challenge, so a resolution failure in the EndBlocker
// deadline sweep used to commit the de-indexing and strand the challenge in
// IN_JURY_REVIEW forever. The sweep must discard the failed branch and retry.
func TestDeadlineSweepRetriesFailedTally(t *testing.T) {
	const reviewDeadline = int64(1000)
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	jrID, chalID, initID, assignee := juryDeadlineReview(t, f, reviewDeadline)

	// Deterministic failure inside the resolution: the assignee's member record
	// is gone, so UpholdChallenge fails at GetMember before writing anything.
	assigneeMember, err := k.GetMember(ctx, assignee)
	require.NoError(t, err)
	require.NoError(t, k.Member.Remove(ctx, assignee.String()))

	future := ctx.WithBlockHeight(reviewDeadline + 1)
	require.NoError(t, k.ResolveExpiredChallengeJuryReviews(future))

	// Nothing committed: the verdict is still PENDING, the review is still in
	// the PENDING verdict index (i.e. the sweep will see it again), and the
	// challenge is unresolved.
	jr, err := k.GetJuryReview(future, jrID)
	require.NoError(t, err)
	require.Equal(t, types.Verdict_VERDICT_PENDING, jr.Verdict)
	ch, err := k.GetChallenge(future, chalID)
	require.NoError(t, err)
	require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_IN_JURY_REVIEW, ch.Status)
	pending := 0
	k.IterateActiveJuryReviews(future, func(_ int64, _ types.JuryReview) bool {
		pending++
		return false
	})
	require.Equal(t, 1, pending, "a failed tally must keep the review retryable")

	// Healing the record lets the next block's sweep finish the job.
	require.NoError(t, k.Member.Set(future, assignee.String(), assigneeMember))
	require.NoError(t, k.ResolveExpiredChallengeJuryReviews(future))

	ch, err = k.GetChallenge(future, chalID)
	require.NoError(t, err)
	require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_UPHELD, ch.Status)
	initiative, err := k.GetInitiative(future, initID)
	require.NoError(t, err)
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_REJECTED, initiative.Status)
}

// TestExpireInterimRetriesFailedResolution: the adjudication-timeout default
// resolves the challenge by rejecting it. A mid-way failure of that rejection
// used to be logged after the interim was already EXPIRED — out of every
// sweep, with the resolution half-done. Expiry and resolution now commit
// together, and a failure leaves both retryable.
func TestExpireInterimRetriesFailedResolution(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	initID, chalID, challenger := challengedInitiative(t, k, ctx)

	interimID, err := k.CreateInterimWork(ctx,
		types.InterimType_INTERIM_TYPE_ADJUDICATION,
		[]string{k.GetAuthorityString()},
		"technical_operations", initID, "Inconclusive jury",
		types.InterimComplexity_INTERIM_COMPLEXITY_EPIC, ctx.BlockHeight()+10)
	require.NoError(t, err)

	// RejectChallenge fails at UnlockDREAM once the challenger has no member
	// record.
	challengerMember, err := k.GetMember(ctx, challenger)
	require.NoError(t, err)
	require.NoError(t, k.Member.Remove(ctx, challenger.String()))

	err = k.ExpireInterim(ctx, interimID)
	require.Error(t, err)

	// Nothing committed: the interim is still live and the challenge still
	// awaits its verdict.
	interim, err := k.GetInterim(ctx, interimID)
	require.NoError(t, err)
	require.Equal(t, types.InterimStatus_INTERIM_STATUS_PENDING, interim.Status,
		"the expiry must be discarded with the failed resolution so it can retry")
	ch, err := k.GetChallenge(ctx, chalID)
	require.NoError(t, err)
	require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_IN_JURY_REVIEW, ch.Status)

	// Next block, with the record healed, the expiry completes and resolves.
	require.NoError(t, k.Member.Set(ctx, challenger.String(), challengerMember))
	require.NoError(t, k.ExpireInterim(ctx, interimID))

	interim, err = k.GetInterim(ctx, interimID)
	require.NoError(t, err)
	require.Equal(t, types.InterimStatus_INTERIM_STATUS_EXPIRED, interim.Status)
	ch, err = k.GetChallenge(ctx, chalID)
	require.NoError(t, err)
	require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_REJECTED, ch.Status)
}

// TestRejectChallengeRestoresPreChallengeStatus: a challenge may be filed while
// the work is still SUBMITTED — before its challenge window opens — and
// rejecting it used to flip the initiative to IN_REVIEW with ChallengePeriodEnd
// 0, which the completion sweep read as "window elapsed" (height >= 0) and paid
// out in the next block. The rejected challenge must restore the snapshotted
// pre-challenge status instead.
func TestRejectChallengeRestoresPreChallengeStatus(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	_, initID, _ := setupSubmittedInitiative(t, f)

	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	stake := params.MinChallengeStake
	challenger := sdk.AccAddress([]byte("rh-restore-challenger"))
	require.NoError(t, k.Member.Set(ctx, challenger.String(), types.Member{
		Address:          challenger.String(),
		DreamBalance:     keeper.PtrInt(stake.Mul(math.NewInt(10))),
		StakedDream:      keeper.PtrInt(math.ZeroInt()),
		LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
		LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"tag": "100.0"},
	}))

	chalID, err := k.CreateChallenge(ctx, challenger, initID, "bad work", nil, stake)
	require.NoError(t, err)

	initiative, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_CHALLENGED, initiative.Status)
	require.Zero(t, initiative.ChallengePeriodEnd, "precondition: the window never opened")

	require.NoError(t, k.RejectChallenge(ctx, chalID))

	initiative, err = k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED, initiative.Status,
		"a rejected challenge restores the status the work held when contested")
	require.Zero(t, initiative.ChallengePeriodEnd)

	t.Run("completion refuses an unopened window", func(t *testing.T) {
		// The legacy shape this fix defends against: IN_REVIEW with
		// ChallengePeriodEnd still 0 (pre-snapshot chains, or drifted state).
		// Payout must not read `height >= 0` as "window elapsed".
		legacy := initiative
		legacy.Status = types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW
		require.NoError(t, k.UpdateInitiative(ctx, legacy))

		err := k.CompleteInitiative(ctx, initID)
		require.ErrorIs(t, err, types.ErrChallengePeriodActive)

		// The EndBlocker heals the shape by opening a window, not by paying.
		require.NoError(t, k.EndBlocker(ctx))
		healed, err := k.GetInitiative(ctx, initID)
		require.NoError(t, err)
		require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW, healed.Status)
		require.NotZero(t, healed.ChallengePeriodEnd)
		require.Greater(t, healed.ChallengePeriodEnd, ctx.BlockHeight(),
			"the adopted window must actually run")
		require.NotEqual(t, types.InitiativeStatus_INITIATIVE_STATUS_COMPLETED, healed.Status)
	})

	t.Run("a zero snapshot restores IN_REVIEW like the old chain", func(t *testing.T) {
		_, initID2, _ := setupSubmittedInitiative(t, f)
		chalID2, err := k.CreateChallenge(ctx, challenger, initID2, "bad work too", nil, stake)
		require.NoError(t, err)

		// Pre-snapshot challenges carry the zero value (OPEN).
		ch, err := k.GetChallenge(ctx, chalID2)
		require.NoError(t, err)
		ch.StatusBeforeChallenge = types.InitiativeStatus_INITIATIVE_STATUS_OPEN
		require.NoError(t, k.Challenge.Set(ctx, chalID2, ch))

		require.NoError(t, k.RejectChallenge(ctx, chalID2))
		restored, err := k.GetInitiative(ctx, initID2)
		require.NoError(t, err)
		require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW, restored.Status)
	})
}

// TestCompleteAdjudicationInterimAppliesExplicitDecision: the committee's
// verdict is a structured field, not a keyword parsed out of free-text notes —
// an unmatched keyword (or a failed resolution) used to complete the interim
// and strand the challenge it was raised to settle, with nothing left to retry.
func TestCompleteAdjudicationInterimAppliesExplicitDecision(t *testing.T) {
	newCase := func(t *testing.T) (keeper.Keeper, sdk.Context, uint64, uint64, uint64) {
		f := initFixture(t)
		k, ctx := f.keeper, f.ctx
		initID, chalID, _ := challengedInitiative(t, k, ctx)
		interimID, err := k.CreateInterimWork(ctx,
			types.InterimType_INTERIM_TYPE_ADJUDICATION,
			[]string{k.GetAuthorityString()},
			"technical_operations", initID, "Inconclusive jury",
			types.InterimComplexity_INTERIM_COMPLEXITY_EPIC, ctx.BlockHeight()+10)
		require.NoError(t, err)
		return k, ctx, interimID, chalID, initID
	}

	t.Run("uphold resolves the challenge against the work", func(t *testing.T) {
		k, ctx, interimID, chalID, initID := newCase(t)
		require.NoError(t, k.CompleteInterimDirectly(ctx, interimID,
			types.AdjudicationDecision_ADJUDICATION_DECISION_UPHOLD, "work fails the criteria"))

		ch, err := k.GetChallenge(ctx, chalID)
		require.NoError(t, err)
		require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_UPHELD, ch.Status)
		initiative, err := k.GetInitiative(ctx, initID)
		require.NoError(t, err)
		require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_REJECTED, initiative.Status)
		interim, err := k.GetInterim(ctx, interimID)
		require.NoError(t, err)
		require.Equal(t, types.InterimStatus_INTERIM_STATUS_COMPLETED, interim.Status)
		require.Equal(t, types.AdjudicationDecision_ADJUDICATION_DECISION_UPHOLD, interim.Decision)
	})

	t.Run("reject resolves the challenge for the work", func(t *testing.T) {
		k, ctx, interimID, chalID, initID := newCase(t)
		require.NoError(t, k.CompleteInterimDirectly(ctx, interimID,
			types.AdjudicationDecision_ADJUDICATION_DECISION_REJECT, "work meets the criteria"))

		ch, err := k.GetChallenge(ctx, chalID)
		require.NoError(t, err)
		require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_REJECTED, ch.Status)
		// challengedInitiative contests SUBMITTED work, so the reject restores
		// SUBMITTED (see TestRejectChallengeRestoresPreChallengeStatus).
		initiative, err := k.GetInitiative(ctx, initID)
		require.NoError(t, err)
		require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED, initiative.Status)
	})

	t.Run("a missing decision is refused while the interim is still live", func(t *testing.T) {
		k, ctx, interimID, chalID, _ := newCase(t)
		err := k.CompleteInterimDirectly(ctx, interimID,
			types.AdjudicationDecision_ADJUDICATION_DECISION_UNSPECIFIED, "Committee decision: Challenge REJECTED")
		require.ErrorIs(t, err, types.ErrInvalidRequest)

		interim, err := k.GetInterim(ctx, interimID)
		require.NoError(t, err)
		require.Equal(t, types.InterimStatus_INTERIM_STATUS_PENDING, interim.Status,
			"the interim must stay completable, not complete-and-stranded")
		ch, err := k.GetChallenge(ctx, chalID)
		require.NoError(t, err)
		require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_IN_JURY_REVIEW, ch.Status)
	})

	t.Run("a failed resolution leaves both live", func(t *testing.T) {
		k, ctx, interimID, chalID, initID := newCase(t)
		// UpholdChallenge fails at GetMember once the assignee is gone.
		initiative, err := k.GetInitiative(ctx, initID)
		require.NoError(t, err)
		assignee, err := sdk.AccAddressFromBech32(initiative.Assignee)
		require.NoError(t, err)
		assigneeMember, err := k.GetMember(ctx, assignee)
		require.NoError(t, err)
		require.NoError(t, k.Member.Remove(ctx, assignee.String()))

		err = k.CompleteInterimDirectly(ctx, interimID,
			types.AdjudicationDecision_ADJUDICATION_DECISION_UPHOLD, "work fails")
		require.Error(t, err)

		interim, err := k.GetInterim(ctx, interimID)
		require.NoError(t, err)
		require.Equal(t, types.InterimStatus_INTERIM_STATUS_PENDING, interim.Status)
		ch, err := k.GetChallenge(ctx, chalID)
		require.NoError(t, err)
		require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_IN_JURY_REVIEW, ch.Status)

		// Retried after healing, the same decision lands.
		require.NoError(t, k.Member.Set(ctx, assignee.String(), assigneeMember))
		require.NoError(t, k.CompleteInterimDirectly(ctx, interimID,
			types.AdjudicationDecision_ADJUDICATION_DECISION_UPHOLD, "work fails"))
		ch, err = k.GetChallenge(ctx, chalID)
		require.NoError(t, err)
		require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_UPHELD, ch.Status)
	})
}

// TestAuthorityNotCappedOnAdjudicationInterims: every ADJUDICATION interim
// names the module authority as its assignee, so counting it against the
// per-member anti-monopolization cap made the 11th concurrent committee
// escalation fail to raise its interim at all.
func TestAuthorityNotCappedOnAdjudicationInterims(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	require.True(t, params.MaxActiveInterimsPerMember > 0, "precondition: a finite cap")

	authority := k.GetAuthorityString()
	for i := uint32(0); i < params.MaxActiveInterimsPerMember+2; i++ {
		_, err := k.CreateInterimWork(ctx,
			types.InterimType_INTERIM_TYPE_ADJUDICATION,
			[]string{authority},
			"technical_operations", uint64(i), "Inconclusive jury",
			types.InterimComplexity_INTERIM_COMPLEXITY_EPIC, ctx.BlockHeight()+1000)
		require.NoError(t, err, "adjudication interim %d past the member cap must still be raisable", i)
	}
}

// TestApproveInterimRefusesAdjudication closes the second door into the freeze
// CompleteInterimDirectly's explicit-decision requirement shut.
//
// MsgApproveInterim shares the Operations Committee gate with
// MsgCompleteInterim but carries only a bool, and "approved" cannot express a
// verdict — approving the committee's work is a different question from whether
// the challenge is upheld. Before the guard, `approved: false` finalized an
// adjudication interim to EXPIRED with no decision and no resolution; EXPIRED
// leaves IteratePendingInterims, so ExpireInterim's default-REJECT backstop
// never fired and the challenge sat in IN_JURY_REVIEW with nothing left to
// retry it.
func TestApproveInterimRefusesAdjudication(t *testing.T) {
	for _, approved := range []bool{true, false} {
		t.Run(fmt.Sprintf("approved=%v", approved), func(t *testing.T) {
			f := initFixture(t)
			k, ctx := f.keeper, f.ctx
			f.commonsKeeper.IsCommitteeMemberFn = func(_ context.Context, _ sdk.AccAddress, _ string, committee string) (bool, error) {
				return committee == "operations", nil
			}

			initID, chalID, _ := challengedInitiative(t, k, ctx)
			interimID, err := k.CreateInterimWork(ctx,
				types.InterimType_INTERIM_TYPE_ADJUDICATION,
				[]string{k.GetAuthorityString()},
				"technical_operations", initID, "Inconclusive jury",
				types.InterimComplexity_INTERIM_COMPLEXITY_EPIC, ctx.BlockHeight()+10)
			require.NoError(t, err)

			ops := sdk.AccAddress([]byte("ops_committee_member"))
			err = k.ApproveInterim(ctx, interimID, ops, approved, "committee says so")
			require.ErrorIs(t, err, types.ErrInvalidRequest,
				"an adjudication must be settled through MsgCompleteInterim, which carries a decision")

			// The interim stays completable and the dispute stays retryable.
			interim, err := k.GetInterim(ctx, interimID)
			require.NoError(t, err)
			require.Equal(t, types.InterimStatus_INTERIM_STATUS_PENDING, interim.Status,
				"the interim must stay in the pending sweep, not finalize undecided")
			ch, err := k.GetChallenge(ctx, chalID)
			require.NoError(t, err)
			require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_IN_JURY_REVIEW, ch.Status)

			// And the documented path still settles it.
			require.NoError(t, k.CompleteInterimDirectly(ctx, interimID,
				types.AdjudicationDecision_ADJUDICATION_DECISION_REJECT, "work meets the criteria"))
			ch, err = k.GetChallenge(ctx, chalID)
			require.NoError(t, err)
			require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_REJECTED, ch.Status)
		})
	}
}

// TestApproveInterimStillHandlesOrdinaryWork pins the other side of the guard:
// it keys on the ADJUDICATION type, so ordinary committee work is untouched.
func TestApproveInterimStillHandlesOrdinaryWork(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx
	f.commonsKeeper.IsCommitteeMemberFn = func(_ context.Context, _ sdk.AccAddress, _ string, committee string) (bool, error) {
		return committee == "operations", nil
	}

	assignee := sdk.AccAddress([]byte("ordinary_assignee___"))
	mkFundedMember(t, k, ctx, assignee, 10_000_000)

	interimID, err := k.CreateInterimWork(ctx,
		types.InterimType_INTERIM_TYPE_AUDIT,
		[]string{assignee.String()},
		"technical_operations", 1, "initiative",
		types.InterimComplexity_INTERIM_COMPLEXITY_STANDARD, ctx.BlockHeight()+10)
	require.NoError(t, err)

	ops := sdk.AccAddress([]byte("ops_committee_member"))
	require.NoError(t, k.ApproveInterim(ctx, interimID, ops, true, "good work"))

	interim, err := k.GetInterim(ctx, interimID)
	require.NoError(t, err)
	require.Equal(t, types.InterimStatus_INTERIM_STATUS_COMPLETED, interim.Status)
}

// TestEscalationTimeoutRetriesFailedRejection pins the containment on the
// escalation sweep's own terminal path.
//
// resolveSilentEscalations removes the EscalatedReviews entry and only then
// runs rejectReviewRound, which on an exhausted round runs CloseInitiative and
// the stake teardown. Committing the removal alongside a half-done close would
// leave the initiative retired-in-part with nothing scheduled to finish it —
// the shape that stranded devnet initiative #1. Each timeout now applies in its
// own cache branch, so a failure keeps the id in EscalatedReviews and the next
// block retries it.
func TestEscalationTimeoutRetriesFailedRejection(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	creator := newStakerMember(t, f, "esc_retry_creator___", math.NewInt(5_000_000_000))
	staker := newStakerMember(t, f, "esc_retry_staker____", math.NewInt(5_000_000_000))
	initID := newActiveInitiative(t, f, creator, "escretry")

	// A stake with something payable, so the teardown has a mint to fail on.
	stakeID, err := k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(1_000_000))
	require.NoError(t, err)
	require.NoError(t, k.InitSeasonalPool(ctx, 1))
	require.NoError(t, k.DistributeEpochStakingRewardsFromPool(ctx))
	pending, err := k.GetPendingStakingRewards(ctx, mustStake(t, f, stakeID))
	require.NoError(t, err)
	require.True(t, pending.IsPositive(), "precondition: the teardown has a reward to mint")

	params, err := k.Params.Get(ctx)
	require.NoError(t, err)

	// Seat the initiative on its last review round with an expired committee
	// window, and mark it escalated.
	initiative, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW
	initiative.ReviewRound = params.MaxReviewRounds - 1
	initiative.ReviewEscalation = types.ReviewEscalation_REVIEW_ESCALATION_NONE
	initiative.ReviewDeadline = ctx.BlockHeight() + 1
	require.NoError(t, k.UpdateInitiative(ctx, initiative))
	require.NoError(t, k.EscalatedReviews.Set(ctx, initID))

	// Squeeze the epoch budget so the stake teardown inside CloseInitiative
	// aborts the whole timeout.
	fullCap := params.MaxDreamMintPerEpoch
	params.MaxDreamMintPerEpoch = math.OneInt()
	require.NoError(t, k.Params.Set(ctx, params))

	future := ctx.WithBlockHeight(initiative.ReviewDeadline + 1)
	require.NoError(t, k.SweepReviewDeadlines(future),
		"one failed timeout must not abort the sweep")

	// Nothing committed: the escalation entry survives and the work is live.
	escalated, err := k.EscalatedReviews.Has(future, initID)
	require.NoError(t, err)
	require.True(t, escalated, "a failed timeout must stay in the escalation keyset to be retried")
	after, err := k.GetInitiative(future, initID)
	require.NoError(t, err)
	require.False(t, types.IsInitiativeTerminal(after.Status),
		"the initiative must not be half-retired by a discarded branch")
	_, err = k.GetStake(future, stakeID)
	require.NoError(t, err, "the stake must survive the discarded teardown")

	// The cap clears; the next block's sweep finishes the job.
	params.MaxDreamMintPerEpoch = fullCap
	require.NoError(t, k.Params.Set(future, params))
	require.NoError(t, k.SweepReviewDeadlines(future))

	after, err = k.GetInitiative(future, initID)
	require.NoError(t, err)
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_CLOSED, after.Status,
		"the retry must retire the initiative")
	escalated, err = k.EscalatedReviews.Has(future, initID)
	require.NoError(t, err)
	require.False(t, escalated, "a completed timeout must drop its escalation entry")
	_, err = k.GetStake(future, stakeID)
	require.Error(t, err, "the retry must release the stake")
}

// TestUnansweredContentChallengeSweepRetriesFailedUphold is the step-5b twin of
// TestDeadlineSweepRetriesFailedTally.
//
// UpholdContentChallenge burns the author's bond and deletes the bond stake
// before it mints the challenger's reward, and only flips Status to UPHELD
// last. Without a cache branch around the sweep, a mint failure persisted the
// burn with the challenge still live — and the next block's retry burned the
// bond a second time, because UnlockDREAM clamps to StakedDream rather than
// erroring, so nothing downstream noticed the bond was already gone.
func TestUnansweredContentChallengeSweepRetriesFailedUphold(t *testing.T) {
	f, authorAddr, challengerAddr := setupContentChallengeFixture(t)
	k := f.keeper
	ctx := f.ctx

	ccID, err := k.CreateContentChallenge(ctx, challengerAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND, 1,
		"Inaccurate claims", []string{"evidence"}, math.NewInt(100_000_000))
	require.NoError(t, err)

	// Let the author's response window lapse.
	cc, err := k.ContentChallenge.Get(ctx, ccID)
	require.NoError(t, err)
	require.NotZero(t, cc.ResponseDeadline, "precondition: the sweep keys on a deadline")

	authorBefore := mustMember(t, f, authorAddr)
	bondBefore := *authorBefore.DreamBalance
	stakedBefore := *authorBefore.StakedDream
	require.True(t, stakedBefore.IsPositive(), "precondition: the bond is locked")

	// Deterministic failure at the reward mint: the challenger's member record
	// is gone, so MintDREAM fails after the burn would have been written.
	challengerMember := mustMember(t, f, challengerAddr)
	require.NoError(t, k.Member.Remove(ctx, challengerAddr.String()))

	future := ctx.WithBlockHeight(cc.ResponseDeadline + 1)
	require.NoError(t, k.EndBlocker(future), "a failed uphold must not abort the block")

	// Nothing committed: the bond is intact and the challenge is still live.
	authorAfter := mustMember(t, f, authorAddr)
	require.Equal(t, bondBefore.String(), authorAfter.DreamBalance.String(),
		"a discarded uphold must not burn the author's bond")
	require.Equal(t, stakedBefore.String(), authorAfter.StakedDream.String(),
		"and must not release the bond stake")
	cc, err = k.ContentChallenge.Get(future, ccID)
	require.NoError(t, err)
	require.Equal(t, types.ContentChallengeStatus_CONTENT_CHALLENGE_STATUS_ACTIVE, cc.Status,
		"the challenge must stay live so the next block retries it")

	// Healed, the retry upholds exactly once.
	require.NoError(t, k.Member.Set(future, challengerAddr.String(), challengerMember))
	require.NoError(t, k.EndBlocker(future))

	cc, err = k.ContentChallenge.Get(future, ccID)
	require.NoError(t, err)
	require.Equal(t, types.ContentChallengeStatus_CONTENT_CHALLENGE_STATUS_UPHELD, cc.Status)

	final := mustMember(t, f, authorAddr)
	burned := bondBefore.Sub(*final.DreamBalance)
	require.Equal(t, cc.BondAmount.String(), burned.String(),
		"the bond must be burned exactly once, not once per retry")
}
