package keeper_test

import (
	"context"
	"strings"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

// A testimony long enough to clear MinExpertOpinionLength.
var sampleOpinion = strings.Repeat("the deliverable meets criterion one. ", 3)

func (e *expertWitnessFixture) testify(expert sdk.AccAddress, opinion, reasoning string) error {
	_, err := e.ms.SubmitExpertTestimony(e.ctx, &types.MsgSubmitExpertTestimony{
		Expert: expert.String(), JuryReviewId: e.reviewID, Opinion: opinion, Reasoning: reasoning,
	})
	return err
}

func (e *expertWitnessFixture) balance(t *testing.T, a sdk.AccAddress) math.Int {
	t.Helper()
	m, err := e.keeper.Member.Get(e.ctx, a.String())
	require.NoError(t, err)
	return *m.DreamBalance
}

func (e *expertWitnessFixture) interimStatus(t *testing.T, id uint64) types.InterimStatus {
	t.Helper()
	in, err := e.keeper.GetInterim(e.ctx, id)
	require.NoError(t, err)
	return in.Status
}

// REP-E-101: the expert is paid from the interim their invitation opened, by
// id, and only when the review reaches its verdict. A second matching
// EXPERT_TESTIMONY interim (here an EPIC one, which a member could create
// before EXPERT_TESTIMONY became system-only) is never the one paid, and
// members can no longer create the type at all.
func TestREPE101_ExpertPaidFromItsOwnInterimAtVerdict(t *testing.T) {
	e := newExpertWitnessFixture(t)
	k, ctx := e.keeper, e.ctx

	_, err := e.ms.CreateInterim(ctx, &types.MsgCreateInterim{
		Creator: e.expert.String(), InterimType: types.InterimType_INTERIM_TYPE_EXPERT_TESTIMONY,
		ReferenceId: e.reviewID, ReferenceType: "jury_review",
		Complexity: types.InterimComplexity_INTERIM_COMPLEXITY_EPIC,
		Deadline:   ctx.BlockHeight() + 50,
	})
	require.ErrorIs(t, err, types.ErrInterimTypeSystemOnly)

	// A decoy with a lower id than the invitation's, matching every field the
	// old walk compared.
	decoy, err := k.CreateInterimWork(ctx, types.InterimType_INTERIM_TYPE_EXPERT_TESTIMONY,
		[]string{e.expert.String()}, "", e.reviewID, "jury_review",
		types.InterimComplexity_INTERIM_COMPLEXITY_EPIC, ctx.BlockHeight()+50)
	require.NoError(t, err)

	resp, err := e.invite(e.juror, e.expert)
	require.NoError(t, err)
	require.NoError(t, e.endorse(e.juror2, e.expert))
	review, err := k.JuryReview.Get(ctx, e.reviewID)
	require.NoError(t, err)
	require.Equal(t, []uint64{resp.InterimId}, review.ExpertInterimIds)

	before := e.balance(t, e.expert)
	require.NoError(t, e.testify(e.expert, sampleOpinion, "criteria"))
	require.True(t, e.balance(t, e.expert).Equal(before), "testifying pays nothing")
	require.Equal(t, types.InterimStatus_INTERIM_STATUS_PENDING, e.interimStatus(t, resp.InterimId))

	require.NoError(t, k.TallyJuryVotes(ctx, e.reviewID))

	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, params.ComplexComplexityBudget.String(), e.balance(t, e.expert).Sub(before).String(),
		"paid exactly the invitation's COMPLEX budget")
	require.Equal(t, types.InterimStatus_INTERIM_STATUS_COMPLETED, e.interimStatus(t, resp.InterimId))
	require.Equal(t, types.InterimStatus_INTERIM_STATUS_PENDING, e.interimStatus(t, decoy), "the decoy is never paid")

	// Approval cannot pay an expert interim either.
	err = k.ApproveInterim(ctx, decoy, k.GetAuthority(), true, "")
	require.ErrorIs(t, err, types.ErrInvalidRequest)
}

