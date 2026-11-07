package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// Regression tests for the x/rep economics findings REP-E-201 .. REP-E-208.

// REP-E-201: a budget-backed proposal needs an ACTIVE creator, burns the
// creation fee and is capped per creator across aliases.
func TestREPE201_BudgetBackedProposalGates(t *testing.T) {
	t.Run("zeroed creator is refused", func(t *testing.T) {
		rf := newProjectReviewFixture(t, 1)
		m := econGet(t, rf.fixture, rf.creator)
		m.Status = types.MemberStatus_MEMBER_STATUS_ZEROED
		require.NoError(t, rf.keeper.Member.Set(rf.ctx, rf.creator.String(), m))
		_, err := rf.ms.ProposeProject(rf.ctx, &types.MsgProposeProject{
			Creator: rf.creator.String(), Name: "p", Description: "d", Council: "commons",
			RequestedBudget: keeper.PtrInt(math.NewInt(1)), RequestedSpark: keeper.PtrInt(math.ZeroInt()),
		})
		require.ErrorIs(t, err, types.ErrMemberNotActive)
	})

	t.Run("creation fee is burned on the budget-backed path", func(t *testing.T) {
		rf := newProjectReviewFixture(t, 1)
		params, err := rf.keeper.Params.Get(rf.ctx)
		require.NoError(t, err)
		before := econGet(t, rf.fixture, rf.creator).DreamBalance
		rf.propose(t, 1_000_000, 0)
		after := econGet(t, rf.fixture, rf.creator).DreamBalance
		require.Equal(t, before.Sub(params.ProjectCreationFee).String(), after.String())

		// Without the fee the proposal is refused before any state is written.
		m := econGet(t, rf.fixture, rf.creator)
		m.DreamBalance = keeper.PtrInt(math.ZeroInt())
		require.NoError(t, rf.keeper.Member.Set(rf.ctx, rf.creator.String(), m))
		_, err = rf.ms.ProposeProject(rf.ctx, &types.MsgProposeProject{
			Creator: rf.creator.String(), Name: "p", Description: "d", Council: "commons",
			RequestedBudget: keeper.PtrInt(math.NewInt(1)), RequestedSpark: keeper.PtrInt(math.ZeroInt()),
		})
		require.ErrorIs(t, err, types.ErrInsufficientCreationFee)
	})

	t.Run("PROPOSED projects per creator are capped across aliases", func(t *testing.T) {
		rf := newProjectReviewFixture(t, 1)
		params, err := rf.keeper.Params.Get(rf.ctx)
		require.NoError(t, err)
		require.Equal(t, uint32(3), params.MaxProposedProjectsPerCreator)
		params.MaxProposedProjectsPerCreator = 2
		require.NoError(t, rf.keeper.Params.Set(rf.ctx, params))

		propose := func(creator string) error {
			_, err := rf.ms.ProposeProject(rf.ctx, &types.MsgProposeProject{
				Creator: creator, Name: "p", Description: "d", Council: "commons",
				RequestedBudget: keeper.PtrInt(math.NewInt(1)), RequestedSpark: keeper.PtrInt(math.ZeroInt()),
			})
			return err
		}
		// One proposal sits under a former address of the same member.
		oldAddr := sdk.AccAddress([]byte("review-creator-old"))
		require.NoError(t, rf.keeper.Tombstones.Set(rf.ctx, oldAddr.String(), types.Tombstone{Canonical: rf.creator.String()}))
		require.NoError(t, rf.keeper.MigratedFrom.Set(rf.ctx, collections.Join3(rf.creator.String(), int64(0), oldAddr.String())))
		_, err = rf.keeper.CreateProject(rf.ctx, oldAddr, "old", "d", nil,
			types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "commons", math.NewInt(1), math.ZeroInt(), false)
		require.NoError(t, err)
		count, err := rf.keeper.CountProposedProjectsForCreator(rf.ctx, rf.creator.String())
		require.NoError(t, err)
		require.Equal(t, uint32(1), count)

		require.NoError(t, propose(rf.creator.String()))
		require.ErrorIs(t, propose(rf.creator.String()), types.ErrTooManyProposedProjects)

		// A decided proposal frees its slot.
		require.NoError(t, rf.keeper.CancelProject(rf.ctx, 1, "withdrawn"))
		require.NoError(t, propose(rf.creator.String()))
		require.ErrorIs(t, propose(rf.creator.String()), types.ErrTooManyProposedProjects)
	})
}

