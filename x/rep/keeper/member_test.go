package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestUpdateTrustLevel_NewToProvisional(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	member := sdk.AccAddress([]byte("member"))

	// Setup: Member with insufficient reputation and interims
	k.Member.Set(ctx, member.String(), types.Member{
		Address:          member.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": "5.0"}, // Below threshold
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_NEW,
	})

	// Should not upgrade yet
	err := k.UpdateTrustLevel(ctx, member)
	require.NoError(t, err)
	m, _ := k.Member.Get(ctx, member.String())
	require.Equal(t, types.TrustLevel_TRUST_LEVEL_NEW, m.TrustLevel)

	// Update to meet requirements
	params, _ := k.Params.Get(ctx)
	minRep := params.TrustLevelConfig.ProvisionalMinRep
	k.Member.Set(ctx, member.String(), types.Member{
		Address:                member.String(),
		DreamBalance:           PtrInt(math.ZeroInt()),
		StakedDream:            PtrInt(math.ZeroInt()),
		LifetimeEarned:         PtrInt(math.ZeroInt()),
		LifetimeBurned:         PtrInt(math.ZeroInt()),
		ReputationScores:       map[string]string{"backend": minRep.String()},
		TrustLevel:             types.TrustLevel_TRUST_LEVEL_NEW,
		CompletedInterimsCount: params.TrustLevelConfig.ProvisionalMinInterims, // Use cached count
	})

	// Test: Should upgrade to PROVISIONAL
	err = k.UpdateTrustLevel(ctx, member)
	require.NoError(t, err)

	m, _ = k.Member.Get(ctx, member.String())
	require.Equal(t, types.TrustLevel_TRUST_LEVEL_PROVISIONAL, m.TrustLevel)
	require.NotZero(t, m.TrustLevelUpdatedAt)
}

func TestUpdateTrustLevel_ProvisionalToEstablished(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	member := sdk.AccAddress([]byte("member"))
	params, _ := k.Params.Get(ctx)

	// Setup: Member at PROVISIONAL level with enough completed interims (cached count)
	k.Member.Set(ctx, member.String(), types.Member{
		Address:                member.String(),
		DreamBalance:           PtrInt(math.ZeroInt()),
		StakedDream:            PtrInt(math.ZeroInt()),
		LifetimeEarned:         PtrInt(math.ZeroInt()),
		LifetimeBurned:         PtrInt(math.ZeroInt()),
		ReputationScores:       map[string]string{"backend": params.TrustLevelConfig.EstablishedMinRep.String()},
		TrustLevel:             types.TrustLevel_TRUST_LEVEL_PROVISIONAL,
		CompletedInterimsCount: params.TrustLevelConfig.EstablishedMinInterims, // Use cached count
	})

	// Test: Should upgrade to ESTABLISHED
	err := k.UpdateTrustLevel(ctx, member)
	require.NoError(t, err)

	m, _ := k.Member.Get(ctx, member.String())
	require.Equal(t, types.TrustLevel_TRUST_LEVEL_ESTABLISHED, m.TrustLevel)
}

func TestUpdateTrustLevel_EstablishedToTrusted(t *testing.T) {
	// Use custom params with TrustedMinSeasons=0 to test reputation-only upgrades
	// (The implementation also requires season requirements when TrustedMinSeasons > 0)
	customParams := types.DefaultParams()
	customParams.TrustLevelConfig.TrustedMinSeasons = 0
	fixture := initFixture(t, WithCustomParams(customParams))
	k := fixture.keeper
	ctx := fixture.ctx

	member := sdk.AccAddress([]byte("member"))
	params, _ := k.Params.Get(ctx)

	// Setup: Member at ESTABLISHED level with insufficient reputation to reach TRUSTED
	insufficientRep := params.TrustLevelConfig.TrustedMinRep.Sub(math.LegacyOneDec()) // Just under the threshold
	k.Member.Set(ctx, member.String(), types.Member{
		Address:          member.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": insufficientRep.String()},
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		JoinedSeason:     0,
	})

	// Test: Should not upgrade (insufficient reputation)
	err := k.UpdateTrustLevel(ctx, member)
	require.NoError(t, err)

	m, _ := k.Member.Get(ctx, member.String())
	require.Equal(t, types.TrustLevel_TRUST_LEVEL_ESTABLISHED, m.TrustLevel)

	// Now test that with enough reputation, they DO upgrade
	k.Member.Set(ctx, member.String(), types.Member{
		Address:          member.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": params.TrustLevelConfig.TrustedMinRep.String()},
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		JoinedSeason:     0,
	})

	err = k.UpdateTrustLevel(ctx, member)
	require.NoError(t, err)

	m, _ = k.Member.Get(ctx, member.String())
	require.Equal(t, types.TrustLevel_TRUST_LEVEL_TRUSTED, m.TrustLevel)
}

