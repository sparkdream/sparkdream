package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// Project review interims: a budget-backed MsgProposeProject opens a
// PROJECT_APPROVAL (STANDARD) or BUDGET_REVIEW (COMPLEX, requested budget at
// or above budget_review_threshold) interim for the Commons Operations
// Committee; MsgApproveProjectBudget pays it, a cancelled or expired proposal
// ends it unpaid, and members cannot create, submit, approve or complete one.

type projectReviewFixture struct {
	*fixture
	ms        types.MsgServer
	creator   sdk.AccAddress
	committee []sdk.AccAddress
}

// newProjectReviewFixture seeds an active creator (funded for the creation
// fee) and n active Commons Operations Committee members, and points the
// commons mock's ListCommitteeMembers at them. Every address is authorized by
// the default policy, so a committee member can approve the project directly.
func newProjectReviewFixture(t *testing.T, n int) *projectReviewFixture {
	t.Helper()
	f := initFixture(t)
	creator := sdk.AccAddress([]byte("review-creator___"))
	setupMemberWithStatus(t, f, creator, types.MemberStatus_MEMBER_STATUS_ACTIVE, types.TrustLevel_TRUST_LEVEL_ESTABLISHED)
	require.NoError(t, f.keeper.MintDREAM(f.ctx, creator, math.NewInt(100_000_000)))

	var committee []sdk.AccAddress
	var committeeStrs []string
	for i := 0; i < n; i++ {
		addr := sdk.AccAddress([]byte("ops-committee-" + string(rune('a'+i)) + "__"))
		setupMemberWithStatus(t, f, addr, types.MemberStatus_MEMBER_STATUS_ACTIVE, types.TrustLevel_TRUST_LEVEL_ESTABLISHED)
		committee = append(committee, addr)
		committeeStrs = append(committeeStrs, addr.String())
	}
	f.commonsKeeper.ListCommitteeMembersFn = func(_ context.Context, council, committee string) ([]string, error) {
		require.Equal(t, "commons", council)
		require.Equal(t, "operations", committee)
		return committeeStrs, nil
	}
	return &projectReviewFixture{
		fixture:   f,
		ms:        keeper.NewMsgServerImpl(f.keeper),
		creator:   creator,
		committee: committee,
	}
}

func (rf *projectReviewFixture) propose(t *testing.T, budget, spark int64) types.Project {
	t.Helper()
	_, err := rf.ms.ProposeProject(rf.ctx, &types.MsgProposeProject{
		Creator:         rf.creator.String(),
		Name:            "Review me",
		Description:     "A budget-backed proposal",
		Tags:            []string{"backend"},
		Category:        types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE,
		Council:         "commons",
		RequestedBudget: keeper.PtrInt(math.NewInt(budget)),
		RequestedSpark:  keeper.PtrInt(math.NewInt(spark)),
	})
	require.NoError(t, err)
	// The sequence starts at 1 and each fixture proposes at most once.
	project, err := rf.keeper.GetProject(rf.ctx, 1)
	require.NoError(t, err)
	return project
}

func (rf *projectReviewFixture) interim(t *testing.T, id uint64) types.Interim {
	t.Helper()
	interim, err := rf.keeper.GetInterim(rf.ctx, id)
	require.NoError(t, err)
	return interim
}

