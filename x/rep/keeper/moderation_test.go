package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

// TestZeroMember tests the ZeroMember function
func TestZeroMember(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	memberAddr := sdk.AccAddress([]byte("member_to_zero__"))

	// Setup: Active member with DREAM balance and reputation
	err := k.Member.Set(ctx, memberAddr.String(), types.Member{
		Address:            memberAddr.String(),
		Status:             types.MemberStatus_MEMBER_STATUS_ACTIVE,
		DreamBalance:       PtrInt(math.NewInt(1000)),
		StakedDream:        PtrInt(math.NewInt(500)),
		LifetimeEarned:     PtrInt(math.NewInt(1500)),
		LifetimeBurned:     PtrInt(math.NewInt(0)),
		ReputationScores:   map[string]string{"backend": "100.0", "frontend": "50.0"},
		TrustLevel:         types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		InvitationCredits:  5,
		TipsGivenThisEpoch: 3,
		GiftsSentThisEpoch: PtrInt(math.NewInt(100)),
	})
	require.NoError(t, err)

	// Test: Zero the member
	err = k.ZeroMember(ctx, memberAddr, "violated community guidelines")
	require.NoError(t, err)

	// Verify member state after zeroing
	member, err := k.Member.Get(ctx, memberAddr.String())
	require.NoError(t, err)

	// Status should be ZEROED
	require.Equal(t, types.MemberStatus_MEMBER_STATUS_ZEROED, member.Status)

	// DREAM balance should be zeroed
	require.True(t, member.DreamBalance.IsZero(), "DREAM balance should be zero")
	require.True(t, member.StakedDream.IsZero(), "staked DREAM should be zero")

	// Lifetime burned should reflect burned amount (1000 + 500 = 1500)
	require.Equal(t, int64(1500), member.LifetimeBurned.Int64())

	// All reputation scores should be zeroed
	for tag, score := range member.ReputationScores {
		require.Equal(t, "0", score, "reputation for %s should be zeroed", tag)
	}

	// Lifetime reputation should be archived
	require.NotNil(t, member.LifetimeReputation)
	require.Contains(t, member.LifetimeReputation, "backend")
	require.Contains(t, member.LifetimeReputation, "frontend")

	// Trust level should be reset to NEW
	require.Equal(t, types.TrustLevel_TRUST_LEVEL_NEW, member.TrustLevel)

	// Credits and counters should be reset
	require.Equal(t, uint32(0), member.InvitationCredits)
	require.Equal(t, uint32(0), member.TipsGivenThisEpoch)
	require.True(t, member.GiftsSentThisEpoch.IsZero())

	// Zeroed count should be incremented
	require.Equal(t, uint32(1), member.ZeroedCount)

	// ZeroedAt should be set
	require.NotZero(t, member.ZeroedAt)
}

func TestZeroMember_NotFound(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	nonExistentAddr := sdk.AccAddress([]byte("non_existent____"))

	// Test: Should fail for non-existent member
	err := k.ZeroMember(ctx, nonExistentAddr, "test reason")
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrMemberNotFound)
}

func TestZeroMember_AlreadyZeroed(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	memberAddr := sdk.AccAddress([]byte("already_zeroed__"))

	// Setup: Already zeroed member
	err := k.Member.Set(ctx, memberAddr.String(), types.Member{
		Address:        memberAddr.String(),
		Status:         types.MemberStatus_MEMBER_STATUS_ZEROED,
		DreamBalance:   PtrInt(math.ZeroInt()),
		StakedDream:    PtrInt(math.ZeroInt()),
		LifetimeEarned: PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.ZeroInt()),
		TrustLevel:     types.TrustLevel_TRUST_LEVEL_NEW,
		ZeroedCount:    1,
	})
	require.NoError(t, err)

	// Test: Should fail for already zeroed member
	err = k.ZeroMember(ctx, memberAddr, "test reason")
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrMemberAlreadyZeroed)
}