func TestUpdateTrustLevel_MultipleTagsReputation(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	member := sdk.AccAddress([]byte("member"))
	params, _ := k.Params.Get(ctx)

	// Setup: Member with reputation spread across multiple tags
	totalRep := params.TrustLevelConfig.ProvisionalMinRep
	halfRep := totalRep.Quo(math.LegacyNewDec(2))

	k.Member.Set(ctx, member.String(), types.Member{
		Address:        member.String(),
		DreamBalance:   PtrInt(math.ZeroInt()),
		StakedDream:    PtrInt(math.ZeroInt()),
		LifetimeEarned: PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{
			"backend":  halfRep.String(),
			"frontend": halfRep.String(),
		},
		TrustLevel:             types.TrustLevel_TRUST_LEVEL_NEW,
		CompletedInterimsCount: params.TrustLevelConfig.ProvisionalMinInterims, // Use cached count
	})

	// Test: Should upgrade (total reputation meets threshold)
	err := k.UpdateTrustLevel(ctx, member)
	require.NoError(t, err)

	m, _ := k.Member.Get(ctx, member.String())
	require.Equal(t, types.TrustLevel_TRUST_LEVEL_PROVISIONAL, m.TrustLevel)
}

func TestUpdateTrustLevel_InsufficientInterims(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	member := sdk.AccAddress([]byte("member"))
	params, _ := k.Params.Get(ctx)

	// Setup: Member with enough reputation but not enough interims
	// In testing mode, ProvisionalMinInterims is 1, so we set to 0 to test insufficient interims
	k.Member.Set(ctx, member.String(), types.Member{
		Address:                member.String(),
		DreamBalance:           PtrInt(math.ZeroInt()),
		StakedDream:            PtrInt(math.ZeroInt()),
		LifetimeEarned:         PtrInt(math.ZeroInt()),
		LifetimeBurned:         PtrInt(math.ZeroInt()),
		ReputationScores:       map[string]string{"backend": params.TrustLevelConfig.ProvisionalMinRep.Mul(math.LegacyNewDec(10)).String()}, // Way more than needed
		TrustLevel:             types.TrustLevel_TRUST_LEVEL_NEW,
		CompletedInterimsCount: 0, // Zero completed interims (less than required)
	})

	// Test: Should NOT upgrade (insufficient interims despite high reputation)
	err := k.UpdateTrustLevel(ctx, member)
	require.NoError(t, err)

	m, _ := k.Member.Get(ctx, member.String())
	require.Equal(t, types.TrustLevel_TRUST_LEVEL_NEW, m.TrustLevel)
}

func TestUpdateTrustLevel_InterimNotCompleted(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	member := sdk.AccAddress([]byte("member"))
	params, _ := k.Params.Get(ctx)

	// Setup: Member with enough reputation but zero completed interims
	// (The cached count represents only COMPLETED interims, not in-progress ones)
	k.Member.Set(ctx, member.String(), types.Member{
		Address:                member.String(),
		DreamBalance:           PtrInt(math.ZeroInt()),
		StakedDream:            PtrInt(math.ZeroInt()),
		LifetimeEarned:         PtrInt(math.ZeroInt()),
		LifetimeBurned:         PtrInt(math.ZeroInt()),
		ReputationScores:       map[string]string{"backend": params.TrustLevelConfig.ProvisionalMinRep.String()},
		TrustLevel:             types.TrustLevel_TRUST_LEVEL_NEW,
		CompletedInterimsCount: 0, // No completed interims (only in-progress ones don't count)
	})

	// Test: Should NOT upgrade (no completed interims)
	err := k.UpdateTrustLevel(ctx, member)
	require.NoError(t, err)

	m, _ := k.Member.Get(ctx, member.String())
	require.Equal(t, types.TrustLevel_TRUST_LEVEL_NEW, m.TrustLevel)
}

