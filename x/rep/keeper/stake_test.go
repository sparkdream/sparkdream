package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestCreateStake(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Setup: Create project and initiative
	creator := sdk.AccAddress([]byte("creator"))
	projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"backend"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000))
	k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))

	assignee := sdk.AccAddress([]byte("assignee"))
	k.Member.Set(ctx, assignee.String(), types.Member{
		Address:          assignee.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": "100.0"},
	})

	initID, _ := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"backend"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, "", math.NewInt(100))
	k.AssignInitiativeToMember(ctx, initID, assignee)

	// Create staker with DREAM balance
	staker := sdk.AccAddress([]byte("staker"))
	k.Member.Set(ctx, staker.String(), types.Member{
		Address:          staker.String(),
		DreamBalance:     PtrInt(math.NewInt(10000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": "50.0"},
	})

	// Test: Create stake
	stakeAmount := math.NewInt(1000)
	stakeID, err := k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", stakeAmount)
	require.NoError(t, err)

	// Verify stake
	stake, err := k.Stake.Get(ctx, stakeID)
	require.NoError(t, err)
	require.Equal(t, staker.String(), stake.Staker)
	require.Equal(t, initID, stake.TargetId)
	require.Equal(t, types.StakeTargetType_STAKE_TARGET_INITIATIVE, stake.TargetType)
	require.Equal(t, stakeAmount.String(), stake.Amount.String())

	// Verify DREAM was locked
	stakerMember, _ := k.Member.Get(ctx, staker.String())
	require.Equal(t, stakeAmount.String(), stakerMember.StakedDream.String())
}

func TestCreateStakeErrors(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	creator := sdk.AccAddress([]byte("creator"))
	staker := sdk.AccAddress([]byte("staker"))
	k.Member.Set(ctx, staker.String(), types.Member{
		Address:          staker.String(),
		DreamBalance:     PtrInt(math.NewInt(100)), // Low balance
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: make(map[string]string),
	})

	// Create a project and initiative to test against
	projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"backend"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000))
	k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))
	initID, _ := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"backend"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, "", math.NewInt(100))

	// Test: Insufficient balance (staker has 100, trying to stake 1000)
	_, err := k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(1000))
	require.ErrorIs(t, err, types.ErrInsufficientBalance)

	// Test: Zero amount
	_, err = k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.ZeroInt())
	require.ErrorIs(t, err, types.ErrInvalidAmount)

	// Test: Negative amount
	_, err = k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(-100))
	require.ErrorIs(t, err, types.ErrInvalidAmount)
}

func TestRemoveStake(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Setup: Create stake
	creator := sdk.AccAddress([]byte("creator"))
	projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"backend"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000))
	k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))

	assignee := sdk.AccAddress([]byte("assignee"))
	k.Member.Set(ctx, assignee.String(), types.Member{
		Address:          assignee.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": "100.0"},
	})

	initID, _ := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"backend"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, "", math.NewInt(100))
	k.AssignInitiativeToMember(ctx, initID, assignee)

	staker := sdk.AccAddress([]byte("staker"))
	k.Member.Set(ctx, staker.String(), types.Member{
		Address:          staker.String(),
		DreamBalance:     PtrInt(math.NewInt(10000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": "50.0"},
	})

	stakeAmount := math.NewInt(1000)
	stakeID, _ := k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", stakeAmount)

	// Get initial balance after staking
	stakerMember, _ := k.Member.Get(ctx, staker.String())
	initialStaked := *stakerMember.StakedDream

	// Test: Remove stake
	err := k.RemoveStake(ctx, stakeID, staker, stakeAmount)
	require.NoError(t, err)

	// Verify DREAM was unlocked (with decay)
	stakerMember, _ = k.Member.Get(ctx, staker.String())
	require.True(t, stakerMember.StakedDream.LT(initialStaked)) // Decay applied
}

func TestCalculateConviction(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Test quadratic dampening
	// For amount = 100, conviction should be sqrt(100) = 10 (after time weighting = 1)
	stakeAmount := math.NewInt(100)

	// Create a stake to test conviction calculation
	staker := sdk.AccAddress([]byte("staker"))
	k.Member.Set(ctx, staker.String(), types.Member{
		Address:          staker.String(),
		DreamBalance:     PtrInt(math.NewInt(10000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: make(map[string]string),
	})

	creator := sdk.AccAddress([]byte("creator"))
	projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000))
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

	initID, _ := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, "", math.NewInt(100))
	k.AssignInitiativeToMember(ctx, initID, assignee)

	stakeID, _ := k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", stakeAmount)

	// Get stake and calculate conviction
	stake, _ := k.Stake.Get(ctx, stakeID)

	// Pass initiative tags for tag-weighted reputation
	conviction, err := k.CalculateStakeConviction(ctx, stake, []string{"tag"})
	require.NoError(t, err)

	// At creation, time weight is ~0, so conviction should be very small
	// After some blocks pass, conviction would grow
	require.True(t, conviction.GTE(math.LegacyZeroDec()))
}