// REP-E-201: system interims (a committee review seat) do not count against
// the member's max_active_interims_per_member.
func TestREPE201_SystemInterimsDoNotCountAgainstCap(t *testing.T) {
	rf := newProjectReviewFixture(t, 1)
	params, err := rf.keeper.Params.Get(rf.ctx)
	require.NoError(t, err)
	params.MaxActiveInterimsPerMember = 1
	require.NoError(t, rf.keeper.Params.Set(rf.ctx, params))

	// Two review seats for the committee member (two proposals).
	rf.propose(t, 1_000_000, 0)
	_, err = rf.ms.ProposeProject(rf.ctx, &types.MsgProposeProject{
		Creator: rf.creator.String(), Name: "second", Description: "d", Council: "commons",
		RequestedBudget: keeper.PtrInt(math.NewInt(1_000_000)), RequestedSpark: keeper.PtrInt(math.ZeroInt()),
	})
	require.NoError(t, err)
	ids, err := rf.keeper.LiveInterimIDsForAddresses(rf.ctx, []string{rf.committee[0].String()})
	require.NoError(t, err)
	require.Len(t, ids, 2, "both seats are indexed")

	count, err := rf.keeper.CountActiveInterimsForMember(rf.ctx, rf.committee[0].String())
	require.NoError(t, err)
	require.Zero(t, count, "review seats do not count")

	// The member's own work still counts, and the cap still binds it.
	_, err = rf.ms.CreateInterim(rf.ctx, &types.MsgCreateInterim{
		Creator: rf.committee[0].String(), InterimType: types.InterimType_INTERIM_TYPE_AUDIT,
		Complexity: types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE,
	})
	require.NoError(t, err)
	count, err = rf.keeper.CountActiveInterimsForMember(rf.ctx, rf.committee[0].String())
	require.NoError(t, err)
	require.Equal(t, uint32(1), count)
	_, err = rf.ms.CreateInterim(rf.ctx, &types.MsgCreateInterim{
		Creator: rf.committee[0].String(), InterimType: types.InterimType_INTERIM_TYPE_AUDIT,
		Complexity: types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE,
	})
	require.ErrorIs(t, err, types.ErrTooManyActiveInterims)
}

// REP-E-201: interim deadlines and proposal expiries are swept from due
// queues, not by a walk over every live record.
func TestREPE201_DueQueuesExpireInterimsAndProjects(t *testing.T) {
	rf := newProjectReviewFixture(t, 1)
	k := rf.keeper
	sdkCtx := sdk.UnwrapSDKContext(rf.ctx)
	project := rf.propose(t, 1_000_000, 0)
	id, err := k.CreateInterimWork(rf.ctx, types.InterimType_INTERIM_TYPE_AUDIT,
		[]string{rf.committee[0].String()}, "", 0, "", types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE, sdkCtx.BlockHeight()+10)
	require.NoError(t, err)

	// Not due yet: nothing changes.
	require.NoError(t, k.EndBlocker(sdkCtx.WithBlockHeight(sdkCtx.BlockHeight()+5)))
	require.Equal(t, types.InterimStatus_INTERIM_STATUS_PENDING, rf.interim(t, id).Status)
	require.Equal(t, types.InterimStatus_INTERIM_STATUS_PENDING, rf.interim(t, project.ApprovalInterimId).Status)

	// The member interim's deadline passes.
	require.NoError(t, k.EndBlocker(sdkCtx.WithBlockHeight(sdkCtx.BlockHeight()+10)))
	require.Equal(t, types.InterimStatus_INTERIM_STATUS_EXPIRED, rf.interim(t, id).Status)
	require.Equal(t, types.InterimStatus_INTERIM_STATUS_PENDING, rf.interim(t, project.ApprovalInterimId).Status)
	ids, err := k.LiveInterimIDsForAddresses(rf.ctx, []string{rf.committee[0].String()})
	require.NoError(t, err)
	require.Equal(t, []uint64{project.ApprovalInterimId}, ids, "an expired interim leaves the assignee index")

	// The proposal's expiry passes: project and review end together.
	require.NoError(t, k.EndBlocker(sdkCtx.WithBlockHeight(project.ExpiryBlockHeight)))
	expired, err := k.GetProject(rf.ctx, project.Id)
	require.NoError(t, err)
	require.Equal(t, types.ProjectStatus_PROJECT_STATUS_EXPIRED, expired.Status)
	require.Equal(t, types.InterimStatus_INTERIM_STATUS_EXPIRED, rf.interim(t, project.ApprovalInterimId).Status)
	count, err := k.CountProposedProjectsForCreator(rf.ctx, rf.creator.String())
	require.NoError(t, err)
	require.Zero(t, count, "an expired proposal leaves the creator index")
}