func TestUpdateTrustLevel_GrantsInvitationCredits(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	member := sdk.AccAddress([]byte("member"))
	params, _ := k.Params.Get(ctx)

	// Setup: Member at NEW level with 0 credits, ready to upgrade to PROVISIONAL
	k.Member.Set(ctx, member.String(), types.Member{
		Address:                member.String(),
		DreamBalance:           PtrInt(math.ZeroInt()),
		StakedDream:            PtrInt(math.ZeroInt()),
		LifetimeEarned:         PtrInt(math.ZeroInt()),
		LifetimeBurned:         PtrInt(math.ZeroInt()),
		ReputationScores:       map[string]string{"backend": params.TrustLevelConfig.ProvisionalMinRep.String()},
		TrustLevel:             types.TrustLevel_TRUST_LEVEL_NEW,
		CompletedInterimsCount: params.TrustLevelConfig.ProvisionalMinInterims,
		InvitationCredits:      0, // NEW level has 0 credits
	})

	// Test: Upgrade should grant credits
	err := k.UpdateTrustLevel(ctx, member)
	require.NoError(t, err)

	m, _ := k.Member.Get(ctx, member.String())
	require.Equal(t, types.TrustLevel_TRUST_LEVEL_PROVISIONAL, m.TrustLevel)

	// Should have gained credits (PROVISIONAL has 1 credit, NEW has 0, so gain = 1)
	expectedCredits := params.TrustLevelConfig.ProvisionalInvitationCredits - params.TrustLevelConfig.NewInvitationCredits
	require.Equal(t, expectedCredits, m.InvitationCredits)
}

func TestGetCurrentSeason(t *testing.T) {
	// Test that GetCurrentSeason properly delegates to the season keeper
	testCases := []struct {
		name           string
		seasonNumber   uint64
		expectedSeason int64
	}{
		{
			name:           "Season 0",
			seasonNumber:   0,
			expectedSeason: 0,
		},
		{
			name:           "Season 1",
			seasonNumber:   1,
			expectedSeason: 1,
		},
		{
			name:           "Season 5",
			seasonNumber:   5,
			expectedSeason: 5,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := initFixture(t, WithSeasonNumber(tc.seasonNumber))
			k := fixture.keeper
			ctx := fixture.ctx

			season, err := k.GetCurrentSeason(ctx)
			require.NoError(t, err)
			require.Equal(t, tc.expectedSeason, season)
		})
	}
}

func TestEnsureInvitationCreditsReset_NewSeason(t *testing.T) {
	// Use season 1 so member's LastCreditResetSeason=0 triggers reset
	fixture := initFixture(t, WithSeasonNumber(1))
	k := fixture.keeper
	ctx := fixture.ctx

	member := sdk.AccAddress([]byte("member"))
	params, _ := k.Params.Get(ctx)

	// Setup: Member at PROVISIONAL level with 0 credits, last reset at season 0
	k.Member.Set(ctx, member.String(), types.Member{
		Address:               member.String(),
		DreamBalance:          PtrInt(math.ZeroInt()),
		StakedDream:           PtrInt(math.ZeroInt()),
		LifetimeEarned:        PtrInt(math.ZeroInt()),
		LifetimeBurned:        PtrInt(math.ZeroInt()),
		TrustLevel:            types.TrustLevel_TRUST_LEVEL_PROVISIONAL,
		InvitationCredits:     0, // Used up all credits
		LastCreditResetSeason: 0, // Last reset at season 0
	})

	// Test: Should reset credits (current season 1 > last reset season 0)
	reset, err := k.EnsureInvitationCreditsReset(ctx, member.String())
	require.NoError(t, err)
	require.True(t, reset, "should have reset credits")

	// Verify credits were reset to PROVISIONAL max
	m, _ := k.Member.Get(ctx, member.String())
	require.Equal(t, params.TrustLevelConfig.ProvisionalInvitationCredits, m.InvitationCredits)
	require.Equal(t, int64(1), m.LastCreditResetSeason)
}

func TestEnsureInvitationCreditsReset_SameSeason(t *testing.T) {
	// Use season 0 so member's LastCreditResetSeason=0 does NOT trigger reset
	fixture := initFixture(t, WithSeasonNumber(0))
	k := fixture.keeper
	ctx := fixture.ctx

	member := sdk.AccAddress([]byte("member"))

	// Setup: Member already reset this season with some credits remaining
	k.Member.Set(ctx, member.String(), types.Member{
		Address:               member.String(),
		DreamBalance:          PtrInt(math.ZeroInt()),
		StakedDream:           PtrInt(math.ZeroInt()),
		LifetimeEarned:        PtrInt(math.ZeroInt()),
		LifetimeBurned:        PtrInt(math.ZeroInt()),
		TrustLevel:            types.TrustLevel_TRUST_LEVEL_CORE,
		InvitationCredits:     5, // Has 5 credits left
		LastCreditResetSeason: 0, // Already reset at season 0
	})

	// Test: Should NOT reset credits (current season 0 == last reset season 0)
	reset, err := k.EnsureInvitationCreditsReset(ctx, member.String())
	require.NoError(t, err)
	require.False(t, reset, "should not reset credits in same season")

	// Verify credits unchanged
	m, _ := k.Member.Get(ctx, member.String())
	require.Equal(t, uint32(5), m.InvitationCredits)
	require.Equal(t, int64(0), m.LastCreditResetSeason)
}