func TestProjectReviewInterim_OpenedOnBudgetBackedProposal(t *testing.T) {
	t.Run("below threshold opens PROJECT_APPROVAL at STANDARD", func(t *testing.T) {
		rf := newProjectReviewFixture(t, 2)
		params, err := rf.keeper.Params.Get(rf.ctx)
		require.NoError(t, err)

		project := rf.propose(t, params.BudgetReviewThreshold.Int64()-1, 0)
		require.Equal(t, types.ProjectStatus_PROJECT_STATUS_PROPOSED, project.Status)
		require.NotZero(t, project.ApprovalInterimId)

		interim := rf.interim(t, project.ApprovalInterimId)
		require.Equal(t, types.InterimType_INTERIM_TYPE_PROJECT_APPROVAL, interim.Type)
		require.Equal(t, types.InterimComplexity_INTERIM_COMPLEXITY_STANDARD, interim.Complexity)
		require.Equal(t, params.StandardComplexityBudget.String(), interim.Budget.String())
		require.Equal(t, types.InterimStatus_INTERIM_STATUS_PENDING, interim.Status)
		require.Equal(t, project.Id, interim.ReferenceId)
		require.Equal(t, "project", interim.ReferenceType)
		require.Equal(t, []string{rf.committee[0].String(), rf.committee[1].String()}, interim.Assignees)
		// The review lives as long as the proposal (REP-E-205).
		require.Equal(t, project.ExpiryBlockHeight, interim.Deadline)
		require.Equal(t, "commons_operations", interim.Committee)
	})

	t.Run("at threshold opens BUDGET_REVIEW at COMPLEX", func(t *testing.T) {
		rf := newProjectReviewFixture(t, 1)
		params, err := rf.keeper.Params.Get(rf.ctx)
		require.NoError(t, err)

		project := rf.propose(t, params.BudgetReviewThreshold.Int64(), 0)
		interim := rf.interim(t, project.ApprovalInterimId)
		require.Equal(t, types.InterimType_INTERIM_TYPE_BUDGET_REVIEW, interim.Type)
		require.Equal(t, types.InterimComplexity_INTERIM_COMPLEXITY_COMPLEX, interim.Complexity)
		require.Equal(t, params.ComplexComplexityBudget.String(), interim.Budget.String())
	})

	t.Run("SPARK-only request still opens PROJECT_APPROVAL", func(t *testing.T) {
		rf := newProjectReviewFixture(t, 1)
		project := rf.propose(t, 0, 1_000_000)
		require.Equal(t, types.ProjectStatus_PROJECT_STATUS_PROPOSED, project.Status)
		interim := rf.interim(t, project.ApprovalInterimId)
		require.Equal(t, types.InterimType_INTERIM_TYPE_PROJECT_APPROVAL, interim.Type)
	})

	t.Run("empty committee skips the interim", func(t *testing.T) {
		rf := newProjectReviewFixture(t, 0)
		project := rf.propose(t, 1_000_000, 0)
		require.Equal(t, types.ProjectStatus_PROJECT_STATUS_PROPOSED, project.Status)
		require.Zero(t, project.ApprovalInterimId)
		_, err := rf.keeper.GetInterim(rf.ctx, 1)
		require.Error(t, err)
		require.True(t, hasEvent(rf.ctx, "project_approval_interim_skipped"))
	})

	t.Run("committee lookup failure does not fail the proposal", func(t *testing.T) {
		rf := newProjectReviewFixture(t, 1)
		rf.commonsKeeper.ListCommitteeMembersFn = func(context.Context, string, string) ([]string, error) {
			return nil, context.DeadlineExceeded
		}
		project := rf.propose(t, 1_000_000, 0)
		require.Equal(t, types.ProjectStatus_PROJECT_STATUS_PROPOSED, project.Status)
		require.Zero(t, project.ApprovalInterimId)
		require.True(t, hasEvent(rf.ctx, "project_approval_interim_failed"))
	})

	t.Run("permissionless proposal opens nothing", func(t *testing.T) {
		rf := newProjectReviewFixture(t, 2)
		project := rf.propose(t, 0, 0)
		require.Equal(t, types.ProjectStatus_PROJECT_STATUS_ACTIVE, project.Status)
		require.True(t, project.Permissionless)
		require.Zero(t, project.ApprovalInterimId)
		_, err := rf.keeper.GetInterim(rf.ctx, 1)
		require.Error(t, err)
	})

	t.Run("bypasses max_active_interims_per_member for committee members", func(t *testing.T) {
		rf := newProjectReviewFixture(t, 1)
		params, err := rf.keeper.Params.Get(rf.ctx)
		require.NoError(t, err)
		params.MaxActiveInterimsPerMember = 1
		require.NoError(t, rf.keeper.Params.Set(rf.ctx, params))

		// The committee member already holds one live interim of their own.
		_, err = rf.keeper.CreateInterimWork(rf.ctx, types.InterimType_INTERIM_TYPE_AUDIT,
			[]string{rf.committee[0].String()}, "", 0, "", types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE,
			sdk.UnwrapSDKContext(rf.ctx).BlockHeight()+100)
		require.NoError(t, err)

		project := rf.propose(t, 1_000_000, 0)
		require.NotZero(t, project.ApprovalInterimId)
		require.Equal(t, types.InterimType_INTERIM_TYPE_PROJECT_APPROVAL, rf.interim(t, project.ApprovalInterimId).Type)
	})
}

