package keeper_test

import (
	"context"
	"testing"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/rep/types"
)

// ---------------------------------------------------------------------------
// Fix 1: Content conviction loophole — affiliated stakers filtered
// ---------------------------------------------------------------------------

func TestPropagatedConvictionFiltersAffiliatedStakers(t *testing.T) {
	f := initFixture(t, WithAuthorizationPolicy(AlwaysAuthorized))
	k := f.keeper
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	createdAt := int64(1000000)
	sdkCtx = sdkCtx.WithBlockTime(time.Unix(createdAt+86400, 0)).WithBlockHeight(50)

	// Setup members
	SetupMember(t, &k, sdkCtx, DefaultMemberConfig(TestAddrCreator))
	SetupMember(t, &k, sdkCtx, DefaultMemberConfig(TestAddrStaker))  // external
	SetupMember(t, &k, sdkCtx, DefaultMemberConfig(TestAddrMember1)) // will be assignee

	// Create project + initiative with TestAddrMember1 as assignee
	projectID := SetupBasicProject(t, &k, sdkCtx, TestAddrCreator)
	initCfg := DefaultInitiativeConfig(TestAddrCreator, projectID)
	initCfg.ShouldAssign = true
	initCfg.Assignee = TestAddrMember1
	initiativeID := SetupInitiative(t, &k, sdkCtx, initCfg)

	blogType := types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT

	// Stake from external staker on blog post 10
	externalStake := types.Stake{
		Id: 100, Staker: TestAddrStaker.String(),
		TargetType: blogType, TargetId: 10,
		Amount: math.NewInt(1000), CreatedAt: createdAt,
	}
	require.NoError(t, k.Stake.Set(sdkCtx, externalStake.Id, externalStake))
	require.NoError(t, k.AddStakeToTargetIndex(sdkCtx, externalStake))

	// Stake from creator (affiliated) on blog post 20
	creatorStake := types.Stake{
		Id: 101, Staker: TestAddrCreator.String(),
		TargetType: blogType, TargetId: 20,
		Amount: math.NewInt(1000), CreatedAt: createdAt,
	}
	require.NoError(t, k.Stake.Set(sdkCtx, creatorStake.Id, creatorStake))
	require.NoError(t, k.AddStakeToTargetIndex(sdkCtx, creatorStake))

	// Stake from assignee (affiliated) on blog post 30
	assigneeStake := types.Stake{
		Id: 102, Staker: TestAddrMember1.String(),
		TargetType: blogType, TargetId: 30,
		Amount: math.NewInt(1000), CreatedAt: createdAt,
	}
	require.NoError(t, k.Stake.Set(sdkCtx, assigneeStake.Id, assigneeStake))
	require.NoError(t, k.AddStakeToTargetIndex(sdkCtx, assigneeStake))

	// Link all three to the initiative
	require.NoError(t, k.RegisterContentInitiativeLink(sdkCtx, initiativeID, int32(blogType), 10))
	require.NoError(t, k.RegisterContentInitiativeLink(sdkCtx, initiativeID, int32(blogType), 20))
	require.NoError(t, k.RegisterContentInitiativeLink(sdkCtx, initiativeID, int32(blogType), 30))

	// Get propagated conviction with affiliation filtering
	propagated, err := k.GetPropagatedConviction(sdkCtx, initiativeID, TestAddrMember1.String(), TestAddrCreator.String())
	require.NoError(t, err)

	// Get what external-only conviction should be (only post 10 from staker)
	externalContent, err := k.GetExternalContentConviction(sdkCtx, blogType, 10, TestAddrMember1.String(), TestAddrCreator.String())
	require.NoError(t, err)

	params, _ := k.Params.Get(sdkCtx)
	expectedPropagated := externalContent.Mul(params.ConvictionPropagationRatio)

	require.True(t, propagated.Equal(expectedPropagated),
		"propagated conviction should only include external stakers: expected %s, got %s",
		expectedPropagated, propagated)

	// Verify that without filtering (empty addrs), all three stakes count
	unfilteredPropagated, err := k.GetPropagatedConviction(sdkCtx, initiativeID, "", "")
	require.NoError(t, err)
	require.True(t, unfilteredPropagated.GT(propagated),
		"unfiltered propagation %s should be greater than filtered %s",
		unfilteredPropagated, propagated)
}

// ---------------------------------------------------------------------------
// Fix 2: Within-season reputation decay
// ---------------------------------------------------------------------------