// TestConvictionCalculation_TimeWeighting validates the time-based growth of conviction
func TestConvictionCalculation_TimeWeighting(t *testing.T) {
	fixture := initFixture(t)
	k := &fixture.keeper
	ctx := fixture.ctx

	// Setup member with no reputation (multiplier = 1.0)
	staker := sdk.AccAddress([]byte("staker"))
	k.Member.Set(ctx, staker.String(), types.Member{
		Address:          staker.String(),
		DreamBalance:     PtrInt(math.NewInt(10000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		ReputationScores: make(map[string]string),
	})

	// Create stake
	creator := sdk.AccAddress([]byte("creator"))
	projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000))
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

	initID, _ := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, "", math.NewInt(100))
	k.AssignInitiativeToMember(ctx, initID, assignee)

	// Stake 10000 DREAM (should result in sqrt(10000) = 100 conviction when fully weighted)
	stakeAmount := math.NewInt(10000)
	stakeID, _ := k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", stakeAmount)

	// Test conviction at different time points
	testCases := []struct {
		name           string
		advanceSeconds int64
		expectMinConv  math.LegacyDec // Minimum expected conviction
		expectMaxConv  math.LegacyDec // Maximum expected conviction
	}{
		{
			name:           "At creation (t=0)",
			advanceSeconds: 0,
			expectMinConv:  math.LegacyZeroDec(),
			expectMaxConv:  math.LegacyNewDec(1), // Very small conviction
		},
		{
			name:           "After 1 hour (growing)",
			advanceSeconds: 3600,
			expectMinConv:  math.LegacyNewDec(1),
			expectMaxConv:  math.LegacyNewDec(50), // Should be between 1-50
		},
		{
			name:           "After many days (approaching max)",
			advanceSeconds: 86400 * 30, // 30 days
			expectMinConv:  math.LegacyNewDec(80),
			expectMaxConv:  math.LegacyNewDec(100), // Approaching sqrt(10000) = 100
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Get params to calculate half-life
			params, _ := k.Params.Get(ctx)
			halfLifeSeconds := int64(params.ConvictionHalfLifeEpochs * params.EpochBlocks * 6)

			// Create new context with advanced time
			sdkCtx := sdk.UnwrapSDKContext(ctx)
			advanceDuration := time.Duration(tc.advanceSeconds) * time.Second
			newCtx := sdkCtx.WithBlockTime(sdkCtx.BlockTime().Add(advanceDuration))

			// Get stake and manually update CreatedAt to test time progression
			stake, _ := k.Stake.Get(ctx, stakeID)
			stake.CreatedAt = newCtx.BlockTime().Unix() - tc.advanceSeconds
			k.Stake.Set(newCtx, stakeID, stake)

			// Pass initiative tags for tag-weighted reputation (initiative has no tags, member has none)
			conviction, err := k.CalculateStakeConviction(newCtx, stake, []string{"tag"})
			require.NoError(t, err)

			// Verify conviction is within expected range
			require.True(t, conviction.GTE(tc.expectMinConv),
				"conviction %s should be >= %s", conviction.String(), tc.expectMinConv.String())
			require.True(t, conviction.LTE(tc.expectMaxConv),
				"conviction %s should be <= %s", conviction.String(), tc.expectMaxConv.String())

			t.Logf("Time: %ds, Half-life: %ds, Conviction: %s", tc.advanceSeconds, halfLifeSeconds, conviction.String())
		})
	}
}