// REP-E-103 / REP-E-110: an expert who never testified has the interim
// expired at the verdict, and a voided review pays nobody. While the review
// is pending, the EndBlocker's deadline expiry leaves the interim to it.
func TestREPE103_ExpertSettlementWithoutTestimonyOrOnVoid(t *testing.T) {
	t.Run("no testimony: expired at the verdict", func(t *testing.T) {
		e := newExpertWitnessFixture(t)
		resp, err := e.invite(e.juror, e.expert)
		require.NoError(t, err)
		require.NoError(t, e.endorse(e.juror2, e.expert))
		require.NoError(t, e.keeper.TallyJuryVotes(e.ctx, e.reviewID))
		require.Equal(t, types.InterimStatus_INTERIM_STATUS_EXPIRED, e.interimStatus(t, resp.InterimId))
	})
	t.Run("voided review: expired unpaid", func(t *testing.T) {
		e := newExpertWitnessFixture(t)
		resp, err := e.invite(e.juror, e.expert)
		require.NoError(t, err)
		require.NoError(t, e.endorse(e.juror2, e.expert))
		require.NoError(t, e.testify(e.expert, sampleOpinion, ""))
		before := e.balance(t, e.expert)
		review, err := e.keeper.JuryReview.Get(e.ctx, e.reviewID)
		require.NoError(t, err)
		require.NoError(t, e.keeper.AddJuryReviewToVerdictIndex(e.ctx, review))
		require.NoError(t, e.keeper.VoidPendingJuryReviewForChallengeForTest(e.ctx, 7, "voided"))
		require.Equal(t, types.InterimStatus_INTERIM_STATUS_EXPIRED, e.interimStatus(t, resp.InterimId))
		require.True(t, e.balance(t, e.expert).Equal(before))
	})
	t.Run("deadline expiry waits for the verdict", func(t *testing.T) {
		e := newExpertWitnessFixture(t)
		resp, err := e.invite(e.juror, e.expert)
		require.NoError(t, err)
		require.NoError(t, e.endorse(e.juror2, e.expert))
		require.NoError(t, e.testify(e.expert, sampleOpinion, ""))
		review, err := e.keeper.JuryReview.Get(e.ctx, e.reviewID)
		require.NoError(t, err)
		// Past the deadline with the review still pending (its tally deferred
		// to a later block): the interim must survive to be paid.
		ctx := e.ctx.WithBlockHeight(review.Deadline)
		require.NoError(t, e.keeper.EndBlocker(ctx))
		r, err := e.keeper.JuryReview.Get(ctx, e.reviewID)
		require.NoError(t, err)
		require.Equal(t, types.Verdict_VERDICT_PENDING, r.Verdict, "fixture review is outside the deadline sweep")
		require.Equal(t, types.InterimStatus_INTERIM_STATUS_PENDING, e.interimStatus(t, resp.InterimId))
	})
}

// REP-E-103: the inviting juror cannot call their own invitee, nor anyone a
// party invited or was invited by.
func TestREPE103_ExpertInvitationNeighboursExcluded(t *testing.T) {
	e := newExpertWitnessFixture(t)
	k, ctx := e.keeper, e.ctx
	setInviter := func(a, inviter sdk.AccAddress) {
		m, err := k.Member.Get(ctx, a.String())
		require.NoError(t, err)
		m.InvitedBy = inviter.String()
		require.NoError(t, k.Member.Set(ctx, a.String(), m))
	}
	setInviter(e.expert, e.juror)       // the juror's invitee
	setInviter(e.challenger, e.expert2) // the challenger's inviter
	_, err := e.invite(e.juror, e.expert)
	require.ErrorIs(t, err, types.ErrExpertIneligible)
	_, err = e.invite(e.juror, e.expert2)
	require.ErrorIs(t, err, types.ErrExpertIneligible)
	_, err = e.invite(e.juror, e.expert3)
	require.NoError(t, err, "an unrelated expert is still callable")
}

