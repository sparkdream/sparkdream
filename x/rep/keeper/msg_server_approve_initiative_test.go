package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestMsgServerApproveInitiative(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.ApproveInitiative(f.ctx, &types.MsgApproveInitiative{
			Creator:      "invalid-address",
			InitiativeId: 1,
			Approved:     true,
			Comments:     "LGTM",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("non-existent initiative", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		creator := sdk.AccAddress([]byte("creator"))
		creatorStr, err := f.addressCodec.BytesToString(creator)
		require.NoError(t, err)

		_, err = ms.ApproveInitiative(f.ctx, &types.MsgApproveInitiative{
			Creator:      creatorStr,
			InitiativeId: 99999,
			Approved:     true,
			Comments:     "LGTM",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})

	t.Run("invalid status - not SUBMITTED", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: create project, member, and initiative
		creator := sdk.AccAddress([]byte("creator"))
		k.Member.Set(ctx, creator.String(), types.Member{
			Address:          creator.String(),
			DreamBalance:     keeper.PtrInt(math.NewInt(10000)),
			StakedDream:      keeper.PtrInt(math.ZeroInt()),
			LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
			LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		})

		projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
		k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))

		budget := math.NewInt(100)
		initID, _ := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, budget)

		creatorStr, err := f.addressCodec.BytesToString(creator)
		require.NoError(t, err)

		// Try to approve initiative that is OPEN (not SUBMITTED)
		_, err = ms.ApproveInitiative(ctx, &types.MsgApproveInitiative{
			Creator:      creatorStr,
			InitiativeId: initID,
			Approved:     true,
			Comments:     "LGTM",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid initiative status")
	})

	t.Run("disapprove initiative", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: create project, member, and initiative
		creator := sdk.AccAddress([]byte("creator"))
		k.Member.Set(ctx, creator.String(), types.Member{
			Address:          creator.String(),
			DreamBalance:     keeper.PtrInt(math.NewInt(10000)),
			StakedDream:      keeper.PtrInt(math.ZeroInt()),
			LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
			LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		})

		projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
		k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))

		budget := math.NewInt(100)
		initID, _ := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, budget)

		approver := sdk.AccAddress([]byte("approver"))
		approverStr, err := f.addressCodec.BytesToString(approver)
		require.NoError(t, err)

		// Assign and submit work
		assignee := sdk.AccAddress([]byte("assignee"))
		assigneeStr, err := f.addressCodec.BytesToString(assignee)
		require.NoError(t, err)

		k.Member.Set(ctx, assigneeStr, types.Member{
			Address:          assigneeStr,
			DreamBalance:     keeper.PtrInt(math.ZeroInt()),
			StakedDream:      keeper.PtrInt(math.ZeroInt()),
			LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
			LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		})

		k.AssignInitiativeToMember(ctx, initID, assignee)
		k.SubmitInitiativeWork(ctx, initID, assignee, "uri")

		_, err = ms.ApproveInitiative(ctx, &types.MsgApproveInitiative{
			Creator:      approverStr,
			InitiativeId: initID,
			Approved:     false,
			Comments:     "Needs improvement",
		})
		require.NoError(t, err)

		initiative, err := k.GetInitiative(ctx, initID)
		require.NoError(t, err)
		require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_ABANDONED, initiative.Status)
	})

	t.Run("approve initiative", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: create project, member, and initiative
		creator := sdk.AccAddress([]byte("creator"))
		k.Member.Set(ctx, creator.String(), types.Member{
			Address:          creator.String(),
			DreamBalance:     keeper.PtrInt(math.NewInt(10000)),
			StakedDream:      keeper.PtrInt(math.ZeroInt()),
			LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
			LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		})

		projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
		k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))

		budget := math.NewInt(100)
		initID, _ := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, budget)

		approver := sdk.AccAddress([]byte("approver"))
		approverStr, err := f.addressCodec.BytesToString(approver)
		require.NoError(t, err)

		// Assign and submit work
		assignee := sdk.AccAddress([]byte("assignee"))
		assigneeStr, err := f.addressCodec.BytesToString(assignee)
		require.NoError(t, err)

		k.Member.Set(ctx, assigneeStr, types.Member{
			Address:          assigneeStr,
			DreamBalance:     keeper.PtrInt(math.ZeroInt()),
			StakedDream:      keeper.PtrInt(math.ZeroInt()),
			LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
			LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		})

		k.AssignInitiativeToMember(ctx, initID, assignee)
		k.SubmitInitiativeWork(ctx, initID, assignee, "uri")

		// ApproveInitiative adds an approval but does not automatically complete unless logic dictates
		// Assuming ApproveInitiative just adds approval and update status
		_, err = ms.ApproveInitiative(ctx, &types.MsgApproveInitiative{
			Creator:      approverStr,
			InitiativeId: initID,
			Approved:     true,
			Comments:     "LGTM",
		})
		require.NoError(t, err)

		initiative, err := k.GetInitiative(ctx, initID)
		require.NoError(t, err)
		// Check that approval was added
		require.Contains(t, initiative.Approvals, approverStr)
		// Status should remain SUBMITTED as conviction is not checked here or insufficient
		require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED, initiative.Status)
	})

	t.Run("assignee cannot approve own initiative", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		creator := sdk.AccAddress([]byte("creator"))
		k.Member.Set(ctx, creator.String(), types.Member{
			Address:          creator.String(),
			DreamBalance:     keeper.PtrInt(math.NewInt(10000)),
			StakedDream:      keeper.PtrInt(math.ZeroInt()),
			LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
			LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		})

		projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
		k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))

		initID, _ := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(100))

		assignee := sdk.AccAddress([]byte("assignee"))
		assigneeStr, err := f.addressCodec.BytesToString(assignee)
		require.NoError(t, err)
		k.Member.Set(ctx, assigneeStr, types.Member{
			Address:          assigneeStr,
			DreamBalance:     keeper.PtrInt(math.ZeroInt()),
			StakedDream:      keeper.PtrInt(math.ZeroInt()),
			LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
			LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		})

		k.AssignInitiativeToMember(ctx, initID, assignee)
		k.SubmitInitiativeWork(ctx, initID, assignee, "uri")

		// Assignee tries to approve their own submission — rejected even
		// though the default fixture treats everyone as an ops member.
		_, err = ms.ApproveInitiative(ctx, &types.MsgApproveInitiative{
			Creator:      assigneeStr,
			InitiativeId: initID,
			Approved:     true,
			Comments:     "looks great (it's mine)",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot approve their own initiative")
	})

	t.Run("project creator cannot approve own initiative", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		creator := sdk.AccAddress([]byte("creator"))
		creatorStr, err := f.addressCodec.BytesToString(creator)
		require.NoError(t, err)
		k.Member.Set(ctx, creator.String(), types.Member{
			Address:          creator.String(),
			DreamBalance:     keeper.PtrInt(math.NewInt(10000)),
			StakedDream:      keeper.PtrInt(math.ZeroInt()),
			LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
			LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		})

		projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
		k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))

		initID, _ := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(100))

		assignee := sdk.AccAddress([]byte("assignee"))
		assigneeStr, err := f.addressCodec.BytesToString(assignee)
		require.NoError(t, err)
		k.Member.Set(ctx, assigneeStr, types.Member{
			Address:          assigneeStr,
			DreamBalance:     keeper.PtrInt(math.ZeroInt()),
			StakedDream:      keeper.PtrInt(math.ZeroInt()),
			LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
			LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		})

		k.AssignInitiativeToMember(ctx, initID, assignee)
		k.SubmitInitiativeWork(ctx, initID, assignee, "uri")

		// Project creator tries to approve work on their own project — rejected.
		_, err = ms.ApproveInitiative(ctx, &types.MsgApproveInitiative{
			Creator:      creatorStr,
			InitiativeId: initID,
			Approved:     true,
			Comments:     "approving my own project's work",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot approve their own initiative")
	})
}

