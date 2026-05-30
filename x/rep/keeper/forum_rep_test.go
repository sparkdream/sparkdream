package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

// TestUpdateTrustLevel_ForumRepCountsForProvisional confirms that forum-earned
// reputation (Member.ForumRepPerTag) counts toward the NEW -> PROVISIONAL
// upgrade — discourse endorsement is a legitimate early-stage contribution
// signal at the lowest rungs.
func TestUpdateTrustLevel_ForumRepCountsForProvisional(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	member := sdk.AccAddress([]byte("forum-rep-prov"))
	params, _ := k.Params.Get(ctx)
	minRep := params.TrustLevelConfig.ProvisionalMinRep

	// All rep is from forum activity (no ReputationScores).
	k.Member.Set(ctx, member.String(), types.Member{
		Address:                member.String(),
		DreamBalance:           PtrInt(math.ZeroInt()),
		StakedDream:            PtrInt(math.ZeroInt()),
		LifetimeEarned:         PtrInt(math.ZeroInt()),
		LifetimeBurned:         PtrInt(math.ZeroInt()),
		ForumRepPerTag:         map[string]string{"backend": minRep.String()},
		TrustLevel:             types.TrustLevel_TRUST_LEVEL_NEW,
		CompletedInterimsCount: params.TrustLevelConfig.ProvisionalMinInterims,
	})

	require.NoError(t, k.UpdateTrustLevel(ctx, member))
	m, _ := k.Member.Get(ctx, member.String())
	require.Equal(t, types.TrustLevel_TRUST_LEVEL_PROVISIONAL, m.TrustLevel,
		"forum rep alone should be enough to clear PROVISIONAL when interims are met")
}

// TestUpdateTrustLevel_ForumRepCountsForEstablished confirms forum rep also
// counts at PROVISIONAL -> ESTABLISHED. ESTABLISHED is the "plugged-in" tier
// where conviction-staked endorsement is a legitimate signal.
func TestUpdateTrustLevel_ForumRepCountsForEstablished(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	member := sdk.AccAddress([]byte("forum-rep-est"))
	params, _ := k.Params.Get(ctx)
	threshold := params.TrustLevelConfig.EstablishedMinRep

	// Split rep: half from interim/initiative work, half from forum activity.
	half := threshold.Quo(math.LegacyNewDec(2))
	k.Member.Set(ctx, member.String(), types.Member{
		Address:                member.String(),
		DreamBalance:           PtrInt(math.ZeroInt()),
		StakedDream:            PtrInt(math.ZeroInt()),
		LifetimeEarned:         PtrInt(math.ZeroInt()),
		LifetimeBurned:         PtrInt(math.ZeroInt()),
		ReputationScores:       map[string]string{"backend": half.String()},
		ForumRepPerTag:         map[string]string{"backend": half.String()},
		TrustLevel:             types.TrustLevel_TRUST_LEVEL_PROVISIONAL,
		CompletedInterimsCount: params.TrustLevelConfig.EstablishedMinInterims,
	})

	require.NoError(t, k.UpdateTrustLevel(ctx, member))
	m, _ := k.Member.Get(ctx, member.String())
	require.Equal(t, types.TrustLevel_TRUST_LEVEL_ESTABLISHED, m.TrustLevel,
		"forum rep + verified-work rep summed should clear ESTABLISHED")
}

// TestUpdateTrustLevel_ForumRepExcludedFromTrusted is the critical asymmetry
// — TRUSTED is council-adjacent and requires verified-work rep only.
// Discourse popularity cannot push someone past ESTABLISHED.
func TestUpdateTrustLevel_ForumRepExcludedFromTrusted(t *testing.T) {
	customParams := types.DefaultParams()
	customParams.TrustLevelConfig.TrustedMinSeasons = 0
	fixture := initFixture(t, WithCustomParams(customParams))
	k := fixture.keeper
	ctx := fixture.ctx

	member := sdk.AccAddress([]byte("forum-rep-trusted"))
	params, _ := k.Params.Get(ctx)
	trustedMinRep := params.TrustLevelConfig.TrustedMinRep

	// 99% forum rep, 1% verified-work rep. Total >= TrustedMinRep BUT
	// verified-work rep alone is well below threshold.
	verifiedShare := trustedMinRep.Quo(math.LegacyNewDec(100)) // 1%
	forumShare := trustedMinRep.Sub(verifiedShare).Add(math.LegacyNewDec(1000))
	k.Member.Set(ctx, member.String(), types.Member{
		Address:          member.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": verifiedShare.String()},
		ForumRepPerTag:   map[string]string{"backend": forumShare.String()},
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		JoinedSeason:     0,
	})

	require.NoError(t, k.UpdateTrustLevel(ctx, member))
	m, _ := k.Member.Get(ctx, member.String())
	require.Equal(t, types.TrustLevel_TRUST_LEVEL_ESTABLISHED, m.TrustLevel,
		"forum rep must NOT count toward TRUSTED — capped by design")
}

