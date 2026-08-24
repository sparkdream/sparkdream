package keeper_test

import (
	"fmt"
	stdmath "math"
	"strings"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	repkeeper "sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestCreateInitiative(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Setup: Create a project
	creator := sdk.AccAddress([]byte("creator"))
	projectID, err := k.CreateProject(
		ctx,
		creator,
		"Test Project",
		"Description",
		[]string{"backend", "frontend"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE,
		"technical",
		math.NewInt(10000),
		math.NewInt(1000),
		false,
	)
	require.NoError(t, err)

	// Approve project
	approver := sdk.AccAddress([]byte("approver"))
	err = k.ApproveProject(ctx, projectID, approver, math.NewInt(10000), math.NewInt(1000))
	require.NoError(t, err)

	// Create member
	k.Member.Set(ctx, creator.String(), types.Member{
		Address:          creator.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": "50.0", "frontend": "30.0"},
	})

	// Test: Create initiative
	budget := math.NewInt(500)
	initID, err := k.CreateInitiative(
		ctx,
		creator,
		projectID,
		"Build API endpoint",
		"Implement REST API for user management",
		[]string{"backend"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
		budget,
	)
	require.NoError(t, err)

	// Verify initiative
	initiative, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	require.Equal(t, projectID, initiative.ProjectId)
	require.Equal(t, "Build API endpoint", initiative.Title)
	require.Equal(t, []string{"backend"}, initiative.Tags)
	require.Equal(t, types.InitiativeTier_INITIATIVE_TIER_STANDARD, initiative.Tier)
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_OPEN, initiative.Status)

	// Verify required conviction calculation
	// Formula: required_conviction = conviction_per_dream × sqrt(budget)
	params, _ := k.Params.Get(ctx)
	sqrtBudget := math.LegacyMustNewDecFromStr(fmt.Sprintf("%.18f", stdmath.Sqrt(float64(budget.Uint64()))))
	expectedConviction := params.ConvictionPerDream.Mul(sqrtBudget)
	require.Equal(t, expectedConviction.String(), initiative.RequiredConviction.String())

	// Verify budget was allocated from project
	project, err := k.GetProject(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, budget.String(), project.AllocatedBudget.String())
}

func TestAssignInitiative(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Setup
	creator := sdk.AccAddress([]byte("creator"))
	projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
	k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))

	initID, _ := k.CreateInitiative(
		ctx,
		creator,
		projectID,
		"Task",
		"Description",
		[]string{"tag"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
		math.NewInt(100),
	)

	// Create assignee with sufficient reputation
	assignee := sdk.AccAddress([]byte("assignee"))
	k.Member.Set(ctx, assignee.String(), types.Member{
		Address:          assignee.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"tag": "100.0"},
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
	})

	// Test: Assign initiative
	err := k.AssignInitiativeToMember(ctx, initID, assignee)
	require.NoError(t, err)

	// Verify assignment
	initiative, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	require.Equal(t, assignee.String(), initiative.Assignee)
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED, initiative.Status)
}

func TestAssignInitiativeProjectCreatorCanSelfAssign(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Setup: creator owns the project AND takes the work themselves.
	// Accountability comes from the completion gate (full external
	// conviction), the extended challenge window, and the DREAM bond,
	// not from an assignment-time ban.
	creator := sdk.AccAddress([]byte("creator"))
	k.Member.Set(ctx, creator.String(), types.Member{
		Address:          creator.String(),
		DreamBalance:     PtrInt(math.NewInt(1000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"tag": "100.0"},
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
	})

	projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
	k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))

	initID, _ := k.CreateInitiative(
		ctx,
		creator,
		projectID,
		"Task",
		"Description",
		[]string{"tag"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
		math.NewInt(100),
	)

	err := k.AssignInitiativeToMember(ctx, initID, creator)
	require.NoError(t, err)

	initiative, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	require.Equal(t, creator.String(), initiative.Assignee)
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED, initiative.Status)

	// Bond locked: default SelfAssignedBondRate 10% of the 100 DREAM budget
	require.Equal(t, "10", initiative.SelfAssignBond.String())
	member, err := k.GetMember(ctx, creator)
	require.NoError(t, err)
	require.Equal(t, "10", member.StakedDream.String())
	require.Equal(t, "1000", member.DreamBalance.String(), "lock must not reduce total balance")
}