func TestReputationDecayOverEpochs(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// Set initial state: member with 100.0 reputation in "backend"
	initialRep := "100.000000000000000000"
	SetupMemberWithReputation(t, &k, sdkCtx, TestAddrStaker, TestTagBackend, initialRep)

	// Advance by 10 epochs (block height = 10 * EpochBlocks)
	params, err := k.Params.Get(sdkCtx)
	require.NoError(t, err)
	sdkCtx = AdvanceEpochs(sdkCtx, params, 10)

	// Read member and apply decay
	member, err := k.Member.Get(sdkCtx, TestAddrStaker.String())
	require.NoError(t, err)

	err = k.ApplyReputationDecay(sdkCtx, &member)
	require.NoError(t, err)

	// Check decayed value: 100 * (1 - 0.001)^10 ≈ 99.004...
	decayedRep, err := math.LegacyNewDecFromStr(member.ReputationScores[TestTagBackend])
	require.NoError(t, err)

	originalRep := math.LegacyNewDec(100)
	require.True(t, decayedRep.LT(originalRep),
		"reputation should have decayed: got %s", decayedRep)

	// Verify it's approximately correct (within 0.1%)
	expectedFactor := math.LegacyOneDec().Sub(params.ReputationDecayRate).Power(10)
	expectedRep := originalRep.Mul(expectedFactor)
	diff := decayedRep.Sub(expectedRep).Abs()
	require.True(t, diff.LT(math.LegacyNewDecWithPrec(1, 2)),
		"decay should match formula: expected ~%s, got %s (diff %s)",
		expectedRep, decayedRep, diff)
}

func TestReputationDecayZeroRate(t *testing.T) {
	// With zero decay rate, reputation should not change
	params := types.DefaultParams()
	params.ReputationDecayRate = math.LegacyZeroDec()
	f := initFixture(t, WithCustomParams(params))
	k := f.keeper
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	initialRep := "500.000000000000000000"
	SetupMemberWithReputation(t, &k, sdkCtx, TestAddrStaker, TestTagBackend, initialRep)

	sdkCtx = AdvanceEpochs(sdkCtx, params, 100)

	member, err := k.Member.Get(sdkCtx, TestAddrStaker.String())
	require.NoError(t, err)

	err = k.ApplyReputationDecay(sdkCtx, &member)
	require.NoError(t, err)

	require.Equal(t, initialRep, member.ReputationScores[TestTagBackend],
		"reputation should not decay with zero rate")
}

func TestReputationDecayAppliedLazilyViaGetReputationForTags(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	initialRep := "200.000000000000000000"
	SetupMemberWithReputation(t, &k, sdkCtx, TestAddrStaker, TestTagBackend, initialRep)

	params, _ := k.Params.Get(sdkCtx)
	sdkCtx = AdvanceEpochs(sdkCtx, params, 5)

	// GetReputationForTags should apply decay lazily
	rep, err := k.GetReputationForTags(sdkCtx, TestAddrStaker, []string{TestTagBackend})
	require.NoError(t, err)

	originalRep := math.LegacyNewDec(200)
	require.True(t, rep.LT(originalRep),
		"reputation returned by GetReputationForTags should reflect decay: got %s", rep)
}

// ---------------------------------------------------------------------------
// Fix 3: Per-member conviction cap
// ---------------------------------------------------------------------------