// REP-E-202: an assignee who migrated between assignment and payout is paid
// under the new address; the payout does not fail on the tombstone.
func TestREPE202_MigratedAssigneeIsPaid(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx
	oldAddr := sdk.AccAddress([]byte("repe202-old---------"))
	newAddr := sdk.AccAddress([]byte("repe202-new---------"))
	econMember(t, f, newAddr, 0, 0)
	id, err := k.CreateInterimWork(ctx, types.InterimType_INTERIM_TYPE_AUDIT,
		[]string{oldAddr.String()}, "", 0, "", types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE, ctx.BlockHeight()+50)
	require.NoError(t, err)
	// econMember seeds an ACTIVE record under newAddr; the assignee record
	// under oldAddr never existed, exactly as after a finalized migration.
	require.NoError(t, k.Tombstones.Set(ctx, oldAddr.String(), types.Tombstone{Canonical: newAddr.String()}))

	require.NoError(t, k.ApproveInterim(ctx, id, k.GetAuthority(), true, "done"))
	m := econGet(t, f, newAddr)
	require.True(t, m.DreamBalance.IsPositive(), "paid to the canonical record")
	require.Equal(t, uint32(1), m.CompletedInterimsCount)
	require.Equal(t, types.InterimStatus_INTERIM_STATUS_COMPLETED, interimStatus(t, f, id))
}

// REP-E-203: zeroing one assignee of a shared interim drops only their seat;
// a solo interim is expired.
func TestREPE203_ZeroingDropsOnlyTheZeroedSeat(t *testing.T) {
	rf := newProjectReviewFixture(t, 2)
	k := rf.keeper
	project := rf.propose(t, 1_000_000, 0)
	solo, err := k.CreateInterimWork(rf.ctx, types.InterimType_INTERIM_TYPE_AUDIT,
		[]string{rf.committee[1].String()}, "", 0, "", types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE,
		sdk.UnwrapSDKContext(rf.ctx).BlockHeight()+50)
	require.NoError(t, err)

	require.NoError(t, k.ZeroMember(rf.ctx, rf.committee[1], "misconduct"))

	review := rf.interim(t, project.ApprovalInterimId)
	require.Equal(t, types.InterimStatus_INTERIM_STATUS_PENDING, review.Status, "shared review stays live")
	require.Equal(t, []string{rf.committee[0].String()}, review.Assignees)
	require.Equal(t, types.InterimStatus_INTERIM_STATUS_EXPIRED, rf.interim(t, solo).Status, "solo interim expires")
	ids, err := k.LiveInterimIDsForAddresses(rf.ctx, []string{rf.committee[1].String()})
	require.NoError(t, err)
	require.Empty(t, ids)

	// The remaining member is still paid when the project is approved.
	_, err = rf.ms.ApproveProjectBudget(rf.ctx, &types.MsgApproveProjectBudget{
		Approver: rf.committee[0].String(), ProjectId: project.Id,
		ApprovedBudget: keeper.PtrInt(math.NewInt(1_000_000)), ApprovedSpark: keeper.PtrInt(math.ZeroInt()),
	})
	require.NoError(t, err)
	require.Equal(t, types.InterimStatus_INTERIM_STATUS_COMPLETED, rf.interim(t, project.ApprovalInterimId).Status)
	require.True(t, econGet(t, rf.fixture, rf.committee[0]).DreamBalance.IsPositive())
}