// TestConvictionCalculation_QuadraticDampening validates sqrt dampening prevents whale dominance
func TestConvictionCalculation_QuadraticDampening(t *testing.T) {
	fixture := initFixture(t)
	k := &fixture.keeper
	ctx := fixture.ctx

	// Setup member
	staker := sdk.AccAddress([]byte("staker"))
	k.Member.Set(ctx, staker.String(), types.Member{
		Address:          staker.String(),
		DreamBalance:     PtrInt(math.NewInt(1000000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: make(map[string]string), // No reputation bonus
	})

	creator := sdk.AccAddress([]byte("creator"))
	projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(100000), math.NewInt(1000))
	k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(100000), math.NewInt(1000))

	assignee := sdk.AccAddress([]byte("assignee"))
	k.Member.Set(ctx, assignee.String(), types.Member{
		Address:          assignee.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"tag": "100.0"},
	})

	initID, _ := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, "", math.NewInt(100))
	k.AssignInitiativeToMember(ctx, initID, assignee)

	// Test: Quadratic dampening means 4x stake = 2x conviction (not 4x)
	testCases := []struct {
		name         string
		amount       int64
		expectedConv float64 // Expected conviction at full time weight
	}{
		{"100 DREAM", 100, 10.0},        // sqrt(100) = 10
		{"400 DREAM", 400, 20.0},        // sqrt(400) = 20 (4x amount = 2x conviction)
		{"10000 DREAM", 10000, 100.0},   // sqrt(10000) = 100
		{"40000 DREAM", 40000, 200.0},   // sqrt(40000) = 200 (4x amount = 2x conviction)
		{"100000 DREAM", 100000, 316.2}, // sqrt(100000) ≈ 316 (large stake has diminishing returns)
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create stake
			stakeID, err := k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(tc.amount))
			require.NoError(t, err)

			// Simulate full time weight (stake aged to half-life * 2)
			params, _ := k.Params.Get(ctx)
			halfLifeSeconds := int64(params.ConvictionHalfLifeEpochs * params.EpochBlocks * 6)

			sdkCtx := sdk.UnwrapSDKContext(ctx)
			ageDuration := time.Duration(halfLifeSeconds*2) * time.Second
			agedCtx := sdkCtx.WithBlockTime(sdkCtx.BlockTime().Add(ageDuration))

			stake, _ := k.Stake.Get(ctx, stakeID)
			// Pass initiative tags - member has no reputation in "tag", so base multiplier applies
			conviction, err := k.CalculateStakeConviction(agedCtx, stake, []string{"tag"})
			require.NoError(t, err)

			// With full time weight and no reputation bonus:
			// conviction ≈ sqrt(amount)
			// Allow 5% tolerance for time weight calculation
			expectedDec := math.LegacyNewDec(int64(tc.expectedConv * 100)).QuoInt64(100)
			tolerance := expectedDec.MulInt64(5).QuoInt64(100) // 5% tolerance

			diff := conviction.Sub(expectedDec).Abs()
			require.True(t, diff.LTE(tolerance),
				"conviction %s should be within 5%% of expected %s (diff: %s, tolerance: %s)",
				conviction.String(), expectedDec.String(), diff.String(), tolerance.String())

			t.Logf("Amount: %d, Expected: %s, Actual: %s", tc.amount, expectedDec.String(), conviction.String())
		})
	}
}