func TestSelfAssignBondInsufficientBalanceRejected(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	creator := sdk.AccAddress([]byte("creator"))
	k.Member.Set(ctx, creator.String(), types.Member{
		Address:          creator.String(),
		DreamBalance:     PtrInt(math.ZeroInt()), // cannot cover the 10 DREAM bond
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"tag": "100.0"},
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
	})

	projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
	k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))

	initID, _ := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(100))

	err := k.AssignInitiativeToMember(ctx, initID, creator)
	require.Error(t, err)
	require.Contains(t, err.Error(), "self-assign bond")
}

func TestSelfAssignBondNotLockedForNonCreator(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	creator := sdk.AccAddress([]byte("creator"))
	projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
	k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))

	initID, _ := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(100))

	assignee := sdk.AccAddress([]byte("assignee"))
	k.Member.Set(ctx, assignee.String(), types.Member{
		Address:          assignee.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"tag": "100.0"},
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
	})

	// Non-creator assignee needs no bond even with zero DREAM
	err := k.AssignInitiativeToMember(ctx, initID, assignee)
	require.NoError(t, err)

	initiative, _ := k.GetInitiative(ctx, initID)
	require.True(t, initiative.SelfAssignBond == nil || initiative.SelfAssignBond.IsZero())
}

func TestSelfAssignBondReleasedOnAbandon(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	creator := sdk.AccAddress([]byte("creator"))
	k.Member.Set(ctx, creator.String(), types.Member{
		Address:          creator.String(),
		DreamBalance:     PtrInt(math.NewInt(1000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"tag": "100.0"},
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
	})

	projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
	k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))

	initID, _ := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(100))

	require.NoError(t, k.AssignInitiativeToMember(ctx, initID, creator))

	err := k.AbandonInitiative(ctx, initID, creator, "changed my mind")
	require.NoError(t, err)

	initiative, _ := k.GetInitiative(ctx, initID)
	require.True(t, initiative.SelfAssignBond.IsZero(), "bond should be cleared")
	member, err := k.GetMember(ctx, creator)
	require.NoError(t, err)
	require.Equal(t, "0", member.StakedDream.String(), "bond should be unlocked")
	require.Equal(t, "1000", member.DreamBalance.String(), "no DREAM lost on voluntary abandon")
}

func TestBurnSelfAssignBond(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	creator := sdk.AccAddress([]byte("creator"))
	k.Member.Set(ctx, creator.String(), types.Member{
		Address:          creator.String(),
		DreamBalance:     PtrInt(math.NewInt(1000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"tag": "100.0"},
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
	})

	projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
	k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))

	initID, _ := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(100))

	require.NoError(t, k.AssignInitiativeToMember(ctx, initID, creator))

	initiative, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	require.NoError(t, k.BurnSelfAssignBond(ctx, &initiative))
	require.NoError(t, k.UpdateInitiative(ctx, initiative))

	require.True(t, initiative.SelfAssignBond.IsZero(), "bond should be cleared")
	member, err := k.GetMember(ctx, creator)
	require.NoError(t, err)
	require.Equal(t, "0", member.StakedDream.String())
	require.Equal(t, "990", member.DreamBalance.String(), "10 DREAM bond burned")
	require.Equal(t, "10", member.LifetimeBurned.String())
}

func TestSubmitInitiativeWork(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Setup
	creator := sdk.AccAddress([]byte("creator"))
	projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
	k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))

	assignee := sdk.AccAddress([]byte("assignee"))
	k.Member.Set(ctx, assignee.String(), types.Member{
		Address:          assignee.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"tag": "100.0"},
	})

	initID, _ := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(100))
	k.AssignInitiativeToMember(ctx, initID, assignee)

	// Test: Submit work
	err := k.SubmitInitiativeWork(ctx, initID, assignee, "https://github.com/repo/pr/123")
	require.NoError(t, err)

	// Verify submission
	initiative, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED, initiative.Status)
	require.Equal(t, "https://github.com/repo/pr/123", initiative.DeliverableUri)
}