// REP-E-204: the season baseline is granted only to ACTIVE members and only
// on tags that held a positive score.
func TestREPE204_BaselineResetSkipsZeroedMembersAndZeroTags(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx
	baseline := math.LegacyNewDec(5)

	active := sdk.AccAddress([]byte("repe204-active------"))
	econMember(t, f, active, 0, 0)
	m := econGet(t, f, active)
	m.ReputationScores = map[string]string{"backend": "40", "design": "0"}
	require.NoError(t, k.Member.Set(ctx, active.String(), m))
	_, err := k.ArchiveSeasonalReputation(ctx, active.String(), baseline)
	require.NoError(t, err)
	m = econGet(t, f, active)
	require.Equal(t, map[string]string{"backend": baseline.String()}, m.ReputationScores,
		"only the tag with a positive score restarts at baseline")
	require.Equal(t, "40.000000000000000000", m.LifetimeReputation["backend"])

	zeroed := sdk.AccAddress([]byte("repe204-zeroed------"))
	econMember(t, f, zeroed, 0, 0)
	m = econGet(t, f, zeroed)
	m.Status = types.MemberStatus_MEMBER_STATUS_ZEROED
	m.ReputationScores = map[string]string{"backend": "0", "design": "0"}
	require.NoError(t, k.Member.Set(ctx, zeroed.String(), m))
	_, err = k.ArchiveSeasonalReputation(ctx, zeroed.String(), baseline)
	require.NoError(t, err)
	require.Empty(t, econGet(t, f, zeroed).ReputationScores, "a zeroed member gets no baseline")

	// A zeroed member whose record still carried a positive score (zeroed
	// through an older path) is not re-baselined either.
	m = econGet(t, f, zeroed)
	m.ReputationScores = map[string]string{"backend": "12"}
	require.NoError(t, k.Member.Set(ctx, zeroed.String(), m))
	_, err = k.ArchiveSeasonalReputation(ctx, zeroed.String(), baseline)
	require.NoError(t, err)
	require.Empty(t, econGet(t, f, zeroed).ReputationScores)
}

// REP-E-205: interim_deadline_epochs is validated on both param sets.
func TestREPE205_InterimDeadlineEpochsValidated(t *testing.T) {
	cases := []struct {
		name   string
		epochs int64
		blocks int64
		ok     bool
	}{
		{"zero", 0, 14400, false},
		{"negative", -1, 14400, false},
		{"one", 1, 14400, true},
		{"default", 7, 14400, true},
		{"at the cap", types.MaxInterimDeadlineEpochs, 14400, true},
		{"over the cap", types.MaxInterimDeadlineEpochs + 1, 14400, false},
		{"product overflows", 3650, 1 << 60, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := types.DefaultParams()
			p.InterimDeadlineEpochs, p.EpochBlocks = tc.epochs, tc.blocks
			op := p.ExtractOperationalParams()
			if tc.ok {
				require.NoError(t, p.Validate())
				require.NoError(t, op.Validate())
				return
			}
			require.Error(t, p.Validate())
			require.Error(t, op.Validate())
		})
	}
	p := types.DefaultParams()
	p.MaxProposedProjectsPerCreator = 0
	require.Error(t, p.Validate(), "max_proposed_projects_per_creator must be positive")
}

