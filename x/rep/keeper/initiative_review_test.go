package keeper_test

import (
	"testing"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// Nothing on the happy path reads the deliverable: completion turns on
// conviction, which measures whether people wanted the work done rather than
// whether it was done. These tests pin the bonded reviewer role that closes
// that gap — who may judge, that judging pays regardless of the verdict, and
// that a project which asks for nobody keeps today's behaviour exactly.

type reviewFixture struct {
	f          *fixture
	projectID  uint64
	initiative uint64
	creator    sdk.AccAddress
	assignee   sdk.AccAddress
	reviewer   sdk.AccAddress
}

func mkReviewMember(t *testing.T, k keeper.Keeper, ctx sdk.Context, addr sdk.AccAddress, rep string) {
	t.Helper()
	require.NoError(t, k.Member.Set(ctx, addr.String(), types.Member{
		Address:          addr.String(),
		DreamBalance:     keeper.PtrInt(math.NewInt(100_000_000)),
		StakedDream:      keeper.PtrInt(math.ZeroInt()),
		LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
		LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"tag": rep},
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
	}))
}

// bondReviewer registers an address as a NORMAL-status initiative reviewer with
// enough bond to cover several verdicts.
func bondReviewer(t *testing.T, k keeper.Keeper, ctx sdk.Context, addr sdk.AccAddress, bond int64) {
	t.Helper()
	require.NoError(t, k.BondedRoles.Set(ctx,
		collections.Join(int32(types.RoleType_ROLE_TYPE_INITIATIVE_REVIEWER), addr.String()),
		types.BondedRole{
			Address:            addr.String(),
			RoleType:           types.RoleType_ROLE_TYPE_INITIATIVE_REVIEWER,
			BondStatus:         types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL,
			CurrentBond:        math.NewInt(bond).String(),
			TotalCommittedBond: math.ZeroInt().String(),
			RegisteredAt:       0,
		}))
}