// TestUpdateTrustLevel_ForumRepExcludedFromCore mirrors the TRUSTED test for
// the highest tier. CORE gates council eligibility; forum rep is excluded.
func TestUpdateTrustLevel_ForumRepExcludedFromCore(t *testing.T) {
	customParams := types.DefaultParams()
	customParams.TrustLevelConfig.CoreMinSeasons = 0
	fixture := initFixture(t, WithCustomParams(customParams))
	k := fixture.keeper
	ctx := fixture.ctx

	member := sdk.AccAddress([]byte("forum-rep-core"))
	params, _ := k.Params.Get(ctx)
	coreMinRep := params.TrustLevelConfig.CoreMinRep

	verifiedShare := coreMinRep.Quo(math.LegacyNewDec(100)) // 1%
	forumShare := coreMinRep.Sub(verifiedShare).Add(math.LegacyNewDec(1000))
	k.Member.Set(ctx, member.String(), types.Member{
		Address:          member.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": verifiedShare.String()},
		ForumRepPerTag:   map[string]string{"backend": forumShare.String()},
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_TRUSTED,
		JoinedSeason:     0,
	})

	require.NoError(t, k.UpdateTrustLevel(ctx, member))
	m, _ := k.Member.Get(ctx, member.String())
	require.Equal(t, types.TrustLevel_TRUST_LEVEL_TRUSTED, m.TrustLevel,
		"forum rep must NOT count toward CORE — council eligibility requires verified work")
}

// TestAddForumRep_Floors confirms AddForumRep accumulates correctly and
// DeductForumRep floors at zero (does not go negative).
func TestForumRep_AddAndDeductFloors(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	member := sdk.AccAddress([]byte("forum-rep-addr"))
	k.Member.Set(ctx, member.String(), types.Member{
		Address:        member.String(),
		DreamBalance:   PtrInt(math.ZeroInt()),
		StakedDream:    PtrInt(math.ZeroInt()),
		LifetimeEarned: PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.ZeroInt()),
		Status:         types.MemberStatus_MEMBER_STATUS_ACTIVE,
	})

	require.NoError(t, k.AddForumRep(ctx, member, "backend", math.LegacyNewDec(3)))
	require.NoError(t, k.AddForumRep(ctx, member, "backend", math.LegacyNewDec(2)))
	require.Equal(t, "5.000000000000000000", k.GetForumRep(ctx, member, "backend").String())

	// Over-deduct should floor at zero, not go negative.
	require.NoError(t, k.DeductForumRep(ctx, member, "backend", math.LegacyNewDec(100)))
	require.Equal(t, "0.000000000000000000", k.GetForumRep(ctx, member, "backend").String())
}

// TestForumRep_SumAcrossTags confirms SumForumRep aggregates per-tag scores.
func TestForumRep_SumAcrossTags(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	member := sdk.AccAddress([]byte("forum-rep-sum"))
	k.Member.Set(ctx, member.String(), types.Member{
		Address:        member.String(),
		DreamBalance:   PtrInt(math.ZeroInt()),
		StakedDream:    PtrInt(math.ZeroInt()),
		LifetimeEarned: PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.ZeroInt()),
		ForumRepPerTag: map[string]string{
			"backend":  "12.5",
			"frontend": "7.5",
			"docs":     "5.0",
		},
		Status: types.MemberStatus_MEMBER_STATUS_ACTIVE,
	})

	require.Equal(t, "25.000000000000000000", k.SumForumRep(ctx, member).String())
}