// REP-E-206: the review pays only the assignees who took part in the
// approval, never the proposer; abandoning a review seat drops that seat
// only.
func TestREPE206_ReviewPaysVotersNotProposer(t *testing.T) {
	t.Run("proposer on the committee is not paid; non-voter is not paid", func(t *testing.T) {
		rf := newProjectReviewFixture(t, 2)
		// The creator sits on the committee too.
		rf.commonsKeeper.ListCommitteeMembersFn = func(context.Context, string, string) ([]string, error) {
			return []string{rf.creator.String(), rf.committee[0].String(), rf.committee[1].String()}, nil
		}
		project := rf.propose(t, 1_000_000, 0)
		interim := rf.interim(t, project.ApprovalInterimId)
		require.Len(t, interim.Assignees, 3)
		share := interim.Budget.QuoRaw(3)

		// Everyone but committee[1] voted on proposal 9.
		rf.commonsKeeper.HasVotedFn = func(_ context.Context, proposalID uint64, voter string) bool {
			return proposalID == 9 && voter != rf.committee[1].String()
		}
		ctx := sdk.UnwrapSDKContext(rf.ctx).WithValue(commontypes.ContextKeyExecutingProposal, uint64(9))
		policy := sdk.AccAddress([]byte("ops-policy-------"))
		rf.commonsKeeper.IsCouncilOrCommitteePolicyFn = func(_ context.Context, addr, _, _ string) bool {
			return addr == policy.String()
		}
		_, err := rf.ms.ApproveProjectBudget(ctx, &types.MsgApproveProjectBudget{
			Approver: policy.String(), ProjectId: project.Id,
			ApprovedBudget: keeper.PtrInt(math.NewInt(1_000_000)), ApprovedSpark: keeper.PtrInt(math.ZeroInt()),
		})
		require.NoError(t, err)
		require.Equal(t, types.InterimStatus_INTERIM_STATUS_COMPLETED, rf.interim(t, project.ApprovalInterimId).Status)

		creatorBefore := math.NewInt(100_000_000).Sub(mustParams(t, rf.fixture).ProjectCreationFee)
		require.Equal(t, creatorBefore.String(), econGet(t, rf.fixture, rf.creator).DreamBalance.String(), "proposer unpaid")
		require.Equal(t, share.String(), econGet(t, rf.fixture, rf.committee[0]).DreamBalance.String(), "voter paid a third")
		require.True(t, econGet(t, rf.fixture, rf.committee[1]).DreamBalance.IsZero(), "non-voter unpaid")
		minted, err := rf.keeper.GetSeasonInterimRewardsMinted(rf.ctx)
		require.NoError(t, err)
		require.Equal(t, share.String(), minted.String(), "unpaid shares are not minted")
	})

	t.Run("approval nobody on the committee took part in pays nobody", func(t *testing.T) {
		rf := newProjectReviewFixture(t, 1)
		project := rf.propose(t, 1_000_000, 0)
		gov := sdk.AccAddress([]byte("gov-authority----"))
		rf.commonsKeeper.IsCouncilOrCommitteePolicyFn = func(_ context.Context, addr, _, _ string) bool {
			return addr == gov.String()
		}
		_, err := rf.ms.ApproveProjectBudget(rf.ctx, &types.MsgApproveProjectBudget{
			Approver: gov.String(), ProjectId: project.Id,
			ApprovedBudget: keeper.PtrInt(math.NewInt(1_000_000)), ApprovedSpark: keeper.PtrInt(math.ZeroInt()),
		})
		require.NoError(t, err)
		require.Equal(t, types.InterimStatus_INTERIM_STATUS_EXPIRED, rf.interim(t, project.ApprovalInterimId).Status)
		require.True(t, econGet(t, rf.fixture, rf.committee[0]).DreamBalance.IsZero())
	})

	t.Run("abandoning a review seat drops that seat only", func(t *testing.T) {
		rf := newProjectReviewFixture(t, 2)
		project := rf.propose(t, 1_000_000, 0)
		_, err := rf.ms.AbandonInterim(rf.ctx, &types.MsgAbandonInterim{
			Creator: rf.committee[1].String(), InterimId: project.ApprovalInterimId, Reason: "conflict",
		})
		require.NoError(t, err)
		review := rf.interim(t, project.ApprovalInterimId)
		require.Equal(t, types.InterimStatus_INTERIM_STATUS_PENDING, review.Status)
		require.Equal(t, []string{rf.committee[0].String()}, review.Assignees)

		// The last seat gone, the review ends unpaid.
		_, err = rf.ms.AbandonInterim(rf.ctx, &types.MsgAbandonInterim{
			Creator: rf.committee[0].String(), InterimId: project.ApprovalInterimId, Reason: "conflict",
		})
		require.NoError(t, err)
		review = rf.interim(t, project.ApprovalInterimId)
		require.Equal(t, types.InterimStatus_INTERIM_STATUS_EXPIRED, review.Status)
		require.Empty(t, review.Assignees)
	})

	t.Run("abandoned seats do not raise the voter's share", func(t *testing.T) {
		rf := newProjectReviewFixture(t, 3)
		project := rf.propose(t, 1_000_000, 0)
		opened := rf.interim(t, project.ApprovalInterimId)
		require.Len(t, opened.Assignees, 3)
		share := opened.Budget.QuoRaw(3)
		for _, m := range rf.committee[1:] {
			_, err := rf.ms.AbandonInterim(rf.ctx, &types.MsgAbandonInterim{
				Creator: m.String(), InterimId: project.ApprovalInterimId, Reason: "will not vote",
			})
			require.NoError(t, err)
		}
		require.Equal(t, share.String(), rf.interim(t, project.ApprovalInterimId).Budget.String(),
			"the budget keeps only the seat left")

		rf.commonsKeeper.HasVotedFn = func(_ context.Context, proposalID uint64, voter string) bool {
			return proposalID == 9 && voter == rf.committee[0].String()
		}
		ctx := sdk.UnwrapSDKContext(rf.ctx).WithValue(commontypes.ContextKeyExecutingProposal, uint64(9))
		policy := sdk.AccAddress([]byte("ops-policy-------"))
		rf.commonsKeeper.IsCouncilOrCommitteePolicyFn = func(_ context.Context, addr, _, _ string) bool {
			return addr == policy.String()
		}
		_, err := rf.ms.ApproveProjectBudget(ctx, &types.MsgApproveProjectBudget{
			Approver: policy.String(), ProjectId: project.Id,
			ApprovedBudget: keeper.PtrInt(math.NewInt(1_000_000)), ApprovedSpark: keeper.PtrInt(math.ZeroInt()),
		})
		require.NoError(t, err)
		require.Equal(t, share.String(), econGet(t, rf.fixture, rf.committee[0]).DreamBalance.String(),
			"the one voter is paid a third, not the whole budget")
	})

	t.Run("review is assigned to the picked council's committee", func(t *testing.T) {
		rf := newProjectReviewFixture(t, 1)
		var asked string
		rf.commonsKeeper.ListCommitteeMembersFn = func(_ context.Context, council, committee string) ([]string, error) {
			asked = council + "/" + committee
			return []string{rf.committee[0].String()}, nil
		}
		_, err := rf.ms.ProposeProject(rf.ctx, &types.MsgProposeProject{
			Creator: rf.creator.String(), Name: "p", Description: "d", Council: "technical",
			RequestedBudget: keeper.PtrInt(math.NewInt(1)), RequestedSpark: keeper.PtrInt(math.ZeroInt()),
		})
		require.NoError(t, err)
		require.Equal(t, "technical/operations", asked)
		require.Equal(t, "technical_operations", rf.interim(t, 1).Committee)
	})
}