// TestSlashReputation tests the SlashReputation function
func TestSlashReputation(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	memberAddr := sdk.AccAddress([]byte("member_to_slash_"))

	// Setup: Active member with reputation
	err := k.Member.Set(ctx, memberAddr.String(), types.Member{
		Address:          memberAddr.String(),
		Status:           types.MemberStatus_MEMBER_STATUS_ACTIVE,
		DreamBalance:     PtrInt(math.NewInt(1000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": "100.0", "frontend": "50.0"},
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
	})
	require.NoError(t, err)

	// Test: Slash 30% of all reputation
	penaltyRate := math.LegacyMustNewDecFromStr("0.3")
	err = k.SlashReputation(ctx, memberAddr, penaltyRate, nil, "poor quality work")
	require.NoError(t, err)

	// Verify reputation was slashed
	member, err := k.Member.Get(ctx, memberAddr.String())
	require.NoError(t, err)

	// Backend should be 100 * 0.7 = 70
	backendRep, err := math.LegacyNewDecFromStr(member.ReputationScores["backend"])
	require.NoError(t, err)
	expectedBackend := math.LegacyMustNewDecFromStr("70.0")
	require.True(t, backendRep.Equal(expectedBackend), "expected %s, got %s", expectedBackend, backendRep)

	// Frontend should be 50 * 0.7 = 35
	frontendRep, err := math.LegacyNewDecFromStr(member.ReputationScores["frontend"])
	require.NoError(t, err)
	expectedFrontend := math.LegacyMustNewDecFromStr("35.0")
	require.True(t, frontendRep.Equal(expectedFrontend), "expected %s, got %s", expectedFrontend, frontendRep)
}

func TestSlashReputation_SpecificTags(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	memberAddr := sdk.AccAddress([]byte("member_specific_"))

	// Setup: Active member with reputation across multiple tags
	err := k.Member.Set(ctx, memberAddr.String(), types.Member{
		Address:          memberAddr.String(),
		Status:           types.MemberStatus_MEMBER_STATUS_ACTIVE,
		DreamBalance:     PtrInt(math.NewInt(1000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": "100.0", "frontend": "50.0", "design": "80.0"},
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
	})
	require.NoError(t, err)

	// Test: Slash only backend tag by 50%
	penaltyRate := math.LegacyMustNewDecFromStr("0.5")
	err = k.SlashReputation(ctx, memberAddr, penaltyRate, []string{"backend"}, "specific tag violation")
	require.NoError(t, err)

	// Verify only backend was slashed
	member, err := k.Member.Get(ctx, memberAddr.String())
	require.NoError(t, err)

	// Backend should be 100 * 0.5 = 50
	backendRep, err := math.LegacyNewDecFromStr(member.ReputationScores["backend"])
	require.NoError(t, err)
	require.True(t, backendRep.Equal(math.LegacyMustNewDecFromStr("50.0")))

	// Frontend should be unchanged
	frontendRep, err := math.LegacyNewDecFromStr(member.ReputationScores["frontend"])
	require.NoError(t, err)
	require.True(t, frontendRep.Equal(math.LegacyMustNewDecFromStr("50.0")))

	// Design should be unchanged
	designRep, err := math.LegacyNewDecFromStr(member.ReputationScores["design"])
	require.NoError(t, err)
	require.True(t, designRep.Equal(math.LegacyMustNewDecFromStr("80.0")))
}

func TestSlashReputation_NotActive(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	memberAddr := sdk.AccAddress([]byte("inactive_member_"))

	// Setup: Inactive (zeroed) member
	err := k.Member.Set(ctx, memberAddr.String(), types.Member{
		Address:        memberAddr.String(),
		Status:         types.MemberStatus_MEMBER_STATUS_ZEROED,
		DreamBalance:   PtrInt(math.ZeroInt()),
		StakedDream:    PtrInt(math.ZeroInt()),
		LifetimeEarned: PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.ZeroInt()),
		TrustLevel:     types.TrustLevel_TRUST_LEVEL_NEW,
	})
	require.NoError(t, err)

	// Test: Should fail for inactive member
	penaltyRate := math.LegacyMustNewDecFromStr("0.3")
	err = k.SlashReputation(ctx, memberAddr, penaltyRate, nil, "test reason")
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrMemberNotActive)
}

func TestSlashReputation_InvalidPenaltyRate(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	memberAddr := sdk.AccAddress([]byte("member_invalid__"))

	// Setup: Active member
	err := k.Member.Set(ctx, memberAddr.String(), types.Member{
		Address:          memberAddr.String(),
		Status:           types.MemberStatus_MEMBER_STATUS_ACTIVE,
		DreamBalance:     PtrInt(math.NewInt(1000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": "100.0"},
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
	})
	require.NoError(t, err)

	// Test: Should fail for negative penalty rate
	negativePenalty := math.LegacyMustNewDecFromStr("-0.3")
	err = k.SlashReputation(ctx, memberAddr, negativePenalty, nil, "test reason")
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInvalidAmount)

	// Test: Should fail for penalty rate > 1
	excessivePenalty := math.LegacyMustNewDecFromStr("1.5")
	err = k.SlashReputation(ctx, memberAddr, excessivePenalty, nil, "test reason")
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInvalidAmount)
}

// TestDemoteMember tests the DemoteMember function
func TestDemoteMember(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	memberAddr := sdk.AccAddress([]byte("member_demote___"))

	// Setup: Active member with reputation
	err := k.Member.Set(ctx, memberAddr.String(), types.Member{
		Address:          memberAddr.String(),
		Status:           types.MemberStatus_MEMBER_STATUS_ACTIVE,
		DreamBalance:     PtrInt(math.NewInt(1000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": "100.0"},
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
	})
	require.NoError(t, err)

	// Test: Demote the member (uses SevereSlashPenalty from params, default 30%)
	err = k.DemoteMember(ctx, memberAddr, "repeated violations")
	require.NoError(t, err)

	// Verify reputation was slashed (30% by default)
	member, err := k.Member.Get(ctx, memberAddr.String())
	require.NoError(t, err)

	// Backend should be 100 * 0.7 = 70
	backendRep, err := math.LegacyNewDecFromStr(member.ReputationScores["backend"])
	require.NoError(t, err)
	expectedBackend := math.LegacyMustNewDecFromStr("70.0")
	require.True(t, backendRep.Equal(expectedBackend), "expected %s, got %s", expectedBackend, backendRep)
}

// TestIsMember tests the IsMember function
func TestIsMember(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	existingMemberAddr := sdk.AccAddress([]byte("existing_member_"))
	nonExistentAddr := sdk.AccAddress([]byte("non_existent____"))

	// Setup: Create a member
	err := k.Member.Set(ctx, existingMemberAddr.String(), types.Member{
		Address:        existingMemberAddr.String(),
		Status:         types.MemberStatus_MEMBER_STATUS_ACTIVE,
		DreamBalance:   PtrInt(math.ZeroInt()),
		StakedDream:    PtrInt(math.ZeroInt()),
		LifetimeEarned: PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.ZeroInt()),
		TrustLevel:     types.TrustLevel_TRUST_LEVEL_NEW,
	})
	require.NoError(t, err)

	// Test: Existing member should return true
	isMember := k.IsMember(ctx, existingMemberAddr)
	require.True(t, isMember, "existing member should return true")

	// Test: Non-existent address should return false
	isMember = k.IsMember(ctx, nonExistentAddr)
	require.False(t, isMember, "non-existent address should return false")
}

// TestIsActiveMember tests the IsActiveMember function
func TestIsActiveMember(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	activeMemberAddr := sdk.AccAddress([]byte("active_member___"))
	zeroedMemberAddr := sdk.AccAddress([]byte("zeroed_member___"))
	nonExistentAddr := sdk.AccAddress([]byte("non_existent____"))

	// Setup: Active member
	err := k.Member.Set(ctx, activeMemberAddr.String(), types.Member{
		Address:        activeMemberAddr.String(),
		Status:         types.MemberStatus_MEMBER_STATUS_ACTIVE,
		DreamBalance:   PtrInt(math.ZeroInt()),
		StakedDream:    PtrInt(math.ZeroInt()),
		LifetimeEarned: PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.ZeroInt()),
		TrustLevel:     types.TrustLevel_TRUST_LEVEL_NEW,
	})
	require.NoError(t, err)

	// Setup: Zeroed member
	err = k.Member.Set(ctx, zeroedMemberAddr.String(), types.Member{
		Address:        zeroedMemberAddr.String(),
		Status:         types.MemberStatus_MEMBER_STATUS_ZEROED,
		DreamBalance:   PtrInt(math.ZeroInt()),
		StakedDream:    PtrInt(math.ZeroInt()),
		LifetimeEarned: PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.ZeroInt()),
		TrustLevel:     types.TrustLevel_TRUST_LEVEL_NEW,
	})
	require.NoError(t, err)

	// Test: Active member should return true
	isActive := k.IsActiveMember(ctx, activeMemberAddr)
	require.True(t, isActive, "active member should return true")

	// Test: Zeroed member should return false
	isActive = k.IsActiveMember(ctx, zeroedMemberAddr)
	require.False(t, isActive, "zeroed member should return false")

	// Test: Non-existent address should return false
	isActive = k.IsActiveMember(ctx, nonExistentAddr)
	require.False(t, isActive, "non-existent address should return false")
}

// TestGetTrustLevel tests the GetTrustLevel function
func TestGetTrustLevel(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	testCases := []struct {
		name       string
		trustLevel types.TrustLevel
	}{
		{"NEW trust level", types.TrustLevel_TRUST_LEVEL_NEW},
		{"PROVISIONAL trust level", types.TrustLevel_TRUST_LEVEL_PROVISIONAL},
		{"ESTABLISHED trust level", types.TrustLevel_TRUST_LEVEL_ESTABLISHED},
		{"TRUSTED trust level", types.TrustLevel_TRUST_LEVEL_TRUSTED},
		{"CORE trust level", types.TrustLevel_TRUST_LEVEL_CORE},
	}

	for i, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			memberAddr := sdk.AccAddress([]byte("trust_level_" + string(rune('a'+i)) + "___"))

			// Setup: Create member with specific trust level
			err := k.Member.Set(ctx, memberAddr.String(), types.Member{
				Address:        memberAddr.String(),
				Status:         types.MemberStatus_MEMBER_STATUS_ACTIVE,
				DreamBalance:   PtrInt(math.ZeroInt()),
				StakedDream:    PtrInt(math.ZeroInt()),
				LifetimeEarned: PtrInt(math.ZeroInt()),
				LifetimeBurned: PtrInt(math.ZeroInt()),
				TrustLevel:     tc.trustLevel,
			})
			require.NoError(t, err)

			// Test: Get trust level
			trustLevel, err := k.GetTrustLevel(ctx, memberAddr)
			require.NoError(t, err)
			require.Equal(t, tc.trustLevel, trustLevel)
		})
	}
}