func setupReview(t *testing.T, minVerifiers uint32) *reviewFixture {
	t.Helper()
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx

	creator := sdk.AccAddress([]byte("rv-creator-------"))
	assignee := sdk.AccAddress([]byte("rv-assignee------"))
	reviewer := sdk.AccAddress([]byte("rv-reviewer------"))
	for _, a := range []sdk.AccAddress{creator, assignee, reviewer} {
		mkReviewMember(t, k, ctx, a, "500.0")
	}
	bondReviewer(t, k, ctx, reviewer, 100_000_000)

	projectID, err := k.CreateProject(ctx, creator, "RP", "D", []string{"tag"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.NewInt(100_000), math.ZeroInt(), false)
	require.NoError(t, err)
	require.NoError(t, k.ApproveProject(ctx, projectID, creator, math.NewInt(100_000), math.ZeroInt()))

	if minVerifiers > 0 {
		project, gErr := k.GetProject(ctx, projectID)
		require.NoError(t, gErr)
		project.VerificationPolicy = &types.VerificationPolicy{MinVerifierCount: minVerifiers}
		require.NoError(t, k.UpdateProject(ctx, project))
	}

	initID, err := k.CreateInitiative(ctx, creator, projectID, "T", "D", []string{"tag"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(1000))
	require.NoError(t, err)
	require.NoError(t, k.AssignInitiativeToMember(ctx, initID, assignee))
	require.NoError(t, k.SubmitInitiativeWork(ctx, initID, assignee, "ipfs://work"))

	return &reviewFixture{f: f, projectID: projectID, initiative: initID,
		creator: creator, assignee: assignee, reviewer: reviewer}
}

func TestReviewGateIsOffByDefault(t *testing.T) {
	// min_verifier_count 0 is the genesis default and must be exactly today's
	// conviction-only behaviour — otherwise turning the role on would wedge
	// every existing project while the roster is still filling.
	rf := setupReview(t, 0)
	project, err := rf.f.keeper.GetProject(rf.f.ctx, rf.projectID)
	require.NoError(t, err)
	initiative, err := rf.f.keeper.GetInitiative(rf.f.ctx, rf.initiative)
	require.NoError(t, err)
	gp := gateParams(t, rf.f.keeper, rf.f.ctx)
	require.False(t, keeper.ReviewRequired(gp, initiative, project))

	ok, err := rf.f.keeper.ReviewGateSatisfied(rf.f.ctx, gp, initiative, project)
	require.NoError(t, err)
	require.True(t, ok, "a project asking for no reviewers is always satisfied")
	require.Zero(t, initiative.ReviewDeadline, "no review window opens when none is required")
}

// gateParams fetches params for the review-gate signatures, which now need
// them because the gate combines project policy with a chain-wide threshold.
func gateParams(t *testing.T, k keeper.Keeper, ctx sdk.Context) types.Params {
	t.Helper()
	p, err := k.Params.Get(ctx)
	require.NoError(t, err)
	return p
}

func TestReviewGateBlocksUntilApproved(t *testing.T) {
	rf := setupReview(t, 1)
	k, ctx := rf.f.keeper, rf.f.ctx

	project, err := k.GetProject(ctx, rf.projectID)
	require.NoError(t, err)
	initiative, err := k.GetInitiative(ctx, rf.initiative)
	require.NoError(t, err)
	require.NotZero(t, initiative.ReviewDeadline, "a review window opens on submission")

	ok, err := k.ReviewGateSatisfied(ctx, gateParams(t, k, ctx), initiative, project)
	require.NoError(t, err)
	require.False(t, ok, "unreviewed work must not satisfy the gate")

	require.NoError(t, k.SubmitInitiativeReview(ctx, rf.initiative, rf.reviewer.String(),
		true, nil, "looks done"))

	initiative, err = k.GetInitiative(ctx, rf.initiative)
	require.NoError(t, err)
	ok, err = k.ReviewGateSatisfied(ctx, gateParams(t, k, ctx), initiative, project)
	require.NoError(t, err)
	require.True(t, ok, "one approval meets a min_verifier_count of 1")
}

func TestReviewReservesBondScaledToBudget(t *testing.T) {
	rf := setupReview(t, 1)
	k, ctx := rf.f.keeper, rf.f.ctx

	// A wrong approval mints, so liability tracks the budget rather than being
	// flat: 10% of 1000 at the default reserve rate.
	require.NoError(t, k.SubmitInitiativeReview(ctx, rf.initiative, rf.reviewer.String(),
		true, nil, "ok"))

	role, err := k.GetBondedRole(ctx, types.RoleType_ROLE_TYPE_INITIATIVE_REVIEWER, rf.reviewer.String())
	require.NoError(t, err)
	require.Equal(t, "100", role.TotalCommittedBond,
		"the verdict commits reviewer_bond_reserve_rate of the initiative budget")
}

func TestAffiliatesAndStakersMayNotReview(t *testing.T) {
	rf := setupReview(t, 1)
	k, ctx := rf.f.keeper, rf.f.ctx

	// The assignee is an affiliate — judging your own commission is the exact
	// conflict this role exists to remove.
	bondReviewer(t, k, ctx, rf.assignee, 100_000_000)
	err := k.SubmitInitiativeReview(ctx, rf.initiative, rf.assignee.String(), true, nil, "mine is great")
	require.ErrorIs(t, err, types.ErrConflictOfInterest)

	// A staker holds conviction on the outcome; permitting it would rebuild
	// "paid to say yes" one layer down.
	staker := sdk.AccAddress([]byte("rv-staker--------"))
	mkReviewMember(t, k, ctx, staker, "500.0")
	bondReviewer(t, k, ctx, staker, 100_000_000)
	_, sErr := k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, rf.initiative, "", math.NewInt(100))
	require.NoError(t, sErr)
	err = k.SubmitInitiativeReview(ctx, rf.initiative, staker.String(), true, nil, "pass it")
	require.ErrorIs(t, err, types.ErrConflictOfInterest)
}

func TestUnbondedAddressMayNotReview(t *testing.T) {
	rf := setupReview(t, 1)
	stranger := sdk.AccAddress([]byte("rv-stranger------"))
	mkReviewMember(t, rf.f.keeper, rf.f.ctx, stranger, "500.0")
	err := rf.f.keeper.SubmitInitiativeReview(rf.f.ctx, rf.initiative, stranger.String(), true, nil, "hi")
	require.ErrorIs(t, err, types.ErrUnauthorized)
}

func TestRejectionReturnsWorkForAnotherRound(t *testing.T) {
	rf := setupReview(t, 1)
	k, ctx := rf.f.keeper, rf.f.ctx

	require.NoError(t, k.SubmitInitiativeReview(ctx, rf.initiative, rf.reviewer.String(),
		false, nil, "criterion 2 not met"))

	initiative, err := k.GetInitiative(ctx, rf.initiative)
	require.NoError(t, err)
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED, initiative.Status,
		"the natural remedy for 'not done' is to finish it")
	require.Equal(t, uint32(1), initiative.ReviewRound, "a resubmission opens a new round")
	require.Empty(t, initiative.DeliverableUri, "the rejected deliverable is cleared")

	// The reviewer may file again on the new round without colliding with the
	// verdict already on the previous one.
	require.NoError(t, k.SubmitInitiativeWork(ctx, rf.initiative, rf.assignee, "ipfs://work-v2"))
	require.NoError(t, k.SubmitInitiativeReview(ctx, rf.initiative, rf.reviewer.String(),
		true, nil, "fixed"))

	round0, err := k.GetInitiativeReviews(ctx, rf.initiative, 0)
	require.NoError(t, err)
	require.Len(t, round0, 1)
	require.False(t, round0[0].Approved, "the earlier round keeps its own verdict")
	round1, err := k.GetInitiativeReviews(ctx, rf.initiative, 1)
	require.NoError(t, err)
	require.Len(t, round1, 1)
	require.True(t, round1[0].Approved)
}