func TestProjectReviewInterim_SettledOnApproval(t *testing.T) {
	t.Run("a committee proposal pays every member who voted an equal share", func(t *testing.T) {
		rf := newProjectReviewFixture(t, 2)
		project := rf.propose(t, 1_000_000, 0)
		interim := rf.interim(t, project.ApprovalInterimId)
		share := interim.Budget.QuoRaw(2)

		// The approval executes from committee proposal 7, on which both
		// members voted.
		rf.commonsKeeper.HasVotedFn = func(_ context.Context, proposalID uint64, voter string) bool {
			return proposalID == 7
		}
		ctx := sdk.UnwrapSDKContext(rf.ctx).WithValue(commontypes.ContextKeyExecutingProposal, uint64(7))
		_, err := rf.ms.ApproveProjectBudget(ctx, &types.MsgApproveProjectBudget{
			Approver:       rf.committee[0].String(),
			ProjectId:      project.Id,
			ApprovedBudget: keeper.PtrInt(math.NewInt(1_000_000)),
			ApprovedSpark:  keeper.PtrInt(math.ZeroInt()),
		})
		require.NoError(t, err)

		approved, err := rf.keeper.GetProject(rf.ctx, project.Id)
		require.NoError(t, err)
		require.Equal(t, types.ProjectStatus_PROJECT_STATUS_ACTIVE, approved.Status)

		settled := rf.interim(t, project.ApprovalInterimId)
		require.Equal(t, types.InterimStatus_INTERIM_STATUS_COMPLETED, settled.Status)
		require.NotZero(t, settled.CompletedAt)
		for _, addr := range rf.committee {
			member, err := rf.keeper.GetMember(rf.ctx, addr)
			require.NoError(t, err)
			require.Equal(t, share.String(), member.DreamBalance.String())
			require.Equal(t, uint32(1), member.CompletedInterimsCount)
		}
		minted, err := rf.keeper.GetSeasonInterimRewardsMinted(rf.ctx)
		require.NoError(t, err)
		require.Equal(t, share.MulRaw(2).String(), minted.String())
		require.True(t, hasEvent(ctx, "project_approval_interim_settled"))
	})

	t.Run("a single-member approval pays the approver alone", func(t *testing.T) {
		rf := newProjectReviewFixture(t, 2)
		project := rf.propose(t, 1_000_000, 0)
		share := rf.interim(t, project.ApprovalInterimId).Budget.QuoRaw(2)

		_, err := rf.ms.ApproveProjectBudget(rf.ctx, &types.MsgApproveProjectBudget{
			Approver:       rf.committee[0].String(),
			ProjectId:      project.Id,
			ApprovedBudget: keeper.PtrInt(math.NewInt(1_000_000)),
			ApprovedSpark:  keeper.PtrInt(math.ZeroInt()),
		})
		require.NoError(t, err)
		require.Equal(t, types.InterimStatus_INTERIM_STATUS_COMPLETED, rf.interim(t, project.ApprovalInterimId).Status)
		approver, err := rf.keeper.GetMember(rf.ctx, rf.committee[0])
		require.NoError(t, err)
		require.Equal(t, share.String(), approver.DreamBalance.String(), "share stays budget / assignees")
		other, err := rf.keeper.GetMember(rf.ctx, rf.committee[1])
		require.NoError(t, err)
		require.True(t, other.DreamBalance.IsZero(), "an unvoted seat is unpaid")
		require.True(t, hasEvent(rf.ctx, "interim_assignee_skipped"))
	})

	t.Run("a payout refused by the season cap does not fail the approval", func(t *testing.T) {
		rf := newProjectReviewFixture(t, 2)
		project := rf.propose(t, 1_000_000, 0)
		params, err := rf.keeper.Params.Get(rf.ctx)
		require.NoError(t, err)
		params.MaxInterimRewardsPerSeason = math.NewInt(1)
		require.NoError(t, rf.keeper.Params.Set(rf.ctx, params))

		_, err = rf.ms.ApproveProjectBudget(rf.ctx, &types.MsgApproveProjectBudget{
			Approver:       rf.committee[0].String(),
			ProjectId:      project.Id,
			ApprovedBudget: keeper.PtrInt(math.NewInt(1_000_000)),
			ApprovedSpark:  keeper.PtrInt(math.ZeroInt()),
		})
		require.NoError(t, err)

		approved, err := rf.keeper.GetProject(rf.ctx, project.Id)
		require.NoError(t, err)
		require.Equal(t, types.ProjectStatus_PROJECT_STATUS_ACTIVE, approved.Status)
		// Unpaid and still live: the deadline sweep retires it.
		require.Equal(t, types.InterimStatus_INTERIM_STATUS_PENDING, rf.interim(t, project.ApprovalInterimId).Status)
		for _, addr := range rf.committee {
			member, err := rf.keeper.GetMember(rf.ctx, addr)
			require.NoError(t, err)
			require.True(t, member.DreamBalance.IsZero())
		}
		require.True(t, hasEvent(rf.ctx, "project_approval_interim_settlement_failed"))
	})

	t.Run("approval with no interim is a no-op", func(t *testing.T) {
		rf := newProjectReviewFixture(t, 0)
		project := rf.propose(t, 1_000_000, 0)
		require.Zero(t, project.ApprovalInterimId)
		approver := sdk.AccAddress([]byte("approver_________"))
		_, err := rf.ms.ApproveProjectBudget(rf.ctx, &types.MsgApproveProjectBudget{
			Approver:       approver.String(),
			ProjectId:      project.Id,
			ApprovedBudget: keeper.PtrInt(math.NewInt(1_000_000)),
			ApprovedSpark:  keeper.PtrInt(math.ZeroInt()),
		})
		require.NoError(t, err)
		require.False(t, hasEvent(rf.ctx, "project_approval_interim_settled"))
	})
}