func TestGetTrustLevel_NotFound(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	nonExistentAddr := sdk.AccAddress([]byte("non_existent____"))

	// Test: Should return error for non-existent member
	_, err := k.GetTrustLevel(ctx, nonExistentAddr)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrMemberNotFound)
}

// TestGetReputationTier tests the GetReputationTier function
func TestGetReputationTier(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	testCases := []struct {
		name             string
		reputationScores map[string]string
		expectedTier     uint64
	}{
		{
			name:             "Tier 0: < 10 rep",
			reputationScores: map[string]string{"backend": "5.0"},
			expectedTier:     0,
		},
		{
			name:             "Tier 1: 10-49 rep",
			reputationScores: map[string]string{"backend": "25.0"},
			expectedTier:     1,
		},
		{
			name:             "Tier 2: 50-199 rep",
			reputationScores: map[string]string{"backend": "100.0"},
			expectedTier:     2,
		},
		{
			name:             "Tier 3: 200-499 rep",
			reputationScores: map[string]string{"backend": "300.0"},
			expectedTier:     3,
		},
		{
			name:             "Tier 4: 500-999 rep",
			reputationScores: map[string]string{"backend": "700.0"},
			expectedTier:     4,
		},
		{
			name:             "Tier 5: 1000+ rep",
			reputationScores: map[string]string{"backend": "1500.0"},
			expectedTier:     5,
		},
		{
			name:             "Multiple tags add up",
			reputationScores: map[string]string{"backend": "300.0", "frontend": "300.0"}, // Total: 600
			expectedTier:     4,                                                          // 500-999
		},
	}

	for i, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			memberAddr := sdk.AccAddress([]byte("rep_tier_" + string(rune('a'+i)) + "______"))

			// Setup: Create member with specific reputation
			err := k.Member.Set(ctx, memberAddr.String(), types.Member{
				Address:          memberAddr.String(),
				Status:           types.MemberStatus_MEMBER_STATUS_ACTIVE,
				DreamBalance:     PtrInt(math.ZeroInt()),
				StakedDream:      PtrInt(math.ZeroInt()),
				LifetimeEarned:   PtrInt(math.ZeroInt()),
				LifetimeBurned:   PtrInt(math.ZeroInt()),
				ReputationScores: tc.reputationScores,
				TrustLevel:       types.TrustLevel_TRUST_LEVEL_NEW,
			})
			require.NoError(t, err)

			// Test: Get reputation tier
			tier, err := k.GetReputationTier(ctx, memberAddr)
			require.NoError(t, err)
			require.Equal(t, tc.expectedTier, tier, "expected tier %d, got tier %d", tc.expectedTier, tier)
		})
	}
}