// REP-E-207: the approval is bounded by what was requested.
func TestREPE207_ApprovalBoundedByRequest(t *testing.T) {
	rf := newProjectReviewFixture(t, 1)
	project := rf.propose(t, 1_000_000, 500)
	require.Equal(t, "1000000", project.RequestedBudget.String())
	require.Equal(t, "500", project.RequestedSpark.String())

	approve := func(budget, spark int64) error {
		_, err := rf.ms.ApproveProjectBudget(rf.ctx, &types.MsgApproveProjectBudget{
			Approver: rf.committee[0].String(), ProjectId: project.Id,
			ApprovedBudget: keeper.PtrInt(math.NewInt(budget)), ApprovedSpark: keeper.PtrInt(math.NewInt(spark)),
		})
		return err
	}
	require.ErrorIs(t, approve(1_000_001, 0), types.ErrApprovedBudgetExceedsRequested)
	require.ErrorIs(t, approve(1_000_000, 501), types.ErrApprovedBudgetExceedsRequested)
	require.NoError(t, approve(1_000_000, 500))
}

// REP-E-208: DREAM is never reissued to an inactive member.
func TestREPE208_ReissueRefusesInactiveRecipient(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx
	author := sdk.AccAddress([]byte("repe208-author------"))
	econMember(t, f, author, 0, 0)
	m := econGet(t, f, author)
	m.Status = types.MemberStatus_MEMBER_STATUS_ZEROED
	require.NoError(t, k.Member.Set(ctx, author.String(), m))

	// A slash reversal for a zeroed author restores nothing and does not fail.
	require.NoError(t, k.RestoreAuthorBond(ctx, author, types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND, 3, math.NewInt(50_000_000)))
	require.True(t, econGet(t, f, author).DreamBalance.IsZero())
	require.True(t, econGet(t, f, author).StakedDream.IsZero())
	require.True(t, hasEvent(ctx, "author_bond_restore_skipped"))
}

func interimStatus(t *testing.T, f *fixture, id uint64) types.InterimStatus {
	t.Helper()
	interim, err := f.keeper.GetInterim(f.ctx, id)
	require.NoError(t, err)
	return interim.Status
}