func TestReviewFeeIsPaidOnEitherVerdict(t *testing.T) {
	// The single constraint the reward design cannot trade away: if approving
	// paid and rejecting did not, the role would rebuild the bias it exists to
	// remove. Rejecting is the harder case to get right, so pin that one.
	rf := setupReview(t, 1)
	k, ctx := rf.f.keeper, rf.f.ctx

	before, err := k.Member.Get(ctx, rf.reviewer.String())
	require.NoError(t, err)

	require.NoError(t, k.SubmitInitiativeReview(ctx, rf.initiative, rf.reviewer.String(),
		false, nil, "not done"))

	initiative, err := k.GetInitiative(ctx, rf.initiative)
	require.NoError(t, err)
	// Round 0 held the verdict; pay it as that round's resolution.
	initiative.ReviewRound = 0
	_, feeErr := k.PayReviewFees(ctx, initiative)
	require.NoError(t, feeErr)

	after, err := k.Member.Get(ctx, rf.reviewer.String())
	require.NoError(t, err)
	require.True(t, after.DreamBalance.GT(*before.DreamBalance),
		"a reviewer is paid for rejecting, exactly as for approving")
}

func TestCommitteeEscalationTimeoutRejectsTheRound(t *testing.T) {
	// Silence must never mint, and must never wedge either.
	//
	// This used to resolve to PASSED, which meant a gated initiative could
	// complete with no verdict on it at all: wait out the reviewers, wait out
	// the committee, mint. The gate was therefore advisory for anyone patient
	// enough. It now rejects the round — the assignee resubmits and gets
	// another window, and when max_review_rounds runs out the initiative is
	// abandoned cleanly rather than held forever.
	rf := setupReview(t, 1)
	k := rf.f.keeper

	initiative, err := k.GetInitiative(rf.f.ctx, rf.initiative)
	require.NoError(t, err)
	startRound := initiative.ReviewRound

	// Past the review deadline, then past the committee's own window.
	ctx := rf.f.ctx.WithBlockHeight(initiative.ReviewDeadline + 1)
	require.NoError(t, k.SweepReviewDeadlines(ctx))

	escalated, err := k.GetInitiative(ctx, rf.initiative)
	require.NoError(t, err)
	require.Equal(t, types.ReviewEscalation_REVIEW_ESCALATION_NONE, escalated.ReviewEscalation,
		"escalation only flags the round; the committee still gets its window")

	ctx = ctx.WithBlockHeight(escalated.ReviewDeadline + 1)
	require.NoError(t, k.SweepReviewDeadlines(ctx))

	resolved, err := k.GetInitiative(ctx, rf.initiative)
	require.NoError(t, err)
	require.Equal(t, startRound+1, resolved.ReviewRound,
		"the unreviewed round is rejected and a new one opens")
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED, resolved.Status,
		"the work goes back to the assignee rather than through the gate")
	require.Empty(t, resolved.DeliverableUri,
		"a new round needs a fresh submission — which is also a fresh call for reviewer attention")

	project, err := k.GetProject(ctx, rf.projectID)
	require.NoError(t, err)
	ok, err := k.ReviewGateSatisfied(ctx, gateParams(t, k, ctx), resolved, project)
	require.NoError(t, err)
	require.False(t, ok, "waiting the clock out must not satisfy the gate")

	// The escalation marker is cleared, or the next round would be skipped as
	// "already with the committee" and silently lose its escalation path.
	stillEscalated, hErr := k.EscalatedReviews.Has(ctx, rf.initiative)
	require.NoError(t, hErr)
	require.False(t, stillEscalated)
}