func TestEnsureInvitationCreditsReset_MultipleSeasons(t *testing.T) {
	// Use season 5 so member's LastCreditResetSeason=0 triggers reset
	fixture := initFixture(t, WithSeasonNumber(5))
	k := fixture.keeper
	ctx := fixture.ctx

	member := sdk.AccAddress([]byte("member"))
	params, _ := k.Params.Get(ctx)

	// Setup: Member last reset at season 0, now at season 5
	k.Member.Set(ctx, member.String(), types.Member{
		Address:               member.String(),
		DreamBalance:          PtrInt(math.ZeroInt()),
		StakedDream:           PtrInt(math.ZeroInt()),
		LifetimeEarned:        PtrInt(math.ZeroInt()),
		LifetimeBurned:        PtrInt(math.ZeroInt()),
		TrustLevel:            types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		InvitationCredits:     0,
		LastCreditResetSeason: 0, // Last reset at season 0
	})

	// Test: Should reset to current season (5 > 0)
	reset, err := k.EnsureInvitationCreditsReset(ctx, member.String())
	require.NoError(t, err)
	require.True(t, reset)

	m, _ := k.Member.Get(ctx, member.String())
	require.Equal(t, params.TrustLevelConfig.EstablishedInvitationCredits, m.InvitationCredits)
	require.Equal(t, int64(5), m.LastCreditResetSeason)
}

