package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// escalationParams returns DefaultParams with the two invitation-cost inputs
// forced to fixed values.
//
// InvitationCostMultiplier: the default testparams build sets it to 1.0 to
// keep E2E setup scripts simple, but cost-escalation tests must opt into the
// production behavior to exercise the multiplier path.
//
// The credit ladder: the ceiling is the OTHER half of the escalation input --
// cost is multiplier^(max_credits - remaining_credits), so a test that fixes
// the multiplier and then hardcodes an expected cost is still reading a
// build-tag-varying ceiling. Every rung varies (ESTABLISHED is 5 testparams,
// 6 mainnet, 8 testnet, 10 devnet, and TRUSTED and CORE diverge the same way),
// so pin the whole ladder rather than the one rung a given test happens to use
// today.
func pinCreditLadder(p *types.Params) {
	p.TrustLevelConfig.NewInvitationCredits = 0
	p.TrustLevelConfig.ProvisionalInvitationCredits = 2
	p.TrustLevelConfig.EstablishedInvitationCredits = 5
	p.TrustLevelConfig.TrustedInvitationCredits = 10
	p.TrustLevelConfig.CoreInvitationCredits = 20
}

// escalationParams returns DefaultParams with both invitation-cost inputs
// fixed. The default testparams build sets InvitationCostMultiplier to 1.0 to
// keep E2E setup scripts simple, but cost-escalation tests must opt into the
// production behavior to exercise the multiplier path.
func escalationParams() types.Params {
	p := types.DefaultParams()
	p.InvitationCostMultiplier = math.LegacyNewDecWithPrec(110, 2) // 1.1x
	pinCreditLadder(&p)
	return p
}

// pinnedCreditsParams returns DefaultParams with only the credit ladder
// pinned, for tests that assert on credit counts or on the un-escalated floor
// but deliberately leave InvitationCostMultiplier at its build-tag default.
func pinnedCreditsParams() types.Params {
	p := types.DefaultParams()
	pinCreditLadder(&p)
	return p
}

func TestRequiredInvitationStake_Query(t *testing.T) {
	params := escalationParams()
	f := initFixture(t, WithCustomParams(params))
	qs := keeper.NewQueryServerImpl(f.keeper)
	k := f.keeper
	ctx := f.ctx

	inviter := sdk.AccAddress([]byte("inviter"))
	require.NoError(t, k.Member.Set(ctx, inviter.String(), types.Member{
		Address:               inviter.String(),
		DreamBalance:          PtrInt(math.NewInt(10_000_000_000)),
		StakedDream:           PtrInt(math.ZeroInt()),
		LifetimeEarned:        PtrInt(math.ZeroInt()),
		LifetimeBurned:        PtrInt(math.ZeroInt()),
		ReputationScores:      map[string]string{},
		TrustLevel:            types.TrustLevel_TRUST_LEVEL_ESTABLISHED, // testparams: max=5
		InvitationCredits:     5,
		LastCreditResetSeason: 0,
	}))

	// Fresh inviter (used=0): required equals base floor, multiplier=1.
	resp, err := qs.RequiredInvitationStake(ctx, &types.QueryRequiredInvitationStakeRequest{Inviter: inviter.String()})
	require.NoError(t, err)
	require.Equal(t, math.NewInt(100_000_000), resp.RequiredStake)
	require.Equal(t, math.NewInt(100_000_000), resp.BaseStake)
	require.Equal(t, math.LegacyOneDec(), resp.CostMultiplier)
	require.Equal(t, uint32(0), resp.CreditsUsed)
	require.Equal(t, uint32(5), resp.CreditsRemaining)
	require.Equal(t, types.TrustLevel_TRUST_LEVEL_ESTABLISHED, resp.TrustLevel)

	// After spending 3 credits, used=3 → required = 100M * 1.1^3 = 133.1M (truncated to 133_100_000).
	member, err := k.Member.Get(ctx, inviter.String())
	require.NoError(t, err)
	member.InvitationCredits = 2
	require.NoError(t, k.Member.Set(ctx, inviter.String(), member))

	resp, err = qs.RequiredInvitationStake(ctx, &types.QueryRequiredInvitationStakeRequest{Inviter: inviter.String()})
	require.NoError(t, err)
	require.Equal(t, math.NewInt(133_100_000), resp.RequiredStake)
	require.Equal(t, uint32(3), resp.CreditsUsed)
	require.Equal(t, uint32(2), resp.CreditsRemaining)
	require.True(t, resp.CostMultiplier.GT(math.LegacyOneDec()))
}