func TestRepeatedEscalationTimeoutsExhaustRoundsAndClose(t *testing.T) {
	// The terminal state is abandonment, not an indefinite hold: budget
	// returned, bond released, nothing minted. A farmer cannot wait out the
	// gate, and an assignee cannot be held forever by reviewer absence.
	rf := setupReview(t, 1)
	k := rf.f.keeper
	params, err := k.Params.Get(rf.f.ctx)
	require.NoError(t, err)

	ctx := rf.f.ctx
	for round := uint32(0); round < params.MaxReviewRounds; round++ {
		cur, gErr := k.GetInitiative(ctx, rf.initiative)
		require.NoError(t, gErr)
		if cur.Status == types.InitiativeStatus_INITIATIVE_STATUS_CLOSED {
			break
		}
		// Re-submit so the round has a deliverable and a deadline to expire.
		if cur.Status == types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED {
			assignee, aErr := sdk.AccAddressFromBech32(cur.Assignee)
			require.NoError(t, aErr)
			require.NoError(t, k.SubmitInitiativeWork(ctx, rf.initiative, assignee, "deliverable"))
			cur, gErr = k.GetInitiative(ctx, rf.initiative)
			require.NoError(t, gErr)
		}
		ctx = ctx.WithBlockHeight(cur.ReviewDeadline + 1)
		require.NoError(t, k.SweepReviewDeadlines(ctx))
		mid, gErr := k.GetInitiative(ctx, rf.initiative)
		require.NoError(t, gErr)
		ctx = ctx.WithBlockHeight(mid.ReviewDeadline + 1)
		require.NoError(t, k.SweepReviewDeadlines(ctx))
	}

	final, err := k.GetInitiative(ctx, rf.initiative)
	require.NoError(t, err)
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_CLOSED, final.Status,
		"rounds exhausted with no verdict must close, never pass")
}

// The four defects below were all found by auditing the reviewer role after it
// was written, and every one of them is a state-leak or an incentive inversion
// rather than a crash — the class that does not show up in a happy-path test.