func TestSubmitInitiativeWorkRequiresADeliverable(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Nothing on the happy path reads the deliverable — completion turns on
	// conviction — so an empty URI accepted here rides through the review
	// window, past the challenge window and into a payout, having given
	// stakers, challengers and jurors nothing to judge. The UI requires text,
	// but that is a client-side courtesy.
	creator := sdk.AccAddress([]byte("dl-creator-addr-"))
	projectID, err := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.NewInt(10000), math.NewInt(1000), false)
	require.NoError(t, err)
	require.NoError(t, k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")),
		math.NewInt(10000), math.NewInt(1000)))

	assignee := sdk.AccAddress([]byte("dl-assignee-addr"))
	require.NoError(t, k.Member.Set(ctx, assignee.String(), types.Member{
		Address:          assignee.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"tag": "100.0"},
	}))

	initID, err := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"tag"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(100))
	require.NoError(t, err)
	require.NoError(t, k.AssignInitiativeToMember(ctx, initID, assignee))

	require.ErrorIs(t, k.SubmitInitiativeWork(ctx, initID, assignee, ""), types.ErrEmptyDeliverable)
	// Whitespace is the same nothing, spelled differently.
	require.ErrorIs(t, k.SubmitInitiativeWork(ctx, initID, assignee, "   \t\n "), types.ErrEmptyDeliverable)

	// The initiative must still be submittable afterwards: this is a rejected
	// submission, not a state transition.
	initiative, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED, initiative.Status)

	// An over-long URI is bounded too — it is state the submitter controls and
	// every reviewer reads.
	tooLong := strings.Repeat("u", types.MaxDeliverableURILength+1)
	require.Error(t, k.SubmitInitiativeWork(ctx, initID, assignee, tooLong))

	require.NoError(t, k.SubmitInitiativeWork(ctx, initID, assignee, "  https://example.test/pr/1  "))
	initiative, err = k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	require.Equal(t, "https://example.test/pr/1", initiative.DeliverableUri,
		"the stored URI is trimmed, so the emptiness check and the stored value agree")
}

func TestAbandonInitiative(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Setup
	creator := sdk.AccAddress([]byte("creator"))
	projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
	k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))

	assignee := sdk.AccAddress([]byte("assignee"))
	k.Member.Set(ctx, assignee.String(), types.Member{
		Address:          assignee.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"tag": "100.0"},
	})

	budget := math.NewInt(100)
	initID, _ := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, budget)
	k.AssignInitiativeToMember(ctx, initID, assignee)

	// Test: Abandon initiative
	err := k.AbandonInitiative(ctx, initID, assignee, "No longer needed")
	require.NoError(t, err)

	// Verify abandonment
	initiative, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_ABANDONED, initiative.Status)

	// Verify budget was returned
	project, err := k.GetProject(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, math.ZeroInt().String(), project.AllocatedBudget.String())
}

