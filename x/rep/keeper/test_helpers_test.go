package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// Test constants to replace magic numbers throughout tests
const (
	// Common amounts
	TestDreamAmount     = int64(1000)
	TestBudgetAmount    = int64(10000)
	TestRewardAmount    = int64(100)
	TestStakeAmount     = int64(500)
	TestLargeAmount     = int64(100000)
	TestSmallAmount     = int64(10)
	TestMinimumStake    = int64(100)
	TestTipAmount       = int64(50)
	TestGiftAmount      = int64(200)

	// Common strings
	TestTagBackend      = "backend"
	TestTagFrontend     = "frontend"
	TestTagDesign       = "design"
	TestProjectName     = "Test Project"
	TestProjectDesc     = "Test project description"
	TestInitiativeName  = "Test Initiative"
	TestInitiativeDesc  = "Test initiative description"
	TestCouncilTech     = "technical"
	TestCouncilEco      = "ecosystem"

	// Common reputation values
	TestReputationHigh   = "500.0"
	TestReputationMid    = "250.0"
	TestReputationLow    = "50.0"
	TestReputationZero   = "0.0"

	// Trust levels - use the actual enum values
	TrustLevelNew         = types.TrustLevel_TRUST_LEVEL_NEW
	TrustLevelProvisional = types.TrustLevel_TRUST_LEVEL_PROVISIONAL
	TrustLevelEstablished = types.TrustLevel_TRUST_LEVEL_ESTABLISHED
	TrustLevelTrusted     = types.TrustLevel_TRUST_LEVEL_TRUSTED
	TrustLevelCore        = types.TrustLevel_TRUST_LEVEL_CORE
)

// TestAddresses provides consistent test addresses
var (
	TestAddrCreator  = sdk.AccAddress([]byte("creator_________"))
	TestAddrAssignee = sdk.AccAddress([]byte("assignee________"))
	TestAddrStaker   = sdk.AccAddress([]byte("staker__________"))
	TestAddrInviter  = sdk.AccAddress([]byte("inviter_________"))
	TestAddrInvitee  = sdk.AccAddress([]byte("invitee_________"))
	TestAddrApprover = sdk.AccAddress([]byte("approver________"))
	TestAddrCouncil  = sdk.AccAddress([]byte("council_________"))
	TestAddrMember1  = sdk.AccAddress([]byte("member1_________"))
	TestAddrMember2  = sdk.AccAddress([]byte("member2_________"))
	TestAddrMember3  = sdk.AccAddress([]byte("member3_________"))
)

// MemberSetupConfig provides flexible configuration for member creation
type MemberSetupConfig struct {
	Address          sdk.AccAddress
	DreamBalance     int64
	StakedDream      int64
	TrustLevel       types.TrustLevel
	ReputationScores map[string]string
	LifetimeEarned   int64
	LifetimeBurned   int64
}

// DefaultMemberConfig returns a basic member configuration
func DefaultMemberConfig(addr sdk.AccAddress) MemberSetupConfig {
	return MemberSetupConfig{
		Address:          addr,
		DreamBalance:     TestDreamAmount,
		StakedDream:      0,
		TrustLevel:       TrustLevelProvisional,
		ReputationScores: map[string]string{TestTagBackend: TestReputationMid},
		LifetimeEarned:   0,
		LifetimeBurned:   0,
	}
}

// SetupMember creates a member with the given configuration
func SetupMember(t *testing.T, k *keeper.Keeper, ctx sdk.Context, cfg MemberSetupConfig) {
	t.Helper()

	member := types.Member{
		Address:          cfg.Address.String(),
		DreamBalance:     PtrInt(math.NewInt(cfg.DreamBalance)),
		StakedDream:      PtrInt(math.NewInt(cfg.StakedDream)),
		TrustLevel:       cfg.TrustLevel,
		LifetimeEarned:   PtrInt(math.NewInt(cfg.LifetimeEarned)),
		LifetimeBurned:   PtrInt(math.NewInt(cfg.LifetimeBurned)),
		ReputationScores: cfg.ReputationScores,
	}

	err := k.Member.Set(ctx, cfg.Address.String(), member)
	require.NoError(t, err, "failed to setup member")
}