// REP-R-105: testimony needs a non-trivial opinion and both fields are
// bounded.
func TestREPR105_TestimonyBounds(t *testing.T) {
	e := newExpertWitnessFixture(t)
	_, err := e.invite(e.juror, e.expert)
	require.NoError(t, err)
	require.NoError(t, e.endorse(e.juror2, e.expert))
	require.ErrorIs(t, e.testify(e.expert, "", ""), types.ErrInvalidTestimony)
	require.ErrorIs(t, e.testify(e.expert, "   ok   ", ""), types.ErrInvalidTestimony)
	require.ErrorIs(t, e.testify(e.expert, strings.Repeat("a", types.MaxExpertOpinionLength+1), ""), types.ErrInvalidTestimony)
	require.ErrorIs(t, e.testify(e.expert, sampleOpinion, strings.Repeat("a", types.MaxExpertReasoningLength+1)), types.ErrInvalidTestimony)
	require.NoError(t, e.testify(e.expert, sampleOpinion, "short reasoning is fine"))
}

// REP-E-104: zeroing expires the member's open interims, an expert who was
// zeroed cannot testify, and a payout skips an assignee who is no longer
// active.
func TestREPE104_ZeroingClosesInterims(t *testing.T) {
	e := newExpertWitnessFixture(t)
	k, ctx := e.keeper, e.ctx
	resp, err := e.invite(e.juror, e.expert)
	require.NoError(t, err)
	require.NoError(t, e.endorse(e.juror2, e.expert))
	require.NoError(t, k.ZeroMember(ctx, e.expert, "misconduct"))
	require.Equal(t, types.InterimStatus_INTERIM_STATUS_EXPIRED, e.interimStatus(t, resp.InterimId))
	require.ErrorIs(t, e.testify(e.expert, sampleOpinion, ""), types.ErrExpertIneligible)

	// Backstop: an interim still open on an inactive assignee pays nothing.
	id, err := k.CreateInterimWork(ctx, types.InterimType_INTERIM_TYPE_AUDIT,
		[]string{e.expert2.String()}, "", 0, "", types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE, ctx.BlockHeight()+50)
	require.NoError(t, err)
	m, err := k.Member.Get(ctx, e.expert2.String())
	require.NoError(t, err)
	m.Status = types.MemberStatus_MEMBER_STATUS_ZEROED
	require.NoError(t, k.Member.Set(ctx, e.expert2.String(), m))
	before := e.balance(t, e.expert2)
	require.NoError(t, k.ApproveInterim(ctx, id, k.GetAuthority(), true, "done"))
	require.True(t, e.balance(t, e.expert2).Equal(before), "zeroed assignee not paid")
}

// REP-E-109: zeroed members neither send nor receive tips.
func TestREPE109_ZeroedMembersCannotTip(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx
	a := sdk.AccAddress([]byte("repe109-sender------"))
	b := sdk.AccAddress([]byte("repe109-recipient---"))
	econMember(t, f, a, 100_000_000, 0)
	econMember(t, f, b, 100_000_000, 0)
	setStatus := func(addr sdk.AccAddress, s types.MemberStatus) {
		m := econGet(t, f, addr)
		m.Status = s
		require.NoError(t, k.Member.Set(ctx, addr.String(), m))
	}
	amt := math.NewInt(1_000_000)

	setStatus(b, types.MemberStatus_MEMBER_STATUS_ZEROED)
	require.ErrorIs(t, k.TransferDREAM(ctx, a, b, amt, types.TransferPurpose_TRANSFER_PURPOSE_TIP), types.ErrRecipientNotActive)
	setStatus(b, types.MemberStatus_MEMBER_STATUS_ACTIVE)
	setStatus(a, types.MemberStatus_MEMBER_STATUS_ZEROED)
	require.ErrorIs(t, k.TransferDREAM(ctx, a, b, amt, types.TransferPurpose_TRANSFER_PURPOSE_TIP), types.ErrMemberNotActive)
	setStatus(a, types.MemberStatus_MEMBER_STATUS_ACTIVE)
	require.NoError(t, k.TransferDREAM(ctx, a, b, amt, types.TransferPurpose_TRANSFER_PURPOSE_TIP))
}