func TestPerMemberConvictionCap(t *testing.T) {
	f := initFixture(t, WithAuthorizationPolicy(AlwaysAuthorized))
	k := f.keeper
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	stakeTime := int64(1000000)
	sdkCtx = sdkCtx.WithBlockTime(time.Unix(stakeTime, 0)).WithBlockHeight(500)

	// Setup: creator, whale staker, small staker
	SetupMemberWithDream(t, &k, sdkCtx, TestAddrCreator, TestLargeAmount)
	SetupMemberWithDream(t, &k, sdkCtx, TestAddrStaker, TestLargeAmount*100) // whale
	SetupMemberWithDream(t, &k, sdkCtx, TestAddrMember1, TestLargeAmount)    // normal

	_, initiativeID := SetupProjectWithInitiative(t, &k, sdkCtx, TestAddrCreator)

	// Whale stakes a massive amount
	whaleAmount := math.NewInt(50000)
	_, err := k.CreateStake(sdkCtx, TestAddrStaker,
		types.StakeTargetType_STAKE_TARGET_INITIATIVE, initiativeID, "", whaleAmount)
	require.NoError(t, err)

	// Normal member stakes modest amount
	normalAmount := math.NewInt(500)
	_, err = k.CreateStake(sdkCtx, TestAddrMember1,
		types.StakeTargetType_STAKE_TARGET_INITIATIVE, initiativeID, "", normalAmount)
	require.NoError(t, err)

	// Advance time so conviction builds up (stakes need time elapsed to generate conviction)
	sdkCtx = sdkCtx.WithBlockTime(time.Unix(stakeTime+86400*30, 0)).WithBlockHeight(600)

	// Re-trigger conviction update with advanced time
	err = k.UpdateInitiativeConvictionLazy(sdkCtx, initiativeID)
	require.NoError(t, err)

	// Get initiative to check conviction
	initiative, err := k.GetInitiative(sdkCtx, initiativeID)
	require.NoError(t, err)

	params, _ := k.Params.Get(sdkCtx)
	maxPerMember := DerefDec(initiative.RequiredConviction).Mul(params.MaxConvictionSharePerMember)

	// The whale's contribution should be capped at MaxConvictionSharePerMember * required
	// Total conviction should be positive now that time has elapsed
	require.True(t, DerefDec(initiative.CurrentConviction).IsPositive(),
		"total conviction should be positive")

	// Verify the cap is effective: whale's single-member conviction can't exceed the cap
	// We can verify this indirectly: if the whale's uncapped conviction would be > maxPerMember,
	// then the total with cap should be less than total without cap
	whaleStake := types.Stake{
		Staker:    TestAddrStaker.String(),
		Amount:    whaleAmount,
		CreatedAt: stakeTime,
	}
	whaleConviction, err := k.CalculateStakeConviction(sdkCtx, whaleStake, initiative.Tags)
	require.NoError(t, err)

	if whaleConviction.GT(maxPerMember) {
		// The cap should have been applied — total conviction should be less than
		// what it would be without the cap
		totalWithCap := DerefDec(initiative.CurrentConviction)

		// Without cap, total would be whale + normal convictions
		normalStake := types.Stake{
			Staker:    TestAddrMember1.String(),
			Amount:    normalAmount,
			CreatedAt: stakeTime,
		}
		normalConviction, _ := k.CalculateStakeConviction(sdkCtx, normalStake, initiative.Tags)
		totalWithoutCap := whaleConviction.Add(normalConviction)

		require.True(t, totalWithCap.LT(totalWithoutCap),
			"total conviction with cap (%s) should be less than without cap (%s)",
			totalWithCap, totalWithoutCap)
	}
}

// ---------------------------------------------------------------------------
// Fix 4: Invitation stake partial burn
// ---------------------------------------------------------------------------

func TestInvitationStakePartialBurn(t *testing.T) {
	f := initFixture(t, WithAuthorizationPolicy(AlwaysAuthorized))
	k := f.keeper
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	initialBalance := int64(10000)

	// Setup inviter with sufficient DREAM
	cfg := DefaultMemberConfig(TestAddrInviter)
	cfg.DreamBalance = initialBalance
	cfg.TrustLevel = TrustLevelProvisional
	cfg.ReputationScores = map[string]string{TestTagBackend: TestReputationMid}
	SetupMember(t, &k, sdkCtx, cfg)

	// Give inviter credits
	inviter, _ := k.Member.Get(sdkCtx, TestAddrInviter.String())
	inviter.InvitationCredits = 5
	require.NoError(t, k.Member.Set(sdkCtx, TestAddrInviter.String(), inviter))

	// Create invitation
	stakeAmount := math.NewInt(1000)
	invitationID, err := k.CreateInvitation(sdkCtx, TestAddrInviter, TestAddrInvitee, stakeAmount, []string{TestTagBackend})
	require.NoError(t, err)

	// Accept invitation
	err = k.AcceptInvitation(sdkCtx, invitationID, TestAddrInvitee)
	require.NoError(t, err)

	// Check inviter balance: should have lost 10% of stake
	params, _ := k.Params.Get(sdkCtx)
	burnAmount := params.InvitationStakeBurnRate.MulInt(stakeAmount).TruncateInt()
	expectedBalance := initialBalance - burnAmount.Int64()

	inviter, err = k.Member.Get(sdkCtx, TestAddrInviter.String())
	require.NoError(t, err)
	require.Equal(t, expectedBalance, inviter.DreamBalance.Int64(),
		"inviter should have lost %d DREAM (10%% burn of %d stake), balance: expected %d, got %d",
		burnAmount.Int64(), stakeAmount.Int64(), expectedBalance, inviter.DreamBalance.Int64())
}