// TestConvictionCalculation_ReputationMultiplier validates reputation bonus
// Only reputation in the initiative's tags affects conviction weighting
func TestConvictionCalculation_ReputationMultiplier(t *testing.T) {
	fixture := initFixture(t)
	k := &fixture.keeper
	ctx := fixture.ctx

	creator := sdk.AccAddress([]byte("creator"))
	// Project and initiative use "backend" and "frontend" tags
	projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"backend", "frontend"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000))
	k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))

	assignee := sdk.AccAddress([]byte("assignee"))
	k.Member.Set(ctx, assignee.String(), types.Member{
		Address:          assignee.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": "100.0"},
	})

	// Initiative uses tags ["backend", "frontend"] - only reputation in these tags counts
	initID, _ := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"backend", "frontend"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, "", math.NewInt(100))
	k.AssignInitiativeToMember(ctx, initID, assignee)

	// Test different reputation levels - only "backend" and "frontend" tags affect conviction
	testCases := []struct {
		name                 string
		reputationScores     map[string]string
		stakeAmount          int64
		expectHigherThanBase bool // Should be higher than base (no reputation)
	}{
		{
			name:                 "No reputation (base)",
			reputationScores:     make(map[string]string),
			stakeAmount:          10000,
			expectHigherThanBase: false,
		},
		{
			name:                 "Low reputation in matching tag (100)",
			reputationScores:     map[string]string{"backend": "100.0"},
			stakeAmount:          10000,
			expectHigherThanBase: true, // Multiplier: 1 + 100/1000 = 1.1
		},
		{
			name:                 "High reputation in matching tag (500)",
			reputationScores:     map[string]string{"backend": "500.0"},
			stakeAmount:          10000,
			expectHigherThanBase: true, // Multiplier: 1 + 500/1000 = 1.5
		},
		{
			name:                 "Multiple matching tags (avg 400)",
			reputationScores:     map[string]string{"backend": "300.0", "frontend": "500.0"},
			stakeAmount:          10000,
			expectHigherThanBase: true, // Avg of (300+500)/2 = 400, Multiplier: 1 + 400/1000 = 1.4
		},
		{
			name:                 "Reputation in non-matching tag only (should be base)",
			reputationScores:     map[string]string{"governance": "1000.0"},
			stakeAmount:          10000,
			expectHigherThanBase: false, // "governance" not in initiative tags, so no bonus
		},
	}

	var baseConviction math.LegacyDec

	for i, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			staker := sdk.AccAddress([]byte("staker" + string(rune(i))))
			k.Member.Set(ctx, staker.String(), types.Member{
				Address:          staker.String(),
				DreamBalance:     PtrInt(math.NewInt(100000)),
				StakedDream:      PtrInt(math.ZeroInt()),
				LifetimeEarned:   PtrInt(math.ZeroInt()),
				LifetimeBurned:   PtrInt(math.ZeroInt()),
				ReputationScores: tc.reputationScores,
			})

			stakeID, _ := k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(tc.stakeAmount))

			// Age the stake to full time weight
			params, _ := k.Params.Get(ctx)
			halfLifeSeconds := int64(params.ConvictionHalfLifeEpochs * params.EpochBlocks * 6)

			sdkCtx := sdk.UnwrapSDKContext(ctx)
			ageDuration := time.Duration(halfLifeSeconds*2) * time.Second
			agedCtx := sdkCtx.WithBlockTime(sdkCtx.BlockTime().Add(ageDuration))

			stake, _ := k.Stake.Get(ctx, stakeID)
			// Pass the initiative's tags for tag-weighted reputation calculation
			conviction, err := k.CalculateStakeConviction(agedCtx, stake, []string{"backend", "frontend"})
			require.NoError(t, err)

			if !tc.expectHigherThanBase {
				// For base case (first run) or non-matching tags, conviction should equal base
				if baseConviction.IsNil() || baseConviction.IsZero() {
					baseConviction = conviction
					t.Logf("Base conviction (no reputation): %s", conviction.String())
				} else {
					// Non-matching tag case: should be equal to base (within small tolerance for floating point)
					diff := conviction.Sub(baseConviction).Abs()
					tolerance := baseConviction.MulInt64(1).QuoInt64(100) // 1% tolerance
					require.True(t, diff.LTE(tolerance),
						"conviction with non-matching tags %s should be ≈ base %s",
						conviction.String(), baseConviction.String())
					t.Logf("Conviction with non-matching tag: %s (base: %s)", conviction.String(), baseConviction.String())
				}
			} else {
				require.True(t, conviction.GT(baseConviction),
					"conviction with reputation %s should be > base %s",
					conviction.String(), baseConviction.String())
				t.Logf("Conviction with reputation: %s (base: %s, bonus: %s%%)",
					conviction.String(), baseConviction.String(),
					conviction.Sub(baseConviction).Quo(baseConviction).MulInt64(100).String())
			}
		})
	}
}