func TestRequiredInvitationStake_QueryReflectsSeasonReset(t *testing.T) {
	f := initFixture(t, WithSeasonNumber(2), WithCustomParams(pinnedCreditsParams()))
	qs := keeper.NewQueryServerImpl(f.keeper)
	k := f.keeper
	ctx := f.ctx

	inviter := sdk.AccAddress([]byte("season_inviter"))
	require.NoError(t, k.Member.Set(ctx, inviter.String(), types.Member{
		Address:               inviter.String(),
		DreamBalance:          PtrInt(math.NewInt(10_000_000_000)),
		StakedDream:           PtrInt(math.ZeroInt()),
		LifetimeEarned:        PtrInt(math.ZeroInt()),
		LifetimeBurned:        PtrInt(math.ZeroInt()),
		ReputationScores:      map[string]string{},
		TrustLevel:            types.TrustLevel_TRUST_LEVEL_TRUSTED, // testparams: max=10
		InvitationCredits:     0,                                    // appears spent…
		LastCreditResetSeason: 1,                                    // …but was last reset in a prior season
	}))

	// Despite InvitationCredits=0 on the stored record, the query should
	// account for the pending seasonal reset (current season > last reset)
	// and report the base stake with full credits available.
	resp, err := qs.RequiredInvitationStake(ctx, &types.QueryRequiredInvitationStakeRequest{Inviter: inviter.String()})
	require.NoError(t, err)
	require.Equal(t, math.NewInt(100_000_000), resp.RequiredStake)
	require.Equal(t, uint32(0), resp.CreditsUsed)
	require.Equal(t, uint32(10), resp.CreditsRemaining)
}

func TestRequiredInvitationStake_QueryErrors(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	_, err := qs.RequiredInvitationStake(f.ctx, nil)
	require.Error(t, err)

	_, err = qs.RequiredInvitationStake(f.ctx, &types.QueryRequiredInvitationStakeRequest{Inviter: "not-an-address"})
	require.Error(t, err)

	_, err = qs.RequiredInvitationStake(f.ctx, &types.QueryRequiredInvitationStakeRequest{
		Inviter: sdk.AccAddress([]byte("nobody")).String(),
	})
	require.Error(t, err)
}

func TestCreateInvitation_CostEscalation(t *testing.T) {
	f := initFixture(t, WithCustomParams(escalationParams()))
	k := f.keeper
	ctx := f.ctx

	inviter := sdk.AccAddress([]byte("escalation_inviter"))
	require.NoError(t, k.Member.Set(ctx, inviter.String(), types.Member{
		Address:               inviter.String(),
		DreamBalance:          PtrInt(math.NewInt(10_000_000_000)),
		StakedDream:           PtrInt(math.ZeroInt()),
		LifetimeEarned:        PtrInt(math.ZeroInt()),
		LifetimeBurned:        PtrInt(math.ZeroInt()),
		ReputationScores:      map[string]string{},
		TrustLevel:            types.TrustLevel_TRUST_LEVEL_TRUSTED,
		InvitationCredits:     10, // testparams: TRUSTED max=10, fresh
		LastCreditResetSeason: 0,
	}))

	// First invitation at the base floor (100 DREAM) should succeed.
	invitee1 := sdk.AccAddress([]byte("escalation_invitee_1"))
	_, err := k.CreateInvitation(ctx, inviter, invitee1, math.NewInt(100_000_000), []string{"tag"})
	require.NoError(t, err)

	// Second invitation at the unchanged 100M floor should fail —
	// required is now 110M (100M * 1.1^1).
	invitee2 := sdk.AccAddress([]byte("escalation_invitee_2"))
	_, err = k.CreateInvitation(ctx, inviter, invitee2, math.NewInt(100_000_000), []string{"tag"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "insufficient stake")
	require.Contains(t, err.Error(), "required: 110000000")

	// Bumping to 110M satisfies the escalated floor.
	_, err = k.CreateInvitation(ctx, inviter, invitee2, math.NewInt(110_000_000), []string{"tag"})
	require.NoError(t, err)
}