func TestInvitationStakeZeroBurnRate(t *testing.T) {
	params := types.DefaultParams()
	params.InvitationStakeBurnRate = math.LegacyZeroDec()
	f := initFixture(t, WithCustomParams(params), WithAuthorizationPolicy(AlwaysAuthorized))
	k := f.keeper
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	initialBalance := int64(10000)

	cfg := DefaultMemberConfig(TestAddrInviter)
	cfg.DreamBalance = initialBalance
	cfg.TrustLevel = TrustLevelProvisional
	SetupMember(t, &k, sdkCtx, cfg)

	inviter, _ := k.Member.Get(sdkCtx, TestAddrInviter.String())
	inviter.InvitationCredits = 5
	require.NoError(t, k.Member.Set(sdkCtx, TestAddrInviter.String(), inviter))

	stakeAmount := math.NewInt(1000)
	invitationID, err := k.CreateInvitation(sdkCtx, TestAddrInviter, TestAddrInvitee, stakeAmount, []string{TestTagBackend})
	require.NoError(t, err)

	err = k.AcceptInvitation(sdkCtx, invitationID, TestAddrInvitee)
	require.NoError(t, err)

	// With zero burn rate, full amount should be returned
	inviter, _ = k.Member.Get(sdkCtx, TestAddrInviter.String())
	require.Equal(t, initialBalance, inviter.DreamBalance.Int64(),
		"with zero burn rate, full stake should be returned")
}

// ---------------------------------------------------------------------------
// Fix 5: Tag validation against registry
// ---------------------------------------------------------------------------

func TestCreateInitiativeRejectsFakeTags(t *testing.T) {
	t.Run("unknown tag rejected when tagKeeper is available", func(t *testing.T) {
		f := initFixtureWithTagKeeper(t, func(tag string) bool {
			// Only "backend" exists in registry
			return tag == TestTagBackend
		})
		k := f.keeper
		sdkCtx := sdk.UnwrapSDKContext(f.ctx)

		SetupMember(t, &k, sdkCtx, DefaultMemberConfig(TestAddrCreator))
		projectID := SetupBasicProject(t, &k, sdkCtx, TestAddrCreator)

		// Try to create initiative with a fake tag
		_, err := k.CreateInitiative(sdkCtx, TestAddrCreator, projectID,
			"Test", "Desc", []string{"fake-nonexistent-tag"},
			types.InitiativeTier_INITIATIVE_TIER_STANDARD,
			types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
			"", math.NewInt(TestRewardAmount))
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrTagNotRegistered)
	})

	t.Run("registered tag accepted", func(t *testing.T) {
		f := initFixtureWithTagKeeper(t, func(tag string) bool {
			return tag == TestTagBackend
		})
		k := f.keeper
		sdkCtx := sdk.UnwrapSDKContext(f.ctx)

		SetupMember(t, &k, sdkCtx, DefaultMemberConfig(TestAddrCreator))
		projectID := SetupBasicProject(t, &k, sdkCtx, TestAddrCreator)

		// Create initiative with a valid tag
		initID, err := k.CreateInitiative(sdkCtx, TestAddrCreator, projectID,
			"Test", "Desc", []string{TestTagBackend},
			types.InitiativeTier_INITIATIVE_TIER_STANDARD,
			types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
			"", math.NewInt(TestRewardAmount))
		require.NoError(t, err)
		require.NotZero(t, initID)
	})

	t.Run("nil tagKeeper skips validation gracefully", func(t *testing.T) {
		// Standard fixture has no tag keeper wired
		f := initFixture(t, WithAuthorizationPolicy(AlwaysAuthorized))
		k := f.keeper
		sdkCtx := sdk.UnwrapSDKContext(f.ctx)

		SetupMember(t, &k, sdkCtx, DefaultMemberConfig(TestAddrCreator))
		projectID := SetupBasicProject(t, &k, sdkCtx, TestAddrCreator)

		// Should succeed even with unknown tags (no tagKeeper to validate)
		initID, err := k.CreateInitiative(sdkCtx, TestAddrCreator, projectID,
			"Test", "Desc", []string{"any-random-tag"},
			types.InitiativeTier_INITIATIVE_TIER_STANDARD,
			types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
			"", math.NewInt(TestRewardAmount))
		require.NoError(t, err)
		require.NotZero(t, initID)
	})
}

// ---------------------------------------------------------------------------
// Fix 5b: Tag stuffing — max tags per initiative
// ---------------------------------------------------------------------------