// REP-E-105: a single Operations Committee member cannot approve a project
// they created; the committee policy still can.
func TestREPE105_NoSelfApprovalOfProject(t *testing.T) {
	f := initFixture(t)
	f.commonsKeeper.IsCommitteeMemberFn = func(context.Context, sdk.AccAddress, string, string) (bool, error) { return true, nil }
	f.commonsKeeper.IsCouncilOrCommitteePolicyFn = func(context.Context, string, string, string) bool { return false }
	k, ctx := f.keeper, f.ctx
	creator := sdk.AccAddress([]byte("repe105-creator-----"))
	other := sdk.AccAddress([]byte("repe105-other-------"))
	econMember(t, f, creator, 0, 0)
	econMember(t, f, other, 0, 0)

	projectID, err := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(1000), math.NewInt(100), false)
	require.NoError(t, err)
	require.ErrorIs(t, k.ApproveProject(ctx, projectID, creator, math.NewInt(1000), math.NewInt(100)), types.ErrUnauthorized)
	require.NoError(t, k.ApproveProject(ctx, projectID, other, math.NewInt(1000), math.NewInt(100)))
}

// REP-E-106: a migrated assignee is paid the initiative bounty at the
// address that now carries their record.
func TestREPE106_MigratedAssigneeGetsBounty(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx
	funder := sdk.AccAddress([]byte("repe106-funder------"))
	oldAddr := sdk.AccAddress([]byte("repe106-old---------"))
	newAddr := sdk.AccAddress([]byte("repe106-new---------"))
	econMember(t, f, funder, 1_000_000_000, 0)
	econMember(t, f, newAddr, 0, 0)
	require.NoError(t, k.Tombstones.Set(ctx, oldAddr.String(), types.Tombstone{Canonical: newAddr.String()}))

	amount := math.NewInt(5_000_000)
	require.NoError(t, k.LockDREAM(ctx, funder, amount))
	require.NoError(t, k.InitiativeBounty.Set(ctx, 42, types.InitiativeBounty{
		InitiativeId: 42, Amount: amount,
		Contributions: []types.InitiativeBountyContribution{{Funder: funder.String(), Amount: amount}},
	}))
	paid, err := k.PayInitiativeBounty(ctx, types.Initiative{Id: 42, Assignee: oldAddr.String()})
	require.NoError(t, err)
	require.True(t, paid.IsPositive(), "the bounty is paid, not refunded")
	require.True(t, econGet(t, f, newAddr).DreamBalance.IsPositive(), "credited to the canonical record")
	_, err = k.InitiativeBounty.Get(ctx, 42)
	require.Error(t, err, "bounty settled")
}

// REP-E-107: restoring a slashed author bond moves DREAM back; it is not
// subject to the per-epoch mint ceiling (nor the referral cascade).
func TestREPE107_RestoreAuthorBondSkipsMintCeiling(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx
	author := sdk.AccAddress([]byte("repe107-author------"))
	econMember(t, f, author, 0, 0)
	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	params.MaxDreamMintPerEpoch = math.NewInt(1)
	require.NoError(t, k.Params.Set(ctx, params))

	amount := math.NewInt(50_000_000)
	require.NoError(t, k.RestoreAuthorBond(ctx, author, types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND, 3, amount))
	m := econGet(t, f, author)
	require.Equal(t, amount.String(), m.DreamBalance.String())
	require.Equal(t, amount.String(), m.StakedDream.String())
}

// REP-E-108: a decay pass interrupted by an export resumes where it stopped
// after import.
func TestREPE108_DecayPassSurvivesGenesis(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx
	require.NoError(t, k.DecayPassEpoch.Set(ctx, 4))
	require.NoError(t, k.DecayPassMemberCursor.Set(ctx, "phoenix"))
	require.NoError(t, k.DecayPassStakeCursor.Set(ctx, 17))

	out, err := k.ExportGenesis(ctx)
	require.NoError(t, err)
	require.NoError(t, out.Validate())

	g := initFixture(t)
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *out))
	epoch, err := g.keeper.DecayPassEpoch.Get(g.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(4), epoch)
	cursor, err := g.keeper.DecayPassMemberCursor.Get(g.ctx)
	require.NoError(t, err)
	require.Equal(t, "phoenix", cursor)
	stake, err := g.keeper.DecayPassStakeCursor.Get(g.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(17), stake)

	// Cursors with no pass in flight are malformed.
	bad := *out
	econ := *out.EconomicState
	econ.DecayPassEpoch = 0
	bad.EconomicState = &econ
	require.Error(t, bad.Validate())
}