// SetupBasicMember creates a member with default configuration
func SetupBasicMember(t *testing.T, k *keeper.Keeper, ctx sdk.Context, addr sdk.AccAddress) {
	t.Helper()
	SetupMember(t, k, ctx, DefaultMemberConfig(addr))
}

// SetupMemberWithReputation creates a member with specific reputation
func SetupMemberWithReputation(t *testing.T, k *keeper.Keeper, ctx sdk.Context, addr sdk.AccAddress, tag string, reputation string) {
	t.Helper()
	cfg := DefaultMemberConfig(addr)
	cfg.ReputationScores = map[string]string{tag: reputation}
	SetupMember(t, k, ctx, cfg)
}

// SetupMemberWithDream creates a member with specific DREAM balance
func SetupMemberWithDream(t *testing.T, k *keeper.Keeper, ctx sdk.Context, addr sdk.AccAddress, dreamAmount int64) {
	t.Helper()
	cfg := DefaultMemberConfig(addr)
	cfg.DreamBalance = dreamAmount
	SetupMember(t, k, ctx, cfg)
}

// ProjectSetupConfig provides flexible configuration for project creation
type ProjectSetupConfig struct {
	Creator     sdk.AccAddress
	Name        string
	Description string
	Tags        []string
	Category    types.ProjectCategory
	Council     string
	Budget      int64
	MaxPerInit  int64
	ShouldApprove bool
	Approver    sdk.AccAddress
}

// DefaultProjectConfig returns a basic project configuration
func DefaultProjectConfig(creator sdk.AccAddress) ProjectSetupConfig {
	return ProjectSetupConfig{
		Creator:       creator,
		Name:          TestProjectName,
		Description:   TestProjectDesc,
		Tags:          []string{TestTagBackend},
		Category:      types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE,
		Council:       TestCouncilTech,
		Budget:        TestBudgetAmount,
		MaxPerInit:    TestRewardAmount,
		ShouldApprove: true,
		Approver:      TestAddrApprover,
	}
}

// SetupProject creates and optionally approves a project
func SetupProject(t *testing.T, k *keeper.Keeper, ctx sdk.Context, cfg ProjectSetupConfig) uint64 {
	t.Helper()

	projectID, err := k.CreateProject(
		ctx,
		cfg.Creator,
		cfg.Name,
		cfg.Description,
		cfg.Tags,
		cfg.Category,
		cfg.Council,
		math.NewInt(cfg.Budget),
		math.NewInt(cfg.MaxPerInit),
	)
	require.NoError(t, err, "failed to create project")
	require.NotZero(t, projectID, "project ID should not be zero")

	if cfg.ShouldApprove {
		err = k.ApproveProject(ctx, projectID, cfg.Approver, math.NewInt(cfg.Budget), math.NewInt(cfg.MaxPerInit))
		require.NoError(t, err, "failed to approve project")
	}

	return projectID
}

// SetupBasicProject creates and approves a basic project
func SetupBasicProject(t *testing.T, k *keeper.Keeper, ctx sdk.Context, creator sdk.AccAddress) uint64 {
	t.Helper()
	return SetupProject(t, k, ctx, DefaultProjectConfig(creator))
}

// InitiativeSetupConfig provides flexible configuration for initiative creation
type InitiativeSetupConfig struct {
	Creator      sdk.AccAddress
	ProjectID    uint64
	Name         string
	Description  string
	Tags         []string
	Tier         types.InitiativeTier
	Category     types.InitiativeCategory
	ParentID     string
	RewardAmount int64
	ShouldAssign bool
	Assignee     sdk.AccAddress
}