func TestGetReputationTier_NotFound(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	nonExistentAddr := sdk.AccAddress([]byte("non_existent____"))

	// Test: Should return error for non-existent member
	_, err := k.GetReputationTier(ctx, nonExistentAddr)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrMemberNotFound)
}

func TestGetReputationTier_EmptyReputation(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	memberAddr := sdk.AccAddress([]byte("no_reputation___"))

	// Setup: Member with no reputation scores
	err := k.Member.Set(ctx, memberAddr.String(), types.Member{
		Address:          memberAddr.String(),
		Status:           types.MemberStatus_MEMBER_STATUS_ACTIVE,
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: nil,
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_NEW,
	})
	require.NoError(t, err)

	// Test: Should return tier 0 for member with no reputation
	tier, err := k.GetReputationTier(ctx, memberAddr)
	require.NoError(t, err)
	require.Equal(t, uint64(0), tier)
}

// ─────────────────────────────────────────────────────────────────────────────
// AddReputation tests
// ─────────────────────────────────────────────────────────────────────────────

func TestAddReputation(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	memberAddr := sdk.AccAddress([]byte("add_rep_basic___"))

	// Setup: Active member with existing reputation
	err := k.Member.Set(ctx, memberAddr.String(), types.Member{
		Address:          memberAddr.String(),
		Status:           types.MemberStatus_MEMBER_STATUS_ACTIVE,
		DreamBalance:     PtrInt(math.NewInt(1000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": "100.0"},
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
	})
	require.NoError(t, err)

	// Test: Add 25.5 reputation to existing tag
	err = k.AddReputation(ctx, memberAddr, "backend", math.LegacyMustNewDecFromStr("25.5"))
	require.NoError(t, err)

	// Verify: 100 + 25.5 = 125.5
	member, err := k.Member.Get(ctx, memberAddr.String())
	require.NoError(t, err)

	backendRep, err := math.LegacyNewDecFromStr(member.ReputationScores["backend"])
	require.NoError(t, err)
	require.True(t, backendRep.Equal(math.LegacyMustNewDecFromStr("125.5")),
		"expected 125.5, got %s", backendRep)
}

func TestAddReputation_NewTag(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	memberAddr := sdk.AccAddress([]byte("add_rep_newtag__"))

	// Setup: Active member with no reputation scores
	err := k.Member.Set(ctx, memberAddr.String(), types.Member{
		Address:        memberAddr.String(),
		Status:         types.MemberStatus_MEMBER_STATUS_ACTIVE,
		DreamBalance:   PtrInt(math.NewInt(1000)),
		StakedDream:    PtrInt(math.ZeroInt()),
		LifetimeEarned: PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.ZeroInt()),
		TrustLevel:     types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
	})
	require.NoError(t, err)

	// Test: Add reputation to a tag that doesn't exist yet
	err = k.AddReputation(ctx, memberAddr, "reveal", math.LegacyNewDec(10))
	require.NoError(t, err)

	// Verify: new tag created with value 10
	member, err := k.Member.Get(ctx, memberAddr.String())
	require.NoError(t, err)

	revealRep, err := math.LegacyNewDecFromStr(member.ReputationScores["reveal"])
	require.NoError(t, err)
	require.True(t, revealRep.Equal(math.LegacyNewDec(10)),
		"expected 10, got %s", revealRep)
}

func TestAddReputation_NilReputationScores(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	memberAddr := sdk.AccAddress([]byte("add_rep_nilmap__"))

	// Setup: Active member with nil ReputationScores map
	err := k.Member.Set(ctx, memberAddr.String(), types.Member{
		Address:          memberAddr.String(),
		Status:           types.MemberStatus_MEMBER_STATUS_ACTIVE,
		DreamBalance:     PtrInt(math.NewInt(1000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: nil,
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
	})
	require.NoError(t, err)

	// Test: Should initialize the map and add the tag
	err = k.AddReputation(ctx, memberAddr, "reveal", math.LegacyNewDec(42))
	require.NoError(t, err)

	member, err := k.Member.Get(ctx, memberAddr.String())
	require.NoError(t, err)
	require.NotNil(t, member.ReputationScores)

	revealRep, err := math.LegacyNewDecFromStr(member.ReputationScores["reveal"])
	require.NoError(t, err)
	require.True(t, revealRep.Equal(math.LegacyNewDec(42)))
}

func TestAddReputation_ZeroAmount(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	memberAddr := sdk.AccAddress([]byte("add_rep_zero____"))

	err := k.Member.Set(ctx, memberAddr.String(), types.Member{
		Address:          memberAddr.String(),
		Status:           types.MemberStatus_MEMBER_STATUS_ACTIVE,
		DreamBalance:     PtrInt(math.NewInt(1000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": "100.0"},
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
	})
	require.NoError(t, err)

	// Test: Adding zero should succeed without changing score
	err = k.AddReputation(ctx, memberAddr, "backend", math.LegacyZeroDec())
	require.NoError(t, err)

	member, err := k.Member.Get(ctx, memberAddr.String())
	require.NoError(t, err)

	backendRep, err := math.LegacyNewDecFromStr(member.ReputationScores["backend"])
	require.NoError(t, err)
	require.True(t, backendRep.Equal(math.LegacyMustNewDecFromStr("100.0")))
}

func TestAddReputation_NegativeAmount(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	memberAddr := sdk.AccAddress([]byte("add_rep_neg_____"))

	err := k.Member.Set(ctx, memberAddr.String(), types.Member{
		Address:          memberAddr.String(),
		Status:           types.MemberStatus_MEMBER_STATUS_ACTIVE,
		DreamBalance:     PtrInt(math.NewInt(1000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": "100.0"},
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
	})
	require.NoError(t, err)

	// Test: Negative amount should fail
	err = k.AddReputation(ctx, memberAddr, "backend", math.LegacyMustNewDecFromStr("-5.0"))
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInvalidAmount)
}

func TestAddReputation_NotFound(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	nonExistentAddr := sdk.AccAddress([]byte("add_rep_missing_"))

	err := k.AddReputation(ctx, nonExistentAddr, "backend", math.LegacyNewDec(10))
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrMemberNotFound)
}

func TestAddReputation_NotActive(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	memberAddr := sdk.AccAddress([]byte("add_rep_zeroed__"))

	err := k.Member.Set(ctx, memberAddr.String(), types.Member{
		Address:        memberAddr.String(),
		Status:         types.MemberStatus_MEMBER_STATUS_ZEROED,
		DreamBalance:   PtrInt(math.ZeroInt()),
		StakedDream:    PtrInt(math.ZeroInt()),
		LifetimeEarned: PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.ZeroInt()),
		TrustLevel:     types.TrustLevel_TRUST_LEVEL_NEW,
	})
	require.NoError(t, err)

	err = k.AddReputation(ctx, memberAddr, "backend", math.LegacyNewDec(10))
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrMemberNotActive)
}

func TestAddReputation_Cumulative(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	memberAddr := sdk.AccAddress([]byte("add_rep_cumul___"))

	err := k.Member.Set(ctx, memberAddr.String(), types.Member{
		Address:          memberAddr.String(),
		Status:           types.MemberStatus_MEMBER_STATUS_ACTIVE,
		DreamBalance:     PtrInt(math.NewInt(1000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"reveal": "0"},
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
	})
	require.NoError(t, err)

	// Add multiple times
	require.NoError(t, k.AddReputation(ctx, memberAddr, "reveal", math.LegacyNewDec(10)))
	require.NoError(t, k.AddReputation(ctx, memberAddr, "reveal", math.LegacyNewDec(20)))
	require.NoError(t, k.AddReputation(ctx, memberAddr, "reveal", math.LegacyNewDec(5)))

	// Verify: 0 + 10 + 20 + 5 = 35
	member, err := k.Member.Get(ctx, memberAddr.String())
	require.NoError(t, err)

	revealRep, err := math.LegacyNewDecFromStr(member.ReputationScores["reveal"])
	require.NoError(t, err)
	require.True(t, revealRep.Equal(math.LegacyNewDec(35)),
		"expected 35, got %s", revealRep)
}

// ─────────────────────────────────────────────────────────────────────────────
// DeductReputation tests
// ─────────────────────────────────────────────────────────────────────────────

func TestDeductReputation(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	memberAddr := sdk.AccAddress([]byte("ded_rep_basic___"))

	// Setup: Active member with reputation
	err := k.Member.Set(ctx, memberAddr.String(), types.Member{
		Address:          memberAddr.String(),
		Status:           types.MemberStatus_MEMBER_STATUS_ACTIVE,
		DreamBalance:     PtrInt(math.NewInt(1000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"reveal": "100.0"},
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
	})
	require.NoError(t, err)

	// Test: Deduct 20 reputation
	err = k.DeductReputation(ctx, memberAddr, "reveal", math.LegacyNewDec(20))
	require.NoError(t, err)

	// Verify: 100 - 20 = 80
	member, err := k.Member.Get(ctx, memberAddr.String())
	require.NoError(t, err)

	revealRep, err := math.LegacyNewDecFromStr(member.ReputationScores["reveal"])
	require.NoError(t, err)
	require.True(t, revealRep.Equal(math.LegacyNewDec(80)),
		"expected 80, got %s", revealRep)
}

func TestDeductReputation_FloorAtZero(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	memberAddr := sdk.AccAddress([]byte("ded_rep_floor___"))

	err := k.Member.Set(ctx, memberAddr.String(), types.Member{
		Address:          memberAddr.String(),
		Status:           types.MemberStatus_MEMBER_STATUS_ACTIVE,
		DreamBalance:     PtrInt(math.NewInt(1000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"reveal": "15.0"},
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
	})
	require.NoError(t, err)

	// Test: Deduct more than current score (50 > 15)
	err = k.DeductReputation(ctx, memberAddr, "reveal", math.LegacyNewDec(50))
	require.NoError(t, err)

	// Verify: floored at 0
	member, err := k.Member.Get(ctx, memberAddr.String())
	require.NoError(t, err)

	revealRep, err := math.LegacyNewDecFromStr(member.ReputationScores["reveal"])
	require.NoError(t, err)
	require.True(t, revealRep.IsZero(), "expected 0, got %s", revealRep)
}

func TestDeductReputation_NewTag(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	memberAddr := sdk.AccAddress([]byte("ded_rep_newtag__"))

	err := k.Member.Set(ctx, memberAddr.String(), types.Member{
		Address:          memberAddr.String(),
		Status:           types.MemberStatus_MEMBER_STATUS_ACTIVE,
		DreamBalance:     PtrInt(math.NewInt(1000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": "100.0"},
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
	})
	require.NoError(t, err)

	// Test: Deduct from a tag that doesn't exist (starts at 0, floors at 0)
	err = k.DeductReputation(ctx, memberAddr, "reveal", math.LegacyNewDec(10))
	require.NoError(t, err)

	member, err := k.Member.Get(ctx, memberAddr.String())
	require.NoError(t, err)

	revealRep, err := math.LegacyNewDecFromStr(member.ReputationScores["reveal"])
	require.NoError(t, err)
	require.True(t, revealRep.IsZero(), "expected 0, got %s", revealRep)

	// Verify backend was not affected
	backendRep, err := math.LegacyNewDecFromStr(member.ReputationScores["backend"])
	require.NoError(t, err)
	require.True(t, backendRep.Equal(math.LegacyMustNewDecFromStr("100.0")))
}

func TestDeductReputation_NegativeAmount(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	memberAddr := sdk.AccAddress([]byte("ded_rep_neg_____"))

	err := k.Member.Set(ctx, memberAddr.String(), types.Member{
		Address:          memberAddr.String(),
		Status:           types.MemberStatus_MEMBER_STATUS_ACTIVE,
		DreamBalance:     PtrInt(math.NewInt(1000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": "100.0"},
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
	})
	require.NoError(t, err)

	// Test: Negative amount should fail
	err = k.DeductReputation(ctx, memberAddr, "backend", math.LegacyMustNewDecFromStr("-5.0"))
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInvalidAmount)
}

func TestDeductReputation_NotFound(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	nonExistentAddr := sdk.AccAddress([]byte("ded_rep_missing_"))

	err := k.DeductReputation(ctx, nonExistentAddr, "backend", math.LegacyNewDec(10))
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrMemberNotFound)
}

func TestDeductReputation_NotActive(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	memberAddr := sdk.AccAddress([]byte("ded_rep_zeroed__"))

	err := k.Member.Set(ctx, memberAddr.String(), types.Member{
		Address:        memberAddr.String(),
		Status:         types.MemberStatus_MEMBER_STATUS_ZEROED,
		DreamBalance:   PtrInt(math.ZeroInt()),
		StakedDream:    PtrInt(math.ZeroInt()),
		LifetimeEarned: PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.ZeroInt()),
		TrustLevel:     types.TrustLevel_TRUST_LEVEL_NEW,
	})
	require.NoError(t, err)

	err = k.DeductReputation(ctx, memberAddr, "backend", math.LegacyNewDec(10))
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrMemberNotActive)
}

func TestDeductReputation_ExactBalance(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	memberAddr := sdk.AccAddress([]byte("ded_rep_exact___"))

	err := k.Member.Set(ctx, memberAddr.String(), types.Member{
		Address:          memberAddr.String(),
		Status:           types.MemberStatus_MEMBER_STATUS_ACTIVE,
		DreamBalance:     PtrInt(math.NewInt(1000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"reveal": "20.0"},
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
	})
	require.NoError(t, err)

	// Test: Deduct exactly the current balance
	err = k.DeductReputation(ctx, memberAddr, "reveal", math.LegacyNewDec(20))
	require.NoError(t, err)

	member, err := k.Member.Get(ctx, memberAddr.String())
	require.NoError(t, err)

	revealRep, err := math.LegacyNewDecFromStr(member.ReputationScores["reveal"])
	require.NoError(t, err)
	require.True(t, revealRep.IsZero(), "expected 0, got %s", revealRep)
}

// ─────────────────────────────────────────────────────────────────────────────
// AddReputation + DeductReputation interaction tests
// ─────────────────────────────────────────────────────────────────────────────

func TestAddThenDeductReputation(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	memberAddr := sdk.AccAddress([]byte("rep_add_deduct__"))

	err := k.Member.Set(ctx, memberAddr.String(), types.Member{
		Address:          memberAddr.String(),
		Status:           types.MemberStatus_MEMBER_STATUS_ACTIVE,
		DreamBalance:     PtrInt(math.NewInt(1000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"reveal": "50.0"},
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
	})
	require.NoError(t, err)

	// Add 30, then deduct 10
	require.NoError(t, k.AddReputation(ctx, memberAddr, "reveal", math.LegacyNewDec(30)))
	require.NoError(t, k.DeductReputation(ctx, memberAddr, "reveal", math.LegacyNewDec(10)))

	// Verify: 50 + 30 - 10 = 70
	member, err := k.Member.Get(ctx, memberAddr.String())
	require.NoError(t, err)

	revealRep, err := math.LegacyNewDecFromStr(member.ReputationScores["reveal"])
	require.NoError(t, err)
	require.True(t, revealRep.Equal(math.LegacyNewDec(70)),
		"expected 70, got %s", revealRep)
}

func TestAddReputation_DoesNotAffectOtherTags(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	memberAddr := sdk.AccAddress([]byte("rep_isolate_____"))

	err := k.Member.Set(ctx, memberAddr.String(), types.Member{
		Address:          memberAddr.String(),
		Status:           types.MemberStatus_MEMBER_STATUS_ACTIVE,
		DreamBalance:     PtrInt(math.NewInt(1000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": "100.0", "frontend": "50.0"},
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
	})
	require.NoError(t, err)

	// Add to backend only
	require.NoError(t, k.AddReputation(ctx, memberAddr, "backend", math.LegacyNewDec(25)))

	member, err := k.Member.Get(ctx, memberAddr.String())
	require.NoError(t, err)

	backendRep, err := math.LegacyNewDecFromStr(member.ReputationScores["backend"])
	require.NoError(t, err)
	require.True(t, backendRep.Equal(math.LegacyMustNewDecFromStr("125.0")))

	// Frontend should be unchanged
	frontendRep, err := math.LegacyNewDecFromStr(member.ReputationScores["frontend"])
	require.NoError(t, err)
	require.True(t, frontendRep.Equal(math.LegacyMustNewDecFromStr("50.0")),
		"frontend should be unchanged, got %s", frontendRep)
}