func TestProjectReviewInterim_EndsUnpaidWithoutApproval(t *testing.T) {
	t.Run("creator cancels the proposal", func(t *testing.T) {
		rf := newProjectReviewFixture(t, 2)
		project := rf.propose(t, 1_000_000, 0)

		_, err := rf.ms.CancelProject(rf.ctx, &types.MsgCancelProject{
			Creator:   rf.creator.String(),
			ProjectId: project.Id,
			Reason:    "changed my mind",
		})
		require.NoError(t, err)

		cancelled := rf.interim(t, project.ApprovalInterimId)
		require.Equal(t, types.InterimStatus_INTERIM_STATUS_EXPIRED, cancelled.Status)
		require.Contains(t, cancelled.CompletionNotes, "project cancelled")
		for _, addr := range rf.committee {
			member, err := rf.keeper.GetMember(rf.ctx, addr)
			require.NoError(t, err)
			require.True(t, member.DreamBalance.IsZero())
		}
		require.True(t, hasEvent(rf.ctx, "project_approval_interim_cancelled"))
		// Out of the pending sweep.
		count, err := rf.keeper.CountActiveInterimsForMember(rf.ctx, rf.committee[0].String())
		require.NoError(t, err)
		require.Zero(t, count)
	})

	t.Run("proposal expires in the EndBlocker", func(t *testing.T) {
		rf := newProjectReviewFixture(t, 1)
		project := rf.propose(t, 1_000_000, 0)

		require.NoError(t, rf.keeper.ExpireProject(rf.ctx, project.Id))
		expired, err := rf.keeper.GetProject(rf.ctx, project.Id)
		require.NoError(t, err)
		require.Equal(t, types.ProjectStatus_PROJECT_STATUS_EXPIRED, expired.Status)
		require.Equal(t, types.InterimStatus_INTERIM_STATUS_EXPIRED, rf.interim(t, project.ApprovalInterimId).Status)
		member, err := rf.keeper.GetMember(rf.ctx, rf.committee[0])
		require.NoError(t, err)
		require.True(t, member.DreamBalance.IsZero())
	})

	t.Run("cancelling an approved project leaves the settled interim alone", func(t *testing.T) {
		rf := newProjectReviewFixture(t, 1)
		project := rf.propose(t, 1_000_000, 0)
		_, err := rf.ms.ApproveProjectBudget(rf.ctx, &types.MsgApproveProjectBudget{
			Approver:       rf.committee[0].String(),
			ProjectId:      project.Id,
			ApprovedBudget: keeper.PtrInt(math.NewInt(1_000_000)),
			ApprovedSpark:  keeper.PtrInt(math.ZeroInt()),
		})
		require.NoError(t, err)
		require.Equal(t, types.InterimStatus_INTERIM_STATUS_COMPLETED, rf.interim(t, project.ApprovalInterimId).Status)

		require.NoError(t, rf.keeper.CancelProject(rf.ctx, project.Id, "wound down"))
		require.Equal(t, types.InterimStatus_INTERIM_STATUS_COMPLETED, rf.interim(t, project.ApprovalInterimId).Status)
	})

	t.Run("interim deadline sweep leaves the proposal untouched", func(t *testing.T) {
		rf := newProjectReviewFixture(t, 1)
		project := rf.propose(t, 1_000_000, 0)

		require.NoError(t, rf.keeper.ExpireInterim(rf.ctx, project.ApprovalInterimId))
		require.Equal(t, types.InterimStatus_INTERIM_STATUS_EXPIRED, rf.interim(t, project.ApprovalInterimId).Status)
		still, err := rf.keeper.GetProject(rf.ctx, project.Id)
		require.NoError(t, err)
		require.Equal(t, types.ProjectStatus_PROJECT_STATUS_PROPOSED, still.Status)
		member, err := rf.keeper.GetMember(rf.ctx, rf.committee[0])
		require.NoError(t, err)
		require.True(t, member.DreamBalance.IsZero())

		// A later approval still goes through; the expired review is not paid.
		_, err = rf.ms.ApproveProjectBudget(rf.ctx, &types.MsgApproveProjectBudget{
			Approver:       rf.committee[0].String(),
			ProjectId:      project.Id,
			ApprovedBudget: keeper.PtrInt(math.NewInt(1_000_000)),
			ApprovedSpark:  keeper.PtrInt(math.ZeroInt()),
		})
		require.NoError(t, err)
		require.Equal(t, types.InterimStatus_INTERIM_STATUS_EXPIRED, rf.interim(t, project.ApprovalInterimId).Status)
		member, err = rf.keeper.GetMember(rf.ctx, rf.committee[0])
		require.NoError(t, err)
		require.True(t, member.DreamBalance.IsZero())
	})
}

