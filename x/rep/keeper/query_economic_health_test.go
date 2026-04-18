package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestQueryDreamSupplyStats_EmptyFixture(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	resp, err := qs.DreamSupplyStats(f.ctx, &types.QueryDreamSupplyStatsRequest{})
	require.NoError(t, err)
	require.True(t, resp.TotalMinted.IsZero())
	require.True(t, resp.TotalBurned.IsZero())
	require.True(t, resp.Circulating.IsZero())
	require.True(t, resp.TotalStaked.IsZero())
	require.True(t, resp.TreasuryBalance.IsZero())
	require.True(t, resp.StakedRatio.IsZero())
}

func TestQueryDreamSupplyStats_ReflectsMembers(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	require.NoError(t, f.keeper.TrackMint(f.ctx, math.NewInt(500)))
	require.NoError(t, f.keeper.TrackBurn(f.ctx, math.NewInt(100)))
	require.NoError(t, f.keeper.AddToTreasury(f.ctx, math.NewInt(200)))

	addr := sdk.AccAddress([]byte("supplier"))
	bal := math.NewInt(1000)
	staked := math.NewInt(300)
	zero := math.ZeroInt()
	require.NoError(t, f.keeper.Member.Set(f.ctx, addr.String(), types.Member{
		Address:        addr.String(),
		DreamBalance:   &bal,
		StakedDream:    &staked,
		LifetimeEarned: &zero,
		LifetimeBurned: &zero,
	}))

	resp, err := qs.DreamSupplyStats(f.ctx, &types.QueryDreamSupplyStatsRequest{})
	require.NoError(t, err)
	require.Equal(t, math.NewInt(500), resp.TotalMinted)
	require.Equal(t, math.NewInt(100), resp.TotalBurned)
	require.Equal(t, math.NewInt(1000), resp.Circulating)
	require.Equal(t, math.NewInt(300), resp.TotalStaked)
	require.Equal(t, math.NewInt(200), resp.TreasuryBalance)
	require.True(t, resp.StakedRatio.IsPositive())
}

func TestQueryMintBurnRatio_ZeroBurnReturnsZeroRatio(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	require.NoError(t, f.keeper.TrackMint(f.ctx, math.NewInt(5_000)))

	resp, err := qs.MintBurnRatio(f.ctx, &types.QueryMintBurnRatioRequest{})
	require.NoError(t, err)
	require.Equal(t, math.NewInt(5_000), resp.SeasonMinted)
	require.True(t, resp.SeasonBurned.IsZero())
	require.True(t, resp.Ratio.IsZero(), "ratio must not divide by zero burn")
}

func TestQueryMintBurnRatio_Computed(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	require.NoError(t, f.keeper.TrackMint(f.ctx, math.NewInt(1_000)))
	require.NoError(t, f.keeper.TrackBurn(f.ctx, math.NewInt(500)))

	resp, err := qs.MintBurnRatio(f.ctx, &types.QueryMintBurnRatioRequest{})
	require.NoError(t, err)
	require.Equal(t, math.LegacyNewDec(2), resp.Ratio)
}

func TestQueryEffectiveApy_NoStakeIsZero(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	resp, err := qs.EffectiveApy(f.ctx, &types.QueryEffectiveApyRequest{})
	require.NoError(t, err)
	require.True(t, resp.EffectiveApy.IsZero(), "no staked DREAM means zero APY")
}

func TestQueryEffectiveApy_ScalesWithStake(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	require.NoError(t, f.keeper.InitSeasonalPool(f.ctx, 0))
	require.NoError(t, f.keeper.UpdateSeasonalPoolTotalStaked(f.ctx, math.NewInt(1_000)))

	resp, err := qs.EffectiveApy(f.ctx, &types.QueryEffectiveApyRequest{})
	require.NoError(t, err)
	require.True(t, resp.EffectiveApy.IsPositive(), "nonzero stake should yield a positive APY estimate")
}

func TestQueryTreasuryStatus(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	require.NoError(t, f.keeper.AddToTreasury(f.ctx, math.NewInt(7_500)))
	require.NoError(t, f.keeper.TrackMint(f.ctx, math.NewInt(1_234)))
	require.NoError(t, f.keeper.TrackBurn(f.ctx, math.NewInt(321)))

	resp, err := qs.TreasuryStatus(f.ctx, &types.QueryTreasuryStatusRequest{})
	require.NoError(t, err)
	require.Equal(t, math.NewInt(7_500), resp.Balance)
	require.Equal(t, math.NewInt(1_234), resp.SeasonInflow)
	require.Equal(t, math.NewInt(321), resp.SeasonBurned)
}

func TestQueryEconomicHealth_NilRequestsRejected(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	_, err := qs.DreamSupplyStats(f.ctx, nil)
	require.Error(t, err)
	_, err = qs.MintBurnRatio(f.ctx, nil)
	require.Error(t, err)
	_, err = qs.EffectiveApy(f.ctx, nil)
	require.Error(t, err)
	_, err = qs.TreasuryStatus(f.ctx, nil)
	require.Error(t, err)
}