// DefaultInitiativeConfig returns a basic initiative configuration
func DefaultInitiativeConfig(creator sdk.AccAddress, projectID uint64) InitiativeSetupConfig {
	return InitiativeSetupConfig{
		Creator:      creator,
		ProjectID:    projectID,
		Name:         TestInitiativeName,
		Description:  TestInitiativeDesc,
		Tags:         []string{TestTagBackend},
		Tier:         types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		Category:     types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
		ParentID:     "",
		RewardAmount: TestRewardAmount,
		ShouldAssign: false,
		Assignee:     nil,
	}
}

// SetupInitiative creates and optionally assigns an initiative
func SetupInitiative(t *testing.T, k *keeper.Keeper, ctx sdk.Context, cfg InitiativeSetupConfig) uint64 {
	t.Helper()

	initID, err := k.CreateInitiative(
		ctx,
		cfg.Creator,
		cfg.ProjectID,
		cfg.Name,
		cfg.Description,
		cfg.Tags,
		cfg.Tier,
		cfg.Category,
		cfg.ParentID,
		math.NewInt(cfg.RewardAmount),
	)
	require.NoError(t, err, "failed to create initiative")
	require.NotZero(t, initID, "initiative ID should not be zero")

	if cfg.ShouldAssign {
		require.NotNil(t, cfg.Assignee, "assignee must be provided when ShouldAssign is true")
		err = k.AssignInitiativeToMember(ctx, initID, cfg.Assignee)
		require.NoError(t, err, "failed to assign initiative")
	}

	return initID
}

// SetupBasicInitiative creates a basic initiative
func SetupBasicInitiative(t *testing.T, k *keeper.Keeper, ctx sdk.Context, creator sdk.AccAddress, projectID uint64) uint64 {
	t.Helper()
	return SetupInitiative(t, k, ctx, DefaultInitiativeConfig(creator, projectID))
}

// SetupProjectWithInitiative creates a complete project with an initiative
func SetupProjectWithInitiative(t *testing.T, k *keeper.Keeper, ctx sdk.Context, creator sdk.AccAddress) (projectID uint64, initiativeID uint64) {
	t.Helper()

	projectID = SetupBasicProject(t, k, ctx, creator)
	initiativeID = SetupBasicInitiative(t, k, ctx, creator, projectID)

	return projectID, initiativeID
}

// SetupCompleteWorkflow sets up member, project, initiative, and stake for testing
func SetupCompleteWorkflow(t *testing.T, k *keeper.Keeper, ctx sdk.Context) (
	creator sdk.AccAddress,
	assignee sdk.AccAddress,
	staker sdk.AccAddress,
	projectID uint64,
	initiativeID uint64,
	stakeID uint64,
) {
	t.Helper()

	creator = TestAddrCreator
	assignee = TestAddrAssignee
	staker = TestAddrStaker

	// Setup all members
	SetupBasicMember(t, k, ctx, creator)
	SetupMemberWithReputation(t, k, ctx, assignee, TestTagBackend, TestReputationHigh)
	SetupMemberWithDream(t, k, ctx, staker, TestStakeAmount*10)

	// Create and approve project
	projectID = SetupBasicProject(t, k, ctx, creator)

	// Create and assign initiative
	initCfg := DefaultInitiativeConfig(creator, projectID)
	initCfg.ShouldAssign = true
	initCfg.Assignee = assignee
	initiativeID = SetupInitiative(t, k, ctx, initCfg)

	// Create stake
	stakeID, err := k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initiativeID, "", math.NewInt(TestStakeAmount))
	require.NoError(t, err, "failed to create stake")
	require.NotZero(t, stakeID, "stake ID should not be zero")

	return creator, assignee, staker, projectID, initiativeID, stakeID
}