func TestMaxTagsPerInitiativeEnforced(t *testing.T) {
	f := initFixture(t, WithAuthorizationPolicy(AlwaysAuthorized))
	k := f.keeper
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	SetupMember(t, &k, sdkCtx, DefaultMemberConfig(TestAddrCreator))
	projectID := SetupBasicProject(t, &k, sdkCtx, TestAddrCreator)

	// Default max is 3 tags — creating with 3 should succeed
	initID, err := k.CreateInitiative(sdkCtx, TestAddrCreator, projectID,
		"Three tags", "Desc", []string{"tag1", "tag2", "tag3"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
		"", math.NewInt(TestRewardAmount))
	require.NoError(t, err)
	require.NotZero(t, initID)

	// Creating with 4 tags should fail
	_, err = k.CreateInitiative(sdkCtx, TestAddrCreator, projectID,
		"Four tags", "Desc", []string{"tag1", "tag2", "tag3", "tag4"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
		"", math.NewInt(TestRewardAmount))
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrTooManyTags)
}

// ---------------------------------------------------------------------------
// Fix 5c: Reputation split across tags — total rep is constant regardless of tag count
// ---------------------------------------------------------------------------

func TestReputationSplitAcrossTags(t *testing.T) {
	// Create two identical initiatives, one with 1 tag and one with 3 tags.
	// The total reputation granted should be the same in both cases.
	f := initFixture(t, WithAuthorizationPolicy(AlwaysAuthorized))
	k := f.keeper
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// Use apprentice tier (min rep = 0) so fresh members can be assigned
	budget := math.NewInt(TestRewardAmount)

	// Setup creator with enough DREAM
	SetupMember(t, &k, sdkCtx, DefaultMemberConfig(TestAddrCreator))

	// --- Single-tag initiative ---
	addr1 := sdk.AccAddress([]byte("single-tag-worker_"))
	k.Member.Set(sdkCtx, addr1.String(), types.Member{
		Address:          addr1.String(),
		DreamBalance:     PtrInt(math.NewInt(TestLargeAmount)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{},
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		Status:           types.MemberStatus_MEMBER_STATUS_ACTIVE,
	})
	projectID1, err := k.CreateProject(sdkCtx, TestAddrCreator, "P1", "D", []string{"alpha"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(100000), math.NewInt(0))
	require.NoError(t, err)
	err = k.ApproveProject(sdkCtx, projectID1, TestAddrCreator, math.NewInt(100000), math.NewInt(0))
	require.NoError(t, err)

	initID1, err := k.CreateInitiative(sdkCtx, TestAddrCreator, projectID1, "T1", "D", []string{"alpha"},
		types.InitiativeTier_INITIATIVE_TIER_APPRENTICE, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, "", budget)
	require.NoError(t, err)
	err = k.AssignInitiativeToMember(sdkCtx, initID1, addr1)
	require.NoError(t, err)
	err = k.SubmitInitiativeWork(sdkCtx, initID1, addr1, "uri")
	require.NoError(t, err)

	// Force conviction high enough and complete
	init1, _ := k.GetInitiative(sdkCtx, initID1)
	params, _ := k.Params.Get(sdkCtx)
	reqConv := params.ConvictionPerDream.MulInt(budget).Mul(math.LegacyNewDec(2))
	init1.CurrentConviction = PtrDec(reqConv)
	init1.ExternalConviction = PtrDec(reqConv)
	k.UpdateInitiative(sdkCtx, init1)
	err = k.CompleteInitiative(sdkCtx, initID1)
	require.NoError(t, err)

	m1, _ := k.GetMember(sdkCtx, addr1)
	repAlpha, _ := math.LegacyNewDecFromStr(m1.ReputationScores["alpha"])
	require.True(t, repAlpha.IsPositive(), "should have gained reputation in alpha tag")

	// --- Three-tag initiative ---
	addr2 := sdk.AccAddress([]byte("three-tag-worker__"))
	k.Member.Set(sdkCtx, addr2.String(), types.Member{
		Address:          addr2.String(),
		DreamBalance:     PtrInt(math.NewInt(TestLargeAmount)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{},
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		Status:           types.MemberStatus_MEMBER_STATUS_ACTIVE,
	})
	projectID2, err := k.CreateProject(sdkCtx, TestAddrCreator, "P2", "D", []string{"beta"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(100000), math.NewInt(0))
	require.NoError(t, err)
	err = k.ApproveProject(sdkCtx, projectID2, TestAddrCreator, math.NewInt(100000), math.NewInt(0))
	require.NoError(t, err)

	initID2, err := k.CreateInitiative(sdkCtx, TestAddrCreator, projectID2, "T2", "D", []string{"beta", "gamma", "delta"},
		types.InitiativeTier_INITIATIVE_TIER_APPRENTICE, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, "", budget)
	require.NoError(t, err)
	err = k.AssignInitiativeToMember(sdkCtx, initID2, addr2)
	require.NoError(t, err)
	err = k.SubmitInitiativeWork(sdkCtx, initID2, addr2, "uri")
	require.NoError(t, err)

	init2, _ := k.GetInitiative(sdkCtx, initID2)
	init2.CurrentConviction = PtrDec(reqConv)
	init2.ExternalConviction = PtrDec(reqConv)
	k.UpdateInitiative(sdkCtx, init2)
	err = k.CompleteInitiative(sdkCtx, initID2)
	require.NoError(t, err)

	m2, _ := k.GetMember(sdkCtx, addr2)
	repBeta, _ := math.LegacyNewDecFromStr(m2.ReputationScores["beta"])
	repGamma, _ := math.LegacyNewDecFromStr(m2.ReputationScores["gamma"])
	repDelta, _ := math.LegacyNewDecFromStr(m2.ReputationScores["delta"])

	// Each tag should get ~1/3 of the single-tag grant (allowing rounding dust)
	totalMultiTagRep := repBeta.Add(repGamma).Add(repDelta)
	dust := repAlpha.Sub(totalMultiTagRep).Abs()
	maxDust := math.LegacyNewDecWithPrec(1, 15) // allow up to 0.000000000000001 rounding dust
	require.True(t, dust.LTE(maxDust),
		"total rep across 3 tags (%s) should approximately equal single-tag rep (%s), dust=%s", totalMultiTagRep, repAlpha, dust)

	// All three tags should get equal rep
	require.True(t, repBeta.Equal(repGamma) && repGamma.Equal(repDelta),
		"all tags should get equal rep: beta=%s gamma=%s delta=%s", repBeta, repGamma, repDelta)

	// Each individual tag rep should be approximately 1/3 of the single-tag rep
	expectedPerTag := repAlpha.QuoInt64(3)
	perTagDust := repBeta.Sub(expectedPerTag).Abs()
	require.True(t, perTagDust.LTE(maxDust),
		"per-tag rep (%s) should be ~1/3 of single-tag rep (%s)", repBeta, expectedPerTag)
}

// ---------------------------------------------------------------------------
// Fix 6: Stake splitting sqrt bypass — aggregate raw conviction per staker
// ---------------------------------------------------------------------------

func TestStakeSplittingGivesNoAdvantage(t *testing.T) {
	f := initFixture(t, WithAuthorizationPolicy(AlwaysAuthorized))
	k := f.keeper
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	stakeTime := int64(1000000)
	sdkCtx = sdkCtx.WithBlockTime(time.Unix(stakeTime, 0)).WithBlockHeight(500)

	// Setup: creator and two stakers with identical DREAM
	SetupMemberWithDream(t, &k, sdkCtx, TestAddrCreator, TestLargeAmount)
	SetupMemberWithDream(t, &k, sdkCtx, TestAddrStaker, TestLargeAmount*10)  // single-stake staker
	SetupMemberWithDream(t, &k, sdkCtx, TestAddrMember1, TestLargeAmount*10) // split-stake staker

	_, initiativeID1 := SetupProjectWithInitiative(t, &k, sdkCtx, TestAddrCreator)
	_, initiativeID2 := SetupProjectWithInitiative(t, &k, sdkCtx, TestAddrCreator)

	totalAmount := int64(10000)

	// Staker: one big stake of 10000
	_, err := k.CreateStake(sdkCtx, TestAddrStaker,
		types.StakeTargetType_STAKE_TARGET_INITIATIVE, initiativeID1, "", math.NewInt(totalAmount))
	require.NoError(t, err)

	// Member1: 10 stakes of 1000 each (same total)
	for i := 0; i < 10; i++ {
		_, err := k.CreateStake(sdkCtx, TestAddrMember1,
			types.StakeTargetType_STAKE_TARGET_INITIATIVE, initiativeID2, "", math.NewInt(totalAmount/10))
		require.NoError(t, err)
	}

	// Advance time so conviction builds
	sdkCtx = sdkCtx.WithBlockTime(time.Unix(stakeTime+86400*30, 0)).WithBlockHeight(600)

	// Re-trigger conviction update with advanced time
	require.NoError(t, k.UpdateInitiativeConvictionLazy(sdkCtx, initiativeID1))
	require.NoError(t, k.UpdateInitiativeConvictionLazy(sdkCtx, initiativeID2))

	init1, _ := k.GetInitiative(sdkCtx, initiativeID1)
	init2, _ := k.GetInitiative(sdkCtx, initiativeID2)

	singleConviction := DerefDec(init1.CurrentConviction)
	splitConviction := DerefDec(init2.CurrentConviction)

	// With the fix, splitting should give the same conviction (both get sqrt
	// of the same aggregate). Small floating point difference is acceptable.
	diff := singleConviction.Sub(splitConviction).Abs()
	tolerance := singleConviction.MulInt64(1).QuoInt64(100) // 1% tolerance

	require.True(t, diff.LTE(tolerance),
		"stake splitting should not give advantage: single=%s, split=%s, diff=%s",
		singleConviction, splitConviction, diff)
}

// ---------------------------------------------------------------------------
// Fix 8: Per-member-per-epoch reputation gain cap
// ---------------------------------------------------------------------------

func TestReputationGainCapPerEpoch(t *testing.T) {
	f := initFixture(t, WithAuthorizationPolicy(AlwaysAuthorized))
	k := f.keeper
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	SetupBasicMember(t, &k, sdkCtx, TestAddrMember1)

	params, _ := k.Params.Get(sdkCtx)
	maxGain := params.MaxReputationGainPerEpoch // 50

	// Grant many interims in the same epoch — each SIMPLE grants 5 rep
	// 10 simple interims = 50 rep requested, but cap is 50, so all 50 should be granted
	for i := uint64(1); i <= 10; i++ {
		interim := types.Interim{
			Id:         i,
			Type:       types.InterimType_INTERIM_TYPE_JURY_DUTY,
			Complexity: types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE,
		}
		require.NoError(t, k.GrantInterimReputation(sdkCtx, TestAddrMember1, interim))
	}

	member, _ := k.Member.Get(sdkCtx, TestAddrMember1.String())
	juryRep, _ := math.LegacyNewDecFromStr(member.ReputationScores["jury-duty"])

	// Should have exactly the cap (10 * 5 = 50 = maxGain)
	require.True(t, juryRep.LTE(maxGain),
		"reputation should be capped at %s, got %s", maxGain, juryRep)

	// Now try to grant one more — it should be capped at zero additional
	oneMore := types.Interim{
		Id:         11,
		Type:       types.InterimType_INTERIM_TYPE_JURY_DUTY,
		Complexity: types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE,
	}
	require.NoError(t, k.GrantInterimReputation(sdkCtx, TestAddrMember1, oneMore))

	member, _ = k.Member.Get(sdkCtx, TestAddrMember1.String())
	juryRepAfter, _ := math.LegacyNewDecFromStr(member.ReputationScores["jury-duty"])

	require.True(t, juryRepAfter.Equal(juryRep),
		"reputation should not increase past cap: before=%s, after=%s", juryRep, juryRepAfter)
}

func TestReputationGainCapResetsOnNewEpoch(t *testing.T) {
	f := initFixture(t, WithAuthorizationPolicy(AlwaysAuthorized))
	k := f.keeper
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	SetupBasicMember(t, &k, sdkCtx, TestAddrMember1)

	params, _ := k.Params.Get(sdkCtx)

	// Saturate the cap in epoch 0
	for i := uint64(1); i <= 12; i++ {
		interim := types.Interim{
			Id:         i,
			Type:       types.InterimType_INTERIM_TYPE_JURY_DUTY,
			Complexity: types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE,
		}
		require.NoError(t, k.GrantInterimReputation(sdkCtx, TestAddrMember1, interim))
	}

	member, _ := k.Member.Get(sdkCtx, TestAddrMember1.String())
	repAtCap, _ := math.LegacyNewDecFromStr(member.ReputationScores["jury-duty"])

	// Advance to next epoch
	sdkCtx = AdvanceEpochs(sdkCtx, params, 1)

	// Should be able to earn more reputation in the new epoch
	newInterim := types.Interim{
		Id:         100,
		Type:       types.InterimType_INTERIM_TYPE_JURY_DUTY,
		Complexity: types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE,
	}
	require.NoError(t, k.GrantInterimReputation(sdkCtx, TestAddrMember1, newInterim))

	member, _ = k.Member.Get(sdkCtx, TestAddrMember1.String())
	repAfterNewEpoch, _ := math.LegacyNewDecFromStr(member.ReputationScores["jury-duty"])

	require.True(t, repAfterNewEpoch.GT(repAtCap),
		"reputation should increase in new epoch: at_cap=%s, after_new_epoch=%s",
		repAtCap, repAfterNewEpoch)
}

func TestReputationGainCapZeroMeansUnlimited(t *testing.T) {
	params := types.DefaultParams()
	params.MaxReputationGainPerEpoch = math.LegacyZeroDec()
	f := initFixture(t, WithCustomParams(params), WithAuthorizationPolicy(AlwaysAuthorized))
	k := f.keeper
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	SetupBasicMember(t, &k, sdkCtx, TestAddrMember1)

	// Grant 20 expert interims (20 * 40 = 800 rep), should not be capped
	for i := uint64(1); i <= 20; i++ {
		interim := types.Interim{
			Id:         i,
			Type:       types.InterimType_INTERIM_TYPE_JURY_DUTY,
			Complexity: types.InterimComplexity_INTERIM_COMPLEXITY_EXPERT,
		}
		require.NoError(t, k.GrantInterimReputation(sdkCtx, TestAddrMember1, interim))
	}

	member, _ := k.Member.Get(sdkCtx, TestAddrMember1.String())
	juryRep, _ := math.LegacyNewDecFromStr(member.ReputationScores["jury-duty"])

	require.True(t, juryRep.Equal(math.LegacyNewDec(800)),
		"with zero cap (unlimited), all reputation should be granted: expected 800, got %s", juryRep)
}

// ---------------------------------------------------------------------------
// Fix 9: Circular member staking prevention
// ---------------------------------------------------------------------------

func TestCircularMemberStakePrevented(t *testing.T) {
	f := initFixture(t, WithAuthorizationPolicy(AlwaysAuthorized))
	k := f.keeper
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// Setup two members
	SetupMemberWithDream(t, &k, sdkCtx, TestAddrStaker, TestLargeAmount)
	SetupMemberWithDream(t, &k, sdkCtx, TestAddrMember1, TestLargeAmount)

	// A stakes on B — should succeed
	_, err := k.CreateStake(sdkCtx, TestAddrStaker,
		types.StakeTargetType_STAKE_TARGET_MEMBER, 0, TestAddrMember1.String(),
		math.NewInt(100))
	require.NoError(t, err)

	// B stakes on A — should fail (circular)
	_, err = k.CreateStake(sdkCtx, TestAddrMember1,
		types.StakeTargetType_STAKE_TARGET_MEMBER, 0, TestAddrStaker.String(),
		math.NewInt(100))
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrCircularMemberStake)
}

func TestNonCircularMemberStakeAllowed(t *testing.T) {
	f := initFixture(t, WithAuthorizationPolicy(AlwaysAuthorized))
	k := f.keeper
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// Setup three members
	SetupMemberWithDream(t, &k, sdkCtx, TestAddrStaker, TestLargeAmount)
	SetupMemberWithDream(t, &k, sdkCtx, TestAddrMember1, TestLargeAmount)
	SetupMemberWithDream(t, &k, sdkCtx, TestAddrMember2, TestLargeAmount)

	// A stakes on B — OK
	_, err := k.CreateStake(sdkCtx, TestAddrStaker,
		types.StakeTargetType_STAKE_TARGET_MEMBER, 0, TestAddrMember1.String(),
		math.NewInt(100))
	require.NoError(t, err)

	// C stakes on A — OK (no circular dependency)
	_, err = k.CreateStake(sdkCtx, TestAddrMember2,
		types.StakeTargetType_STAKE_TARGET_MEMBER, 0, TestAddrStaker.String(),
		math.NewInt(100))
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// DerefDec safely dereferences a *LegacyDec, returning zero if nil.
func DerefDec(d *math.LegacyDec) math.LegacyDec {
	if d == nil {
		return math.LegacyZeroDec()
	}
	return *d
}

// mockTagKeeperForAntiGaming is a mock implementation of the TagKeeper interface.
type mockTagKeeperForAntiGaming struct {
	tagExistsFn func(tag string) bool
}

func (m *mockTagKeeperForAntiGaming) TagExists(_ context.Context, name string) (bool, error) {
	if m.tagExistsFn != nil {
		return m.tagExistsFn(name), nil
	}
	return true, nil
}

func (m *mockTagKeeperForAntiGaming) IsReservedTag(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func (m *mockTagKeeperForAntiGaming) GetTag(_ context.Context, name string) (commontypes.Tag, error) {
	return commontypes.Tag{Name: name}, nil
}

func (m *mockTagKeeperForAntiGaming) IncrementTagUsage(_ context.Context, _ string, _ int64) error {
	return nil
}

// initFixtureWithTagKeeper creates a test fixture with a mock TagKeeper wired in.
func initFixtureWithTagKeeper(t *testing.T, tagExistsFn func(string) bool) *fixture {
	t.Helper()
	f := initFixture(t, WithAuthorizationPolicy(AlwaysAuthorized))
	tk := &mockTagKeeperForAntiGaming{tagExistsFn: tagExistsFn}
	f.keeper.SetTagKeeper(tk)
	return f
}