// setupApprovableInitiative builds a SUBMITTED initiative and returns its id
// alongside an approver who clears the handler's authorization check (the
// fixture's commons keeper reports committee membership). The approver is
// neither the assignee nor the project creator, so the conflict-of-interest
// exclusion does not apply.
func setupApprovableInitiative(t *testing.T, f *fixture) (uint64, string) {
	t.Helper()
	k, ctx := f.keeper, f.ctx

	creator := sdk.AccAddress([]byte("idem-creator______"))
	k.Member.Set(ctx, creator.String(), types.Member{
		Address:          creator.String(),
		DreamBalance:     keeper.PtrInt(math.NewInt(10000)),
		StakedDream:      keeper.PtrInt(math.ZeroInt()),
		LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
		LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"tag": "100.0"},
	})
	projectID, err := k.CreateProject(ctx, creator, "P", "D", []string{"tag"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.NewInt(10000), math.NewInt(0), false)
	require.NoError(t, err)
	require.NoError(t, k.ApproveProject(ctx, projectID, creator, math.NewInt(10000), math.NewInt(0)))

	initID, err := k.CreateInitiative(ctx, creator, projectID, "T", "D", []string{"tag"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(100))
	require.NoError(t, err)

	assignee := sdk.AccAddress([]byte("idem-assignee_____"))
	assigneeStr, err := f.addressCodec.BytesToString(assignee)
	require.NoError(t, err)
	k.Member.Set(ctx, assigneeStr, types.Member{
		Address:          assigneeStr,
		DreamBalance:     keeper.PtrInt(math.ZeroInt()),
		StakedDream:      keeper.PtrInt(math.ZeroInt()),
		LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
		LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"tag": "100.0"},
	})
	require.NoError(t, k.AssignInitiativeToMember(ctx, initID, assignee))
	require.NoError(t, k.SubmitInitiativeWork(ctx, initID, assignee, "uri"))

	approverStr, err := f.addressCodec.BytesToString(sdk.AccAddress([]byte("idem-approver_____")))
	require.NoError(t, err)
	return initID, approverStr
}