// TestConvictionCalculation_EdgeCases validates edge cases and error handling
func TestConvictionCalculation_EdgeCases(t *testing.T) {
	testCases := []struct {
		name        string
		setupFn     func(*testing.T, *keeper.Keeper, sdk.Context) types.Stake
		expectError bool
	}{
		{
			name: "Zero stake amount",
			setupFn: func(t *testing.T, k *keeper.Keeper, ctx sdk.Context) types.Stake {
				staker := sdk.AccAddress([]byte("staker"))
				k.Member.Set(ctx, staker.String(), types.Member{
					Address:          staker.String(),
					DreamBalance:     PtrInt(math.NewInt(1000)),
					ReputationScores: make(map[string]string),
				})
				return types.Stake{
					Id:        1,
					Staker:    staker.String(),
					Amount:    math.ZeroInt(),
					CreatedAt: sdk.UnwrapSDKContext(ctx).BlockTime().Unix(),
				}
			},
			expectError: false, // Should return zero conviction
		},
		{
			name: "Negative time elapsed (future creation time)",
			setupFn: func(t *testing.T, k *keeper.Keeper, ctx sdk.Context) types.Stake {
				staker := sdk.AccAddress([]byte("staker"))
				k.Member.Set(ctx, staker.String(), types.Member{
					Address:          staker.String(),
					DreamBalance:     PtrInt(math.NewInt(1000)),
					ReputationScores: make(map[string]string),
				})
				return types.Stake{
					Id:        1,
					Staker:    staker.String(),
					Amount:    math.NewInt(1000),
					CreatedAt: sdk.UnwrapSDKContext(ctx).BlockTime().Unix() + 3600, // 1 hour in future
				}
			},
			expectError: false, // Should handle gracefully, return zero conviction
		},
		{
			name: "Very large stake amount",
			setupFn: func(t *testing.T, k *keeper.Keeper, ctx sdk.Context) types.Stake {
				staker := sdk.AccAddress([]byte("staker"))
				largeAmount := math.NewInt(1_000_000_000_000) // 1 trillion
				k.Member.Set(ctx, staker.String(), types.Member{
					Address:          staker.String(),
					DreamBalance:     PtrInt(largeAmount),
					ReputationScores: make(map[string]string),
				})
				return types.Stake{
					Id:        1,
					Staker:    staker.String(),
					Amount:    largeAmount,
					CreatedAt: sdk.UnwrapSDKContext(ctx).BlockTime().Unix(),
				}
			},
			expectError: false, // Should handle large numbers
		},
		{
			name: "Invalid reputation values",
			setupFn: func(t *testing.T, k *keeper.Keeper, ctx sdk.Context) types.Stake {
				staker := sdk.AccAddress([]byte("staker"))
				k.Member.Set(ctx, staker.String(), types.Member{
					Address:          staker.String(),
					DreamBalance:     PtrInt(math.NewInt(1000)),
					ReputationScores: map[string]string{"invalid": "not-a-number"},
				})
				return types.Stake{
					Id:        1,
					Staker:    staker.String(),
					Amount:    math.NewInt(1000),
					CreatedAt: sdk.UnwrapSDKContext(ctx).BlockTime().Unix(),
				}
			},
			expectError: false, // Should ignore invalid reputation, use multiplier of 1.0
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := initFixture(t)
			k := &fixture.keeper
			ctx := fixture.ctx
			sdkCtx := sdk.UnwrapSDKContext(ctx)

			stake := tc.setupFn(t, k, sdkCtx)
			// Edge cases test uses nil tags (fallback to averaging all reputation)
			conviction, err := k.CalculateStakeConviction(ctx, stake, nil)

			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.True(t, conviction.GTE(math.LegacyZeroDec()),
					"conviction should be non-negative: %s", conviction.String())
				t.Logf("Edge case conviction: %s", conviction.String())
			}
		})
	}
}