func TestVindicatedReviewerGetsTheirBondBack(t *testing.T) {
	// The worst of the four: a reviewer who correctly rejected work that a jury
	// then agreed was bad had their commitment marked settled *without* being
	// released, stranding it permanently. Getting it right cost more than
	// getting it wrong.
	rf := setupReview(t, 1)
	k, ctx := rf.f.keeper, rf.f.ctx

	require.NoError(t, k.SubmitInitiativeReview(ctx, rf.initiative, rf.reviewer.String(),
		false, nil, "not done"))

	before, err := k.GetBondedRole(ctx, types.RoleType_ROLE_TYPE_INITIATIVE_REVIEWER, rf.reviewer.String())
	require.NoError(t, err)
	require.NotEqual(t, "0", before.TotalCommittedBond, "the verdict committed bond")

	// A jury upholds a challenge: the approvers were wrong, the rejecters right.
	require.NoError(t, k.SlashReviewersOnOverturn(ctx, rf.initiative, 0, true))

	after, err := k.GetBondedRole(ctx, types.RoleType_ROLE_TYPE_INITIATIVE_REVIEWER, rf.reviewer.String())
	require.NoError(t, err)
	require.Equal(t, "0", after.TotalCommittedBond,
		"a vindicated reviewer's bond must be released, not stranded")
	require.Equal(t, before.CurrentBond, after.CurrentBond,
		"and it must not be slashed either")
}

func TestRejectionClearsTheEscalationFlag(t *testing.T) {
	// A round that escalated and was then rejected left its id in
	// EscalatedReviews, so the sweep skipped every later round as "already with
	// the committee" — the new round silently lost its escalation path, which is
	// the exact wedge the liveness design exists to prevent.
	rf := setupReview(t, 1)
	k := rf.f.keeper

	initiative, err := k.GetInitiative(rf.f.ctx, rf.initiative)
	require.NoError(t, err)
	ctx := rf.f.ctx.WithBlockHeight(initiative.ReviewDeadline + 1)
	require.NoError(t, k.SweepReviewDeadlines(ctx))

	flagged, err := k.EscalatedReviews.Has(ctx, rf.initiative)
	require.NoError(t, err)
	require.True(t, flagged, "the round is with the committee")

	// The committee rejects, which opens a fresh round.
	require.NoError(t, k.ResolveReviewEscalation(ctx, rf.initiative,
		types.ReviewEscalation_REVIEW_ESCALATION_REJECTED, "committee", "not good enough"))

	stillFlagged, err := k.EscalatedReviews.Has(ctx, rf.initiative)
	require.NoError(t, err)
	require.False(t, stillFlagged,
		"the new round must be able to escalate on its own account")
}

func TestReviewerMayNotStakeOnWorkTheyJudged(t *testing.T) {
	// QualifiedReviewer blocks stake-then-review. Without the mirror check the
	// same conflict is simply acquired in the other order: approve the work,
	// then stake and collect the completion bonus.
	rf := setupReview(t, 1)
	k, ctx := rf.f.keeper, rf.f.ctx

	require.NoError(t, k.SubmitInitiativeReview(ctx, rf.initiative, rf.reviewer.String(),
		true, nil, "looks done"))

	_, err := k.CreateStake(ctx, rf.reviewer, types.StakeTargetType_STAKE_TARGET_INITIATIVE,
		rf.initiative, "", math.NewInt(100))
	require.ErrorIs(t, err, types.ErrConflictOfInterest,
		"holding conviction on work you passed is the conflict the role removes")
}

func TestClosureChargesTheProjectForReview(t *testing.T) {
	// The project pays for having had the work evaluated. Returning the full
	// budget would make review free to the party that asked for it and fund the
	// reviewers purely by dilution.
	rf := setupReview(t, 1)
	k, ctx := rf.f.keeper, rf.f.ctx

	require.NoError(t, k.SubmitInitiativeReview(ctx, rf.initiative, rf.reviewer.String(),
		true, nil, "fine"))

	projectBefore, err := k.GetProject(ctx, rf.projectID)
	require.NoError(t, err)
	allocatedBefore := keeper.DerefInt(projectBefore.AllocatedBudget)

	require.NoError(t, k.CloseInitiative(ctx, rf.initiative, "changed my mind"))

	projectAfter, err := k.GetProject(ctx, rf.projectID)
	require.NoError(t, err)
	allocatedAfter := keeper.DerefInt(projectAfter.AllocatedBudget)

	returned := allocatedBefore.Sub(allocatedAfter)
	require.True(t, returned.LT(math.NewInt(1000)),
		"the returned budget is net of the review fee, got %s of 1000", returned)
	require.True(t, returned.IsPositive(), "but most of it still comes back")
}