// TestApproveInitiativeIsIdempotent covers the re-approval path: signing the
// same approval twice must leave one entry, not two. The approvals list is
// on-chain state, so an appending duplicate is both unbounded growth a single
// staker controls and a miscount for anything that later tallies the list.
func TestApproveInitiativeIsIdempotent(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	k, ctx := f.keeper, f.ctx

	initID, approverStr := setupApprovableInitiative(t, f)

	for i := 0; i < 3; i++ {
		_, err := ms.ApproveInitiative(ctx, &types.MsgApproveInitiative{
			Creator:      approverStr,
			InitiativeId: initID,
			Approved:     true,
		})
		require.NoError(t, err, "re-approving should not error")
	}

	initiative, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	require.Len(t, initiative.Approvals, 1, "the same approver should appear once")
	require.Equal(t, approverStr, initiative.Approvals[0])
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED, initiative.Status)
}

// stakerDisapprovalFixture builds a SUBMITTED initiative backed by two stakers
// holding the given DREAM amounts, on a chain where nobody sits on the
// Operations Committee. That isolates the stake-weighted path: every caller
// here has staker standing and nothing else.
// The stake-weighted staker veto is retired. Stakers are paid on completion, so
// the veto was held by exactly the people who lost money using it — and backing
// a proposal is a different judgement from evaluating a deliverable, made
// earlier and often without the expertise. Quality is the bonded reviewer's
// question now; conviction is the stakers'.
//
// They keep a real exit: conviction is recomputed from live stake records and
// completion needs both thresholds, so withdrawing a stake blocks completion
// within about one refresh interval.
func TestStakerDisapprovalIsRejected(t *testing.T) {
	// Authorize only the project creator, who is the address setup approves the
	// project as. NeverAuthorized would be too blunt — it also blocks
	// ApproveProject during setup, and the test would die before its assertion.
	creator := sdk.AccAddress([]byte("idem-creator______"))
	f := initFixture(t, WithAuthorizationPolicy(AuthorizeAddresses(creator)))
	ms := keeper.NewMsgServerImpl(f.keeper)
	initID, approverStr := setupApprovableInitiative(t, f)

	_, err := ms.ApproveInitiative(f.ctx, &types.MsgApproveInitiative{
		Creator:      approverStr,
		InitiativeId: initID,
		Approved:     false,
		Comments:     "not good enough",
	})
	require.Error(t, err, "a non-committee disapproval must be refused outright")
	require.ErrorIs(t, err, types.ErrUnauthorized)

	initiative, gErr := f.keeper.GetInitiative(f.ctx, initID)
	require.NoError(t, gErr)
	require.NotEqual(t, types.InitiativeStatus_INITIATIVE_STATUS_ABANDONED, initiative.Status,
		"a refused disapproval must not touch the initiative")
}

// TestOpsCommitteeDisapprovalIsImmediate pins the other half of the split: the
// committee does not need a tally.
func TestOpsCommitteeDisapprovalIsImmediate(t *testing.T) {
	f := initFixture(t) // AlwaysAuthorized: the caller reads as committee
	ms := keeper.NewMsgServerImpl(f.keeper)
	initID, approverStr := setupApprovableInitiative(t, f)

	_, err := ms.ApproveInitiative(f.ctx, &types.MsgApproveInitiative{
		Creator:      approverStr,
		InitiativeId: initID,
		Approved:     false,
		Comments:     "out of scope",
	})
	require.NoError(t, err)

	initiative, err := f.keeper.GetInitiative(f.ctx, initID)
	require.NoError(t, err)
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_ABANDONED, initiative.Status)
}