func TestCompleteInitiative(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Setup project and initiative
	creator := sdk.AccAddress([]byte("creator"))
	projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"backend"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
	k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))

	assignee := sdk.AccAddress([]byte("assignee"))
	k.Member.Set(ctx, assignee.String(), types.Member{
		Address:          assignee.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": "50.0"},
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
	})

	budget := math.NewInt(100)
	initID, _ := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"backend"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, budget)
	k.AssignInitiativeToMember(ctx, initID, assignee)
	k.SubmitInitiativeWork(ctx, initID, assignee, "deliverable")

	// Create external staker to meet external conviction requirement
	staker := sdk.AccAddress([]byte("staker"))
	k.Member.Set(ctx, staker.String(), types.Member{
		Address:          staker.String(),
		DreamBalance:     PtrInt(math.NewInt(10000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": "100.0"},
	})

	// Stake enough to meet conviction requirements
	stakeAmount := math.NewInt(1000)
	_, err := k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", stakeAmount)
	require.NoError(t, err)

	// Force update conviction (normally happens in EndBlocker)
	// We manually set conviction to ensure it passes the threshold for completion testing
	initiative, _ := k.GetInitiative(ctx, initID)
	// Required conviction for 100 DREAM budget is likely 100 (assuming 1 DREAM = 1 Conviction param)
	// We verify params first to be sure
	params, _ := k.Params.Get(ctx)
	reqConv := params.ConvictionPerDream.MulInt(budget)

	// Set conviction to > required
	currentConv := reqConv.Mul(math.LegacyNewDec(2))
	initiative.CurrentConviction = PtrDec(currentConv)

	// Set external conviction (assignee != staker if staker is new)
	// Staker created above is new, so it counts as external.
	// But we manually set it to be safe.
	initiative.ExternalConviction = PtrDec(currentConv)

	k.UpdateInitiative(ctx, initiative)

	// Test: Complete initiative
	advanceToCompletable(t, k, ctx, initID)
	err = k.CompleteInitiative(ctx, initID)
	require.NoError(t, err)
}

// newCompletableInitiativeForCap builds a completable initiative with the given
// budget. `withStake` controls whether an external staker exists, which is what
// decides whether a completion bonus is possible at all.
func newCompletableInitiativeForCap(t *testing.T, k repkeeper.Keeper, ctx sdk.Context, budget math.Int, suffix string) uint64 {
	t.Helper()
	return buildCompletableInitiative(t, k, ctx, budget, suffix, false)
}

func buildCompletableInitiative(t *testing.T, k repkeeper.Keeper, ctx sdk.Context, budget math.Int, suffix string, withStake bool) uint64 {
	t.Helper()
	mk := func(prefix, rep string, trust types.TrustLevel, bal math.Int) sdk.AccAddress {
		addr := sdk.AccAddress([]byte(prefix + suffix))
		require.NoError(t, k.Member.Set(ctx, addr.String(), types.Member{
			Address: addr.String(), DreamBalance: PtrInt(bal),
			StakedDream: PtrInt(math.ZeroInt()), LifetimeEarned: PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"backend": rep}, TrustLevel: trust,
		}))
		return addr
	}
	creator := mk("creator", "50.0", types.TrustLevel_TRUST_LEVEL_NEW, math.ZeroInt())
	assignee := mk("assignee", "100.0", types.TrustLevel_TRUST_LEVEL_ESTABLISHED, math.ZeroInt())

	projID, err := k.CreateProject(ctx, creator, "P"+suffix, "D", []string{"backend"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(100000), math.NewInt(1000), false)
	require.NoError(t, err)
	require.NoError(t, k.ApproveProject(ctx, projID, sdk.AccAddress([]byte("approver")), math.NewInt(100000), math.NewInt(1000)))
	initID, err := k.CreateInitiative(ctx, creator, projID, "T"+suffix, "D", []string{"backend"},
		types.InitiativeTier_INITIATIVE_TIER_APPRENTICE, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, budget)
	require.NoError(t, err)
	require.NoError(t, k.AssignInitiativeToMember(ctx, initID, assignee))
	require.NoError(t, k.SubmitInitiativeWork(ctx, initID, assignee, "deliverable"))

	if withStake {
		staker := mk("staker", "100.0", types.TrustLevel_TRUST_LEVEL_NEW, math.NewInt(100000))
		_, sErr := k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(10000))
		require.NoError(t, sErr)
	}

	init, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	init.CurrentConviction = PtrDec(DerefDec(init.RequiredConviction).Mul(math.LegacyNewDec(3)))
	init.ExternalConviction = PtrDec(DerefDec(init.RequiredConviction).Mul(math.LegacyNewDec(3)))
	require.NoError(t, k.UpdateInitiative(ctx, init))
	advanceToCompletable(t, k, ctx, initID)
	return initID
}

// The season cap gate has to cover every DREAM a completion will create, not
// just the completer and treasury shares. The staker bonus and the reviewers'
// fee are minted further down the same function and counted afterwards, so a
// gate that ignored them would admit a completion and then mint past the cap it
// had just checked — overrunning by up to ~15% of the last budget through the
// door.
func TestSeasonCapGateCountsStakerBonusAndReviewFees(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Cap sits between the completer+treasury mint (100) and the full projected
	// mint including the 10% staker bonus (110). A gate that counts only the
	// first pair admits this completion; one that counts everything refuses it.
	params, _ := k.Params.Get(ctx)
	params.MaxInitiativeRewardsPerSeason = math.NewInt(105)
	k.Params.Set(ctx, params)
	k.InitSeasonalPool(ctx, 1)

	initID := buildCompletableInitiative(t, k, ctx, math.NewInt(100), "_capgate", true)

	err := k.CompleteInitiative(ctx, initID)
	require.Error(t, err, "a completion whose full mint exceeds the cap must be refused")
	require.ErrorIs(t, err, types.ErrInitiativeRewardCapReached)
	require.Contains(t, err.Error(), "staker bonus",
		"the error should name the components so an operator can see why it was refused")

	// Nothing minted, so the counter is untouched.
	minted, mErr := k.GetSeasonInitiativeRewardsMinted(ctx)
	require.NoError(t, mErr)
	require.True(t, minted.IsZero())
}

// An initiative with no stakes cannot pay a bonus, so the gate must not charge
// for one -- over-projecting there would refuse completions near the cap for a
// payout that was never going to happen.
func TestSeasonCapGateDoesNotChargeForAnAbsentBonus(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	params, _ := k.Params.Get(ctx)
	params.MaxInitiativeRewardsPerSeason = math.NewInt(105)
	k.Params.Set(ctx, params)
	k.InitSeasonalPool(ctx, 1)

	initID := newCompletableInitiativeForCap(t, k, ctx, math.NewInt(100), "_nostake")
	// Same budget as the test above, but no stake was placed.
	hasStakes, err := k.InitiativeHasStakes(ctx, initID)
	require.NoError(t, err)
	require.False(t, hasStakes)

	require.NoError(t, k.CompleteInitiative(ctx, initID),
		"100 of mint under a 105 cap must be admitted when no bonus is possible")
}