func TestPolicyCannotBeRelaxedOverWorkInReview(t *testing.T) {
	// The project creator owns the verification policy — and for self-assigned
	// work is also the party the gate constrains. Reading the policy live let
	// them zero min_verifier_count over work already submitted and walk the work
	// straight through.
	rf := setupReview(t, 1)
	k, ctx := rf.f.keeper, rf.f.ctx

	initiative, err := k.GetInitiative(ctx, rf.initiative)
	require.NoError(t, err)
	require.Equal(t, uint32(1), initiative.RequiredVerifiers,
		"the requirement is snapshotted when the window opens")

	// Creator relaxes the standard after submission.
	project, err := k.GetProject(ctx, rf.projectID)
	require.NoError(t, err)
	project.VerificationPolicy.MinVerifierCount = 0
	require.NoError(t, k.UpdateProject(ctx, project))

	project, err = k.GetProject(ctx, rf.projectID)
	require.NoError(t, err)
	ok, err := k.ReviewGateSatisfied(ctx, gateParams(t, k, ctx), initiative, project)
	require.NoError(t, err)
	require.False(t, ok,
		"the round keeps the standard it opened under; relaxing the policy does not apply retroactively")

	// It does apply to the next initiative, which is the legitimate use.
	nextID, err := k.CreateInitiative(ctx, rf.creator, rf.projectID, "T2", "D", []string{"tag"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(1000))
	require.NoError(t, err)
	require.NoError(t, k.AssignInitiativeToMember(ctx, nextID, rf.assignee))
	require.NoError(t, k.SubmitInitiativeWork(ctx, nextID, rf.assignee, "ipfs://w2"))
	next, err := k.GetInitiative(ctx, nextID)
	require.NoError(t, err)
	require.Zero(t, next.RequiredVerifiers, "new work opens under the current policy")
}

// Committee disapproval routes through CloseInitiative rather than writing the
// status inline. Written inline it left the bond behind every filed verdict
// reserved with nothing left to release it: the initiative was terminal, so no
// later path would ever call SettleReviewBonds for it, and the reviewer's bond
// was stranded for good.
func TestDisapprovalReleasesCommittedReviewBonds(t *testing.T) {
	rf := setupReview(t, 1) // AlwaysAuthorized: the caller reads as committee
	k, ctx := rf.f.keeper, rf.f.ctx
	ms := keeper.NewMsgServerImpl(k)

	require.NoError(t, k.SubmitInitiativeReview(ctx, rf.initiative, rf.reviewer.String(),
		true, nil, "looks done"))

	committed, err := k.GetBondedRole(ctx, types.RoleType_ROLE_TYPE_INITIATIVE_REVIEWER, rf.reviewer.String())
	require.NoError(t, err)
	require.NotEqual(t, "0", committed.TotalCommittedBond,
		"filing a verdict commits bond, or this test proves nothing")

	// A disapproving voice that is neither the assignee nor the project creator
	// (both are excluded by the conflict-of-interest gate).
	objector := sdk.AccAddress([]byte("rv-objector------"))
	mkReviewMember(t, k, ctx, objector, "500.0")

	_, err = ms.ApproveInitiative(ctx, &types.MsgApproveInitiative{
		Creator:      objector.String(),
		InitiativeId: rf.initiative,
		Approved:     false,
		Comments:     "not what was asked for",
	})
	require.NoError(t, err)

	initiative, err := k.GetInitiative(ctx, rf.initiative)
	require.NoError(t, err)
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_CLOSED, initiative.Status)

	after, err := k.GetBondedRole(ctx, types.RoleType_ROLE_TYPE_INITIATIVE_REVIEWER, rf.reviewer.String())
	require.NoError(t, err)
	require.Equal(t, "0", after.TotalCommittedBond,
		"the terminal exit must release every bond it strands")
}