func TestStakeRewards(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Setup: Create creator member with enough reputation for EXPERT tier (min 100)
	creator := sdk.AccAddress([]byte("creator"))
	k.Member.Set(ctx, creator.String(), types.Member{
		Address:          creator.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": "150.0"},
	})

	projectID, err := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"backend"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000))
	require.NoError(t, err)
	err = k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))
	require.NoError(t, err)

	assignee := sdk.AccAddress([]byte("assignee"))
	k.Member.Set(ctx, assignee.String(), types.Member{
		Address:          assignee.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": "150.0"},
	})

	// Use EXPERT tier which allows up to 2000 DREAM budget
	initBudget := math.NewInt(1000)
	initID, err := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"backend"}, types.InitiativeTier_INITIATIVE_TIER_EXPERT, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, "", initBudget)
	require.NoError(t, err)
	err = k.AssignInitiativeToMember(ctx, initID, assignee)
	require.NoError(t, err)

	// Create three stakers with different amounts
	staker1 := sdk.AccAddress([]byte("staker1"))
	staker2 := sdk.AccAddress([]byte("staker2"))
	staker3 := sdk.AccAddress([]byte("staker3"))

	stakers := []struct {
		addr   sdk.AccAddress
		amount math.Int
	}{
		{staker1, math.NewInt(1000)}, // Stake 1000 DREAM
		{staker2, math.NewInt(2000)}, // Stake 2000 DREAM
		{staker3, math.NewInt(500)},  // Stake 500 DREAM
	}

	// Create staker members and stakes
	for _, s := range stakers {
		k.Member.Set(ctx, s.addr.String(), types.Member{
			Address:          s.addr.String(),
			DreamBalance:     PtrInt(s.amount.Mul(math.NewInt(2))), // Give them enough balance
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"backend": "50.0"},
		})
		_, err := k.CreateStake(ctx, s.addr, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", s.amount)
		require.NoError(t, err)
	}

	// Submit work and get conviction to complete the initiative
	k.SubmitInitiativeWork(ctx, initID, assignee, "ipfs://deliverable")

	// Add conviction by updating stakes (simulate time passing and conviction building)
	// For testing, we'll manually set conviction to meet requirements
	initiative, _ := k.GetInitiative(ctx, initID)
	requiredConviction := math.LegacyNewDec(100) // Set a test threshold
	initiative.RequiredConviction = PtrDec(requiredConviction)
	initiative.CurrentConviction = PtrDec(requiredConviction.Mul(math.LegacyNewDec(2)))  // 200% of required
	initiative.ExternalConviction = PtrDec(requiredConviction.Mul(math.LegacyNewDec(2))) // All external
	k.UpdateInitiative(ctx, initiative)

	// Get initial balances before completion
	initialBalances := make(map[string]math.Int)
	for _, s := range stakers {
		member, _ := k.Member.Get(ctx, s.addr.String())
		initialBalances[s.addr.String()] = *member.DreamBalance
	}

	// Complete the initiative (this distributes rewards)
	err = k.CompleteInitiative(ctx, initID)
	require.NoError(t, err)

	// Get params for reward calculations
	params, _ := k.Params.Get(ctx)

	// Calculate expected time-based staking rewards
	// With the new implementation, rewards are calculated as: Stake × APY × (Duration / Year)
	// APY = 10% (0.10), Duration = time from stake creation to initiative completion
	const secondsPerYear = int64(365.25 * 24 * 60 * 60) // 31,557,600 seconds

	// Get all stakes to find their creation times (they were just created, so duration is minimal)
	allStakes, _ := k.GetInitiativeStakes(ctx, initID)

	// Since stakes are created just before completion, duration is very small
	// For realistic testing, let's check that the calculation is correct
	var stakeDuration int64
	if len(allStakes) > 0 {
		// Stakes were removed, but we know they were just created
		stakeDuration = 0 // Minimal time passed in test
	}

	// Test 1: Verify rewards are calculated based on time and APY
	// For each staker, expected reward = Stake × APY × (Duration / Year)
	for _, s := range stakers {
		member, err := k.Member.Get(ctx, s.addr.String())
		require.NoError(t, err)

		currentBalance := *member.DreamBalance
		initialBalance := initialBalances[s.addr.String()]

		// Calculate expected reward using APY formula
		// expectedReward = stakeAmount * APY * (duration / secondsPerYear)
		expectedReward := math.LegacyNewDecFromInt(s.amount).
			Mul(params.StakingApy).
			Mul(math.LegacyNewDec(stakeDuration)).
			Quo(math.LegacyNewDec(secondsPerYear)).
			TruncateInt()

		// The staker should have: initial balance - staked amount + returned stake + reward
		// Since stake is unlocked and reward is minted
		expectedBalance := initialBalance.Sub(s.amount).Add(s.amount).Add(expectedReward)

		require.Equal(t, expectedBalance.String(), currentBalance.String(),
			"staker %s: expected balance %s, got %s (initial: %s, stake: %s, reward: %s)",
			s.addr.String(), expectedBalance.String(), currentBalance.String(),
			initialBalance.String(), s.amount.String(), expectedReward.String())

		// Test 2: Verify lifetime earned is updated
		require.Equal(t, expectedReward.String(), member.LifetimeEarned.String(),
			"staker %s: lifetime earned should be %s, got %s",
			s.addr.String(), expectedReward.String(), member.LifetimeEarned.String())

		// Test 3: Verify staked DREAM is returned to available balance
		require.Equal(t, math.ZeroInt().String(), member.StakedDream.String(),
			"staker %s: staked DREAM should be 0 after completion, got %s",
			s.addr.String(), member.StakedDream.String())
	}

	// Test 4: Verify stakes are removed after completion
	stakes, err := k.GetInitiativeStakes(ctx, initID)
	require.NoError(t, err)
	require.Equal(t, 0, len(stakes), "all stakes should be removed after initiative completion")

	// Test 5: Verify initiative is marked as completed
	completedInitiative, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_COMPLETED, completedInitiative.Status)
	require.NotEqual(t, int64(0), completedInitiative.CompletedAt)

	// Test 6: Verify proportional APY-based distribution
	// With time-based APY rewards: reward = stake × APY × (duration / year)
	// Since duration and APY are constant, rewards are proportional to stake amount
	// Staker1: 1000 DREAM
	// Staker2: 2000 DREAM (should get 2x staker1's reward)
	// Staker3: 500 DREAM (should get 0.5x staker1's reward)
	member1, _ := k.Member.Get(ctx, staker1.String())
	member2, _ := k.Member.Get(ctx, staker2.String())
	member3, _ := k.Member.Get(ctx, staker3.String())

	reward1 := *member1.LifetimeEarned
	reward2 := *member2.LifetimeEarned
	reward3 := *member3.LifetimeEarned

	// Since all stakes have the same duration and APY, rewards should be proportional to amounts
	// Allow for the case where rewards are 0 (if duration is 0)
	if reward1.GT(math.ZeroInt()) {
		// Verify staker2 got 2x staker1's reward (2000/1000 = 2)
		ratio2to1 := math.LegacyNewDecFromInt(reward2).Quo(math.LegacyNewDecFromInt(reward1))
		expectedRatio := math.LegacyNewDec(2)
		require.True(t, ratio2to1.Sub(expectedRatio).Abs().LT(math.LegacyNewDecWithPrec(1, 2)),
			"staker2 should get 2x staker1's reward: ratio=%s, expected=%s",
			ratio2to1.String(), expectedRatio.String())

		// Verify staker3 got 0.5x staker1's reward (500/1000 = 0.5)
		ratio3to1 := math.LegacyNewDecFromInt(reward3).Quo(math.LegacyNewDecFromInt(reward1))
		expectedRatio = math.LegacyNewDecWithPrec(5, 1)
		require.True(t, ratio3to1.Sub(expectedRatio).Abs().LT(math.LegacyNewDecWithPrec(1, 2)),
			"staker3 should get 0.5x staker1's reward: ratio=%s, expected=%s",
			ratio3to1.String(), expectedRatio.String())

		t.Logf("APY-based rewards: staker1=%s, staker2=%s, staker3=%s",
			reward1.String(), reward2.String(), reward3.String())
	} else {
		// All rewards are 0 because duration is 0 in the test
		require.True(t, reward2.Equal(math.ZeroInt()), "reward2 should also be 0")
		require.True(t, reward3.Equal(math.ZeroInt()), "reward3 should also be 0")
		t.Log("All rewards are 0 because stake duration is 0 (stakes created and completed immediately)")
	}
}