func TestSeasonInitiativeRewardsCap(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Set a low per-season initiative reward cap: 150 micro-DREAM
	params, _ := k.Params.Get(ctx)
	params.MaxInitiativeRewardsPerSeason = math.NewInt(150)
	k.Params.Set(ctx, params)

	// Initialize the seasonal pool (resets counters)
	k.InitSeasonalPool(ctx, 1)

	// Helper to create a completable initiative with the given budget
	createCompletable := func(budget math.Int, suffix string) uint64 {
		creator := sdk.AccAddress([]byte("creator" + suffix))
		k.Member.Set(ctx, creator.String(), types.Member{
			Address: creator.String(), DreamBalance: PtrInt(math.ZeroInt()),
			StakedDream: PtrInt(math.ZeroInt()), LifetimeEarned: PtrInt(math.ZeroInt()),
			LifetimeBurned: PtrInt(math.ZeroInt()), ReputationScores: map[string]string{"backend": "50.0"},
		})
		assignee := sdk.AccAddress([]byte("assignee" + suffix))
		k.Member.Set(ctx, assignee.String(), types.Member{
			Address: assignee.String(), DreamBalance: PtrInt(math.ZeroInt()),
			StakedDream: PtrInt(math.ZeroInt()), LifetimeEarned: PtrInt(math.ZeroInt()),
			LifetimeBurned: PtrInt(math.ZeroInt()), ReputationScores: map[string]string{"backend": "100.0"},
			TrustLevel: types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		})
		staker := sdk.AccAddress([]byte("staker" + suffix))
		k.Member.Set(ctx, staker.String(), types.Member{
			Address: staker.String(), DreamBalance: PtrInt(math.NewInt(100000)),
			StakedDream: PtrInt(math.ZeroInt()), LifetimeEarned: PtrInt(math.ZeroInt()),
			LifetimeBurned: PtrInt(math.ZeroInt()), ReputationScores: map[string]string{"backend": "100.0"},
		})

		projID, _ := k.CreateProject(ctx, creator, "P"+suffix, "D", []string{"backend"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(100000), math.NewInt(1000), false)
		k.ApproveProject(ctx, projID, sdk.AccAddress([]byte("approver")), math.NewInt(100000), math.NewInt(1000))
		initID, _ := k.CreateInitiative(ctx, creator, projID, "T"+suffix, "D", []string{"backend"}, types.InitiativeTier_INITIATIVE_TIER_APPRENTICE, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, budget)
		k.AssignInitiativeToMember(ctx, initID, assignee)
		k.SubmitInitiativeWork(ctx, initID, assignee, "deliverable")
		k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(10000))

		// Force conviction to meet threshold
		init, _ := k.GetInitiative(ctx, initID)
		init.CurrentConviction = PtrDec(DerefDec(init.RequiredConviction).Mul(math.LegacyNewDec(3)))
		init.ExternalConviction = PtrDec(DerefDec(init.RequiredConviction).Mul(math.LegacyNewDec(3)))
		k.UpdateInitiative(ctx, init)
		advanceToCompletable(t, k, ctx, initID)
		return initID
	}

	// First initiative: 100 micro-DREAM budget splits into 90 completer (90%) +
	// 10 treasury (10%) = 100 total minted. Both halves count against the cap.
	// 100 < 150 cap → should succeed.
	initID1 := createCompletable(math.NewInt(100), "_a")
	err := k.CompleteInitiative(ctx, initID1)
	require.NoError(t, err)

	// Verify counter was tracked against the full mint (completer + treasury).
	minted, err := k.GetSeasonInitiativeRewardsMinted(ctx)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(100).String(), minted.String())

	// Second initiative: same budget → another 100 of mint.
	// 100 + 100 = 200 > 150 cap → should fail.
	initID2 := createCompletable(math.NewInt(100), "_b")
	err = k.CompleteInitiative(ctx, initID2)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInitiativeRewardCapReached)

	// Counter should still be 100 (second completion was rejected).
	minted, err = k.GetSeasonInitiativeRewardsMinted(ctx)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(100).String(), minted.String())
}