// AssertMemberDreamBalance checks a member's DREAM balance
func AssertMemberDreamBalance(t *testing.T, k *keeper.Keeper, ctx sdk.Context, addr sdk.AccAddress, expectedAmount int64) {
	t.Helper()

	member, err := k.Member.Get(ctx, addr.String())
	require.NoError(t, err, "failed to get member")
	require.NotNil(t, member.DreamBalance, "DREAM balance should not be nil")
	require.Equal(t, expectedAmount, member.DreamBalance.Int64(),
		"DREAM balance mismatch for %s: expected %d, got %d",
		addr.String(), expectedAmount, member.DreamBalance.Int64())
}

// AssertMemberStakedDream checks a member's staked DREAM amount
func AssertMemberStakedDream(t *testing.T, k *keeper.Keeper, ctx sdk.Context, addr sdk.AccAddress, expectedAmount int64) {
	t.Helper()

	member, err := k.Member.Get(ctx, addr.String())
	require.NoError(t, err, "failed to get member")
	require.NotNil(t, member.StakedDream, "staked DREAM should not be nil")
	require.Equal(t, expectedAmount, member.StakedDream.Int64(),
		"staked DREAM mismatch for %s: expected %d, got %d",
		addr.String(), expectedAmount, member.StakedDream.Int64())
}

// AssertMemberReputation checks a member's reputation for a specific tag
func AssertMemberReputation(t *testing.T, k *keeper.Keeper, ctx sdk.Context, addr sdk.AccAddress, tag string, expectedRep string) {
	t.Helper()

	member, err := k.Member.Get(ctx, addr.String())
	require.NoError(t, err, "failed to get member")
	require.NotNil(t, member.ReputationScores, "reputation scores should not be nil")

	actualRep, exists := member.ReputationScores[tag]
	require.True(t, exists, "reputation for tag %s should exist", tag)
	require.Equal(t, expectedRep, actualRep,
		"reputation mismatch for %s/%s: expected %s, got %s",
		addr.String(), tag, expectedRep, actualRep)
}

// AssertProjectStatus checks a project's status
func AssertProjectStatus(t *testing.T, k *keeper.Keeper, ctx sdk.Context, projectID uint64, expectedStatus types.ProjectStatus) {
	t.Helper()

	project, err := k.Project.Get(ctx, projectID)
	require.NoError(t, err, "failed to get project")
	require.Equal(t, expectedStatus, project.Status,
		"project status mismatch: expected %v, got %v",
		expectedStatus, project.Status)
}

// AssertInitiativeStatus checks an initiative's status
func AssertInitiativeStatus(t *testing.T, k *keeper.Keeper, ctx sdk.Context, initiativeID uint64, expectedStatus types.InitiativeStatus) {
	t.Helper()

	initiative, err := k.Initiative.Get(ctx, initiativeID)
	require.NoError(t, err, "failed to get initiative")
	require.Equal(t, expectedStatus, initiative.Status,
		"initiative status mismatch: expected %v, got %v",
		expectedStatus, initiative.Status)
}

// AssertStakeExists checks that a stake exists
func AssertStakeExists(t *testing.T, k *keeper.Keeper, ctx sdk.Context, stakeID uint64) {
	t.Helper()

	stake, err := k.Stake.Get(ctx, stakeID)
	require.NoError(t, err, "stake should exist")
	require.NotNil(t, stake, "stake should not be nil")
}

// AssertStakeNotExists checks that a stake does not exist
func AssertStakeNotExists(t *testing.T, k *keeper.Keeper, ctx sdk.Context, stakeID uint64) {
	t.Helper()

	_, err := k.Stake.Get(ctx, stakeID)
	require.Error(t, err, "stake should not exist")
}

// AdvanceBlockHeight advances the block height by the given number of blocks
func AdvanceBlockHeight(ctx sdk.Context, blocks int64) sdk.Context {
	newHeight := ctx.BlockHeight() + blocks
	return ctx.WithBlockHeight(newHeight)
}

// AdvanceEpochs advances the block height by the given number of epochs
func AdvanceEpochs(ctx sdk.Context, params types.Params, epochs int64) sdk.Context {
	blocks := epochs * int64(params.EpochBlocks)
	return AdvanceBlockHeight(ctx, blocks)
}
