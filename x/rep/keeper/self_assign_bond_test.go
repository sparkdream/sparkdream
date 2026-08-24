package keeper_test

import (
	"testing"
	"time"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// The self-assignment safeguards used to key off `assignee == project.Creator`.
// Nothing requires an initiative's author to own the project it sits under —
// CreateInitiative takes any active project, and AssignInitiative authorises
// every self-assignment — so authoring an initiative under somebody else's
// project and taking it yourself cleared the bond, the raised external
// conviction floor and the extended challenge window in one move. These tests
// pin the author-based predicate and the heavier rate the minting case carries.

// mkFundedMember creates an ESTABLISHED member holding `balance` DREAM.
func mkFundedMember(t *testing.T, k keeper.Keeper, ctx sdk.Context, addr sdk.AccAddress, balance int64) {
	t.Helper()
	require.NoError(t, k.Member.Set(ctx, addr.String(), types.Member{
		Address:          addr.String(),
		DreamBalance:     PtrInt(math.NewInt(balance)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"tag": "100.0"},
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
	}))
}

func TestPermissionlessSelfAssignBondsTheMint(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// A permissionless initiative's budget is not a reservation against an
	// approved pool — it is an instruction to mint. The exemption that used to
	// live here put the bond on the case backed by a council and removed it
	// from the case backed by nobody.
	creator := sdk.AccAddress([]byte("perm-creator-addr"))
	mkFundedMember(t, k, ctx, creator, 10_000_000)

	projectID, err := k.CreateProject(ctx, creator, "Perm", "Desc", []string{"tag"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.ZeroInt(), math.ZeroInt(), true)
	require.NoError(t, err)

	initID, err := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"tag"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(1000))
	require.NoError(t, err)

	require.NoError(t, k.AssignInitiativeToMember(ctx, initID, creator))

	initiative, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	// 25% (PermissionlessSelfAssignedBondRate), not the 10% budget-backed rate
	// and not the zero it used to be.
	require.Equal(t, "250", keeper.DerefInt(initiative.SelfAssignBond).String())
}

func TestSelfAssignBondAppliesToInitiativeAuthor(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Budget-backed project owned by someone else; the author self-assigns.
	owner := sdk.AccAddress([]byte("bb-owner-address-"))
	mkFundedMember(t, k, ctx, owner, 10_000_000)
	projectID, err := k.CreateProject(ctx, owner, "Proj", "Desc", []string{"tag"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.NewInt(10000), math.NewInt(1000), false)
	require.NoError(t, err)
	require.NoError(t, k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000)))

	author := sdk.AccAddress([]byte("bb-author-address"))
	mkFundedMember(t, k, ctx, author, 10_000_000)
	initID, err := k.CreateInitiative(ctx, author, projectID, "Task", "D", []string{"tag"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(1000))
	require.NoError(t, err)

	require.NoError(t, k.AssignInitiativeToMember(ctx, initID, author))

	initiative, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	// Budget-backed rate, because the DREAM here was already approved — but a
	// bond nonetheless, which the project-creator-only predicate skipped.
	require.Equal(t, "100", keeper.DerefInt(initiative.SelfAssignBond).String())
}

func TestPermissionlessAuthorSelfAssignUnderForeignProject(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// The full shape of the hole: the attacker never needs to own the project.
	owner := sdk.AccAddress([]byte("fp-owner-address-"))
	mkFundedMember(t, k, ctx, owner, 10_000_000)
	projectID, err := k.CreateProject(ctx, owner, "Open", "Desc", []string{"tag"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.ZeroInt(), math.ZeroInt(), true)
	require.NoError(t, err)

	author := sdk.AccAddress([]byte("fp-author-address"))
	mkFundedMember(t, k, ctx, author, 10_000_000)
	initID, err := k.CreateInitiative(ctx, author, projectID, "Task", "D", []string{"tag"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(1000))
	require.NoError(t, err)

	require.NoError(t, k.AssignInitiativeToMember(ctx, initID, author))

	initiative, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	require.Equal(t, "250", keeper.DerefInt(initiative.SelfAssignBond).String(),
		"author self-assigning a mint pays the permissionless rate regardless of who owns the project")
}

func TestSelfAssignBondStillSkippedForArmsLengthAssignee(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// The safeguards must not fire on ordinary externally-assigned work: an
	// assignee who neither authored the initiative nor owns the project has a
	// counterparty, which is the whole point of the bond.
	owner := sdk.AccAddress([]byte("al-owner-address-"))
	mkFundedMember(t, k, ctx, owner, 10_000_000)
	projectID, err := k.CreateProject(ctx, owner, "Open", "Desc", []string{"tag"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.ZeroInt(), math.ZeroInt(), true)
	require.NoError(t, err)

	initID, err := k.CreateInitiative(ctx, owner, projectID, "Task", "D", []string{"tag"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(1000))
	require.NoError(t, err)

	assignee := sdk.AccAddress([]byte("al-assignee-addr-"))
	mkFundedMember(t, k, ctx, assignee, 0)

	require.NoError(t, k.AssignInitiativeToMember(ctx, initID, assignee),
		"a third-party assignee posts no bond, so a zero balance is fine")

	initiative, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	require.True(t, initiative.SelfAssignBond == nil || initiative.SelfAssignBond.IsZero())
}

func TestSelfAssignedChallengeWindowAppliesToAuthor(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	normalWindow := params.DefaultChallengePeriodEpochs * params.EpochBlocks

	// Project owned by someone else, initiative authored and taken by `author`.
	// The extended contest window is one of the few brakes left on this
	// quadrant, and it keyed off the same project-creator predicate as the bond.
	owner := sdk.AccAddress([]byte("cw-owner-address-"))
	mkFundedMember(t, k, ctx, owner, 10_000_000)
	projectID, err := k.CreateProject(ctx, owner, "Open", "Desc", []string{"tag"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.ZeroInt(), math.ZeroInt(), true)
	require.NoError(t, err)

	author := sdk.AccAddress([]byte("cw-author-address"))
	mkFundedMember(t, k, ctx, author, 10_000_000)
	initID, err := k.CreateInitiative(ctx, author, projectID, "Task", "D", []string{"tag"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(1000))
	require.NoError(t, err)
	require.NoError(t, k.AssignInitiativeToMember(ctx, initID, author))
	require.NoError(t, k.SubmitInitiativeWork(ctx, initID, author, "ipfs://work"))

	require.NoError(t, k.TransitionToChallengePeriod(ctx, initID))

	initiative, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	window := initiative.ChallengePeriodEnd - initiative.ReviewPeriodEnd
	require.Equal(t, normalWindow*params.SelfAssignedChallengeMultiplier, window,
		"self-assigned work gets the extended challenge window even when the author does not own the project")
}

func TestInitiativeAuthorDoesNotCountAsExternalConviction(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// The author routes conviction to an accomplice assignee. Excluding only
	// the assignee and the project creator left the person who wrote the
	// initiative vouching for it as a disinterested outsider.
	owner := sdk.AccAddress([]byte("ec-owner-address-"))
	mkFundedMember(t, k, ctx, owner, 10_000_000)
	projectID, err := k.CreateProject(ctx, owner, "Open", "Desc", []string{"tag"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.ZeroInt(), math.ZeroInt(), true)
	require.NoError(t, err)

	author := sdk.AccAddress([]byte("ec-author-address"))
	mkFundedMember(t, k, ctx, author, 10_000_000)
	initID, err := k.CreateInitiative(ctx, author, projectID, "Task", "D", []string{"tag"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(1000))
	require.NoError(t, err)

	accomplice := sdk.AccAddress([]byte("ec-accomplice-adr"))
	mkFundedMember(t, k, ctx, accomplice, 10_000_000)
	require.NoError(t, k.AssignInitiativeToMember(ctx, initID, accomplice))

	_, err = k.CreateStake(ctx, author,
		types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(5000))
	require.NoError(t, err)

	// Let conviction accrue, then recompute.
	sdkCtx := ctx.WithBlockTime(ctx.BlockTime().Add(30 * 24 * time.Hour)).WithBlockHeight(ctx.BlockHeight() + 1000)
	require.NoError(t, k.UpdateInitiativeConvictionLazy(sdkCtx, initID))

	initiative, err := k.GetInitiative(sdkCtx, initID)
	require.NoError(t, err)
	require.True(t, DerefDec(initiative.CurrentConviction).IsPositive(),
		"the author's stake still builds total conviction")
	require.True(t, DerefDec(initiative.ExternalConviction).IsZero(),
		"but none of it counts as external — the author is an insider")
}

func TestPermissionlessSelfAssignBondIsReleasable(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Permissionless self-assignment now locks DREAM where it previously
	// locked none, so every exit path has to give it back. Abandonment is the
	// path an assignee controls unilaterally; if it stranded the bond, the new
	// safeguard would be a way to burn your own DREAM by accident.
	creator := sdk.AccAddress([]byte("rel-creator-addr-"))
	mkFundedMember(t, k, ctx, creator, 10_000_000)

	projectID, err := k.CreateProject(ctx, creator, "Perm", "Desc", []string{"tag"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.ZeroInt(), math.ZeroInt(), true)
	require.NoError(t, err)

	initID, err := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"tag"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(1000))
	require.NoError(t, err)
	require.NoError(t, k.AssignInitiativeToMember(ctx, initID, creator))

	// 250 self-assign bond (25% of the 1000 budget), locked rather than spent.
	// No review bounty: 1000 micro-DREAM is far below
	// review_required_above_budget, so no review is mandatory here and nothing
	// is charged for one.
	staked, err := k.GetMember(ctx, creator)
	require.NoError(t, err)
	require.Equal(t, "250", staked.StakedDream.String(), "bond is locked, not spent")
	require.True(t, k.GetReviewBounty(ctx, initID).Amount.IsZero(),
		"ungated work carries no minimum bounty")

	require.NoError(t, k.AbandonInitiative(ctx, initID, creator, "changed my mind"))

	after, err := k.GetMember(ctx, creator)
	require.NoError(t, err)
	require.True(t, after.StakedDream.IsZero(),
		"bond unlocks on abandonment")

	initiative, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	require.True(t, keeper.DerefInt(initiative.SelfAssignBond).IsZero(), "bond cleared from the record")
}