func TestGetMaxInvitationCredits(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	params, _ := k.Params.Get(ctx)
	config := params.TrustLevelConfig

	testCases := []struct {
		name     string
		level    types.TrustLevel
		expected uint32
	}{
		{
			name:     "NEW level",
			level:    types.TrustLevel_TRUST_LEVEL_NEW,
			expected: config.NewInvitationCredits,
		},
		{
			name:     "PROVISIONAL level",
			level:    types.TrustLevel_TRUST_LEVEL_PROVISIONAL,
			expected: config.ProvisionalInvitationCredits,
		},
		{
			name:     "ESTABLISHED level",
			level:    types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
			expected: config.EstablishedInvitationCredits,
		},
		{
			name:     "TRUSTED level",
			level:    types.TrustLevel_TRUST_LEVEL_TRUSTED,
			expected: config.TrustedInvitationCredits,
		},
		{
			name:     "CORE level",
			level:    types.TrustLevel_TRUST_LEVEL_CORE,
			expected: config.CoreInvitationCredits,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := keeper.GetMaxInvitationCredits(config, tc.level)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestGetInterimReputationTag(t *testing.T) {
	testCases := []struct {
		interimType types.InterimType
		expectedTag string
	}{
		{types.InterimType_INTERIM_TYPE_JURY_DUTY, "jury-duty"},
		{types.InterimType_INTERIM_TYPE_EXPERT_TESTIMONY, "expert-testimony"},
		{types.InterimType_INTERIM_TYPE_DISPUTE_MEDIATION, "dispute-mediation"},
		{types.InterimType_INTERIM_TYPE_PROJECT_APPROVAL, "project-approval"},
		{types.InterimType_INTERIM_TYPE_BUDGET_REVIEW, "budget-review"},
		{types.InterimType_INTERIM_TYPE_CONTRIBUTION_REVIEW, "contribution-review"},
		{types.InterimType_INTERIM_TYPE_OTHER, "interim-work"},
	}

	for _, tc := range testCases {
		t.Run(tc.interimType.String(), func(t *testing.T) {
			result := keeper.GetInterimReputationTag(tc.interimType)
			require.Equal(t, tc.expectedTag, result)
		})
	}
}

func TestGetInterimReputationGrant(t *testing.T) {
	testCases := []struct {
		complexity types.InterimComplexity
		expected   int64
	}{
		{types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE, 5},
		{types.InterimComplexity_INTERIM_COMPLEXITY_STANDARD, 10},
		{types.InterimComplexity_INTERIM_COMPLEXITY_COMPLEX, 20},
		{types.InterimComplexity_INTERIM_COMPLEXITY_EXPERT, 40},
		{types.InterimComplexity_INTERIM_COMPLEXITY_EPIC, 100},
	}

	for _, tc := range testCases {
		t.Run(tc.complexity.String(), func(t *testing.T) {
			result := keeper.GetInterimReputationGrant(tc.complexity)
			require.True(t, result.Equal(math.LegacyNewDec(tc.expected)), "expected %d, got %s", tc.expected, result)
		})
	}
}

func TestGrantInterimReputation(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	member := sdk.AccAddress([]byte("member"))

	// Setup: Member with no reputation
	k.Member.Set(ctx, member.String(), types.Member{
		Address:        member.String(),
		DreamBalance:   PtrInt(math.ZeroInt()),
		StakedDream:    PtrInt(math.ZeroInt()),
		LifetimeEarned: PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.ZeroInt()),
		TrustLevel:     types.TrustLevel_TRUST_LEVEL_NEW,
	})

	// Grant reputation for a SIMPLE jury duty interim
	interim := types.Interim{
		Id:         1,
		Type:       types.InterimType_INTERIM_TYPE_JURY_DUTY,
		Complexity: types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE,
	}

	err := k.GrantInterimReputation(ctx, member, interim)
	require.NoError(t, err)

	// Verify reputation was granted
	m, err := k.Member.Get(ctx, member.String())
	require.NoError(t, err)
	require.NotNil(t, m.ReputationScores)
	require.Contains(t, m.ReputationScores, "jury-duty")

	// Should have 5 reputation (SIMPLE complexity)
	repDec, err := math.LegacyNewDecFromStr(m.ReputationScores["jury-duty"])
	require.NoError(t, err)
	require.True(t, repDec.Equal(math.LegacyNewDec(5)))

	// Grant more reputation for another interim
	interim2 := types.Interim{
		Id:         2,
		Type:       types.InterimType_INTERIM_TYPE_JURY_DUTY,
		Complexity: types.InterimComplexity_INTERIM_COMPLEXITY_STANDARD,
	}

	err = k.GrantInterimReputation(ctx, member, interim2)
	require.NoError(t, err)

	// Should now have 15 reputation (5 + 10)
	m, _ = k.Member.Get(ctx, member.String())
	repDec, err = math.LegacyNewDecFromStr(m.ReputationScores["jury-duty"])
	require.NoError(t, err)
	require.True(t, repDec.Equal(math.LegacyNewDec(15)))
}

func TestGrantInterimReputation_DifferentTypes(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	member := sdk.AccAddress([]byte("member"))

	// Setup: Member with no reputation
	k.Member.Set(ctx, member.String(), types.Member{
		Address:        member.String(),
		DreamBalance:   PtrInt(math.ZeroInt()),
		StakedDream:    PtrInt(math.ZeroInt()),
		LifetimeEarned: PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.ZeroInt()),
		TrustLevel:     types.TrustLevel_TRUST_LEVEL_NEW,
	})

	// Grant reputation for different interim types
	interims := []types.Interim{
		{Id: 1, Type: types.InterimType_INTERIM_TYPE_JURY_DUTY, Complexity: types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE},
		{Id: 2, Type: types.InterimType_INTERIM_TYPE_EXPERT_TESTIMONY, Complexity: types.InterimComplexity_INTERIM_COMPLEXITY_STANDARD},
		{Id: 3, Type: types.InterimType_INTERIM_TYPE_PROJECT_APPROVAL, Complexity: types.InterimComplexity_INTERIM_COMPLEXITY_COMPLEX},
	}

	for _, interim := range interims {
		err := k.GrantInterimReputation(ctx, member, interim)
		require.NoError(t, err)
	}

	// Verify separate reputation tags
	m, _ := k.Member.Get(ctx, member.String())
	require.Contains(t, m.ReputationScores, "jury-duty")
	require.Contains(t, m.ReputationScores, "expert-testimony")
	require.Contains(t, m.ReputationScores, "project-approval")

	// Each should have the appropriate amount
	juryRep, _ := math.LegacyNewDecFromStr(m.ReputationScores["jury-duty"])
	require.True(t, juryRep.Equal(math.LegacyNewDec(5))) // SIMPLE = 5

	expertRep, _ := math.LegacyNewDecFromStr(m.ReputationScores["expert-testimony"])
	require.True(t, expertRep.Equal(math.LegacyNewDec(10))) // STANDARD = 10

	projectRep, _ := math.LegacyNewDecFromStr(m.ReputationScores["project-approval"])
	require.True(t, projectRep.Equal(math.LegacyNewDec(20))) // COMPLEX = 20
}