// TestStakeRewardsWithTime tests time-based APY rewards with realistic durations
func TestStakeRewardsWithTime(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Setup: Create creator member with enough reputation for EXPERT tier (min 100)
	creator := sdk.AccAddress([]byte("creator"))
	k.Member.Set(ctx, creator.String(), types.Member{
		Address:          creator.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": "150.0"},
	})

	projectID, err := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"backend"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000))
	require.NoError(t, err)
	err = k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))
	require.NoError(t, err)

	assignee := sdk.AccAddress([]byte("assignee"))
	k.Member.Set(ctx, assignee.String(), types.Member{
		Address:          assignee.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": "150.0"},
	})

	// Use EXPERT tier which allows up to 2000 DREAM budget
	initBudget := math.NewInt(1000)
	initID, err := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"backend"}, types.InitiativeTier_INITIATIVE_TIER_EXPERT, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, "", initBudget)
	require.NoError(t, err)
	err = k.AssignInitiativeToMember(ctx, initID, assignee)
	require.NoError(t, err)

	// Create stakers with different amounts
	staker1 := sdk.AccAddress([]byte("staker1"))
	staker2 := sdk.AccAddress([]byte("staker2"))

	stakers := []struct {
		addr   sdk.AccAddress
		amount math.Int
	}{
		{staker1, math.NewInt(10000)}, // Stake 10000 DREAM
		{staker2, math.NewInt(5000)},  // Stake 5000 DREAM
	}

	// Create staker members and stakes
	for _, s := range stakers {
		k.Member.Set(ctx, s.addr.String(), types.Member{
			Address:          s.addr.String(),
			DreamBalance:     PtrInt(s.amount.Mul(math.NewInt(2))),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"backend": "50.0"},
		})
		_, err := k.CreateStake(ctx, s.addr, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", s.amount)
		require.NoError(t, err)
	}

	// Submit work
	k.SubmitInitiativeWork(ctx, initID, assignee, "ipfs://deliverable")

	// Simulate 30 days passing (30 days = 2,592,000 seconds)
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	thirtyDays := time.Duration(30*24) * time.Hour
	ctx = sdkCtx.WithBlockTime(sdkCtx.BlockTime().Add(thirtyDays))

	// Set conviction to meet requirements
	initiative, _ := k.GetInitiative(ctx, initID)
	requiredConviction := math.LegacyNewDec(100)
	initiative.RequiredConviction = PtrDec(requiredConviction)
	initiative.CurrentConviction = PtrDec(requiredConviction.Mul(math.LegacyNewDec(2)))
	initiative.ExternalConviction = PtrDec(requiredConviction.Mul(math.LegacyNewDec(2)))
	k.UpdateInitiative(ctx, initiative)

	// Complete the initiative
	err = k.CompleteInitiative(ctx, initID)
	require.NoError(t, err)

	// Get params
	params, _ := k.Params.Get(ctx)
	const secondsPerYear = int64(365.25 * 24 * 60 * 60) // 31,557,600 seconds
	durationSeconds := int64(30 * 24 * 60 * 60)         // 30 days in seconds

	// Test 1: Verify rewards match APY formula for staker1
	member1, err := k.Member.Get(ctx, staker1.String())
	require.NoError(t, err)

	// Expected reward for staker1: 10000 × 0.10 × (30 days / 365.25 days)
	// = 10000 × 0.10 × 0.08213 = 82.13 DREAM
	expectedReward1 := math.LegacyNewDecFromInt(stakers[0].amount).
		Mul(params.StakingApy).
		Mul(math.LegacyNewDec(durationSeconds)).
		Quo(math.LegacyNewDec(secondsPerYear)).
		TruncateInt()

	actualReward1 := *member1.LifetimeEarned
	require.Equal(t, expectedReward1.String(), actualReward1.String(),
		"staker1 reward should be %s, got %s", expectedReward1.String(), actualReward1.String())

	// Test 2: Verify rewards match APY formula for staker2
	member2, err := k.Member.Get(ctx, staker2.String())
	require.NoError(t, err)

	// Expected reward for staker2: 5000 × 0.10 × (30 days / 365.25 days)
	// = 5000 × 0.10 × 0.08213 = 41.06 DREAM
	expectedReward2 := math.LegacyNewDecFromInt(stakers[1].amount).
		Mul(params.StakingApy).
		Mul(math.LegacyNewDec(durationSeconds)).
		Quo(math.LegacyNewDec(secondsPerYear)).
		TruncateInt()

	actualReward2 := *member2.LifetimeEarned
	require.Equal(t, expectedReward2.String(), actualReward2.String(),
		"staker2 reward should be %s, got %s", expectedReward2.String(), actualReward2.String())

	// Test 3: Verify proportional relationship (staker1 staked 2x, should get 2x reward)
	require.True(t, actualReward1.GT(math.ZeroInt()), "reward1 should be positive")
	require.True(t, actualReward2.GT(math.ZeroInt()), "reward2 should be positive")

	ratio := math.LegacyNewDecFromInt(actualReward1).Quo(math.LegacyNewDecFromInt(actualReward2))
	expectedRatio := math.LegacyNewDec(2) // 10000/5000 = 2
	require.True(t, ratio.Sub(expectedRatio).Abs().LT(math.LegacyNewDecWithPrec(1, 10)),
		"reward ratio should be 2.0, got %s", ratio.String())

	t.Logf("30-day APY rewards (10%% annual):")
	t.Logf("  Staker1 (10000 DREAM): %s DREAM", actualReward1.String())
	t.Logf("  Staker2 (5000 DREAM):  %s DREAM", actualReward2.String())
	t.Logf("  Ratio: %s (expected: 2.0)", ratio.String())

	// Test 4: Verify balances are correct
	balance1 := *member1.DreamBalance
	balance2 := *member2.DreamBalance

	// Each staker should have: initial (2x stake) - stake + stake + reward
	// = initial + reward
	expectedBalance1 := stakers[0].amount.Mul(math.NewInt(2)).Add(expectedReward1)
	expectedBalance2 := stakers[1].amount.Mul(math.NewInt(2)).Add(expectedReward2)

	require.Equal(t, expectedBalance1.String(), balance1.String(),
		"staker1 balance should be %s, got %s", expectedBalance1.String(), balance1.String())
	require.Equal(t, expectedBalance2.String(), balance2.String(),
		"staker2 balance should be %s, got %s", expectedBalance2.String(), balance2.String())
}