func TestProjectReviewInterim_SystemOnly(t *testing.T) {
	for _, interimType := range []types.InterimType{
		types.InterimType_INTERIM_TYPE_PROJECT_APPROVAL,
		types.InterimType_INTERIM_TYPE_BUDGET_REVIEW,
	} {
		t.Run("member cannot create "+interimType.String(), func(t *testing.T) {
			rf := newProjectReviewFixture(t, 1)
			_, err := rf.ms.CreateInterim(rf.ctx, &types.MsgCreateInterim{
				Creator:     rf.creator.String(),
				InterimType: interimType,
				Complexity:  types.InterimComplexity_INTERIM_COMPLEXITY_STANDARD,
			})
			require.ErrorIs(t, err, types.ErrInterimTypeSystemOnly)
		})
	}
	require.True(t, keeper.IsSystemOnlyInterimType(types.InterimType_INTERIM_TYPE_PROJECT_APPROVAL))
	require.True(t, keeper.IsSystemOnlyInterimType(types.InterimType_INTERIM_TYPE_BUDGET_REVIEW))

	t.Run("MsgApproveInterim is refused", func(t *testing.T) {
		rf := newProjectReviewFixture(t, 1)
		project := rf.propose(t, 1_000_000, 0)
		approver := sdk.AccAddress([]byte("ops-policy_______"))
		_, err := rf.ms.ApproveInterim(rf.ctx, &types.MsgApproveInterim{
			Creator:   approver.String(),
			InterimId: project.ApprovalInterimId,
			Approved:  true,
		})
		require.ErrorIs(t, err, types.ErrInterimTypeSystemOnly)
		require.Equal(t, types.InterimStatus_INTERIM_STATUS_PENDING, rf.interim(t, project.ApprovalInterimId).Status)
	})

	t.Run("MsgCompleteInterim is refused", func(t *testing.T) {
		rf := newProjectReviewFixture(t, 1)
		project := rf.propose(t, 1_000_000, 0)
		_, err := rf.ms.CompleteInterim(rf.ctx, &types.MsgCompleteInterim{
			Creator:   rf.committee[0].String(),
			InterimId: project.ApprovalInterimId,
		})
		require.ErrorIs(t, err, types.ErrInterimTypeSystemOnly)
		require.Equal(t, types.InterimStatus_INTERIM_STATUS_PENDING, rf.interim(t, project.ApprovalInterimId).Status)
	})

	t.Run("MsgSubmitInterimWork is refused", func(t *testing.T) {
		rf := newProjectReviewFixture(t, 1)
		project := rf.propose(t, 1_000_000, 0)
		_, err := rf.ms.SubmitInterimWork(rf.ctx, &types.MsgSubmitInterimWork{
			Creator:        rf.committee[0].String(),
			InterimId:      project.ApprovalInterimId,
			DeliverableUri: "ipfs://review",
		})
		require.ErrorIs(t, err, types.ErrInterimTypeSystemOnly)
		require.Equal(t, types.InterimStatus_INTERIM_STATUS_PENDING, rf.interim(t, project.ApprovalInterimId).Status)
	})
}

func TestParams_BudgetReviewThreshold(t *testing.T) {
	params := types.DefaultParams()
	require.Equal(t, types.DefaultBudgetReviewThreshold.String(), params.BudgetReviewThreshold.String())
	require.NoError(t, params.Validate())

	params.BudgetReviewThreshold = math.NewInt(-1)
	require.Error(t, params.Validate())
	params.BudgetReviewThreshold = math.ZeroInt()
	require.NoError(t, params.Validate())

	// Governance-only: not carried by the operational params.
	op := types.DefaultParams().ExtractOperationalParams()
	merged := types.DefaultParams()
	merged.BudgetReviewThreshold = math.NewInt(42)
	require.Equal(t, "42", merged.ApplyOperationalParams(op).BudgetReviewThreshold.String())
}

// hasEvent reports whether an event of the given type was emitted on ctx.
func hasEvent(ctx context.Context, eventType string) bool {
	for _, ev := range sdk.UnwrapSDKContext(ctx).EventManager().Events() {
		if ev.Type == eventType {
			return true
		}
	}
	return false
}
