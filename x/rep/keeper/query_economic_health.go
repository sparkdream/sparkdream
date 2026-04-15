package keeper

import (
	"context"

	"cosmossdk.io/math"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/types"
)

// DreamSupplyStats returns aggregate DREAM supply statistics.
func (q queryServer) DreamSupplyStats(ctx context.Context, req *types.QueryDreamSupplyStatsRequest) (*types.QueryDreamSupplyStatsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	totalMinted, err := q.k.GetSeasonMinted(ctx)
	if err != nil {
		totalMinted = math.ZeroInt()
	}
	totalBurned, err := q.k.GetSeasonBurned(ctx)
	if err != nil {
		totalBurned = math.ZeroInt()
	}
	treasuryBalance, err := q.k.GetTreasuryBalance(ctx)
	if err != nil {
		treasuryBalance = math.ZeroInt()
	}

	// Walk all members to compute circulating and staked totals
	circulating := math.ZeroInt()
	totalStaked := math.ZeroInt()
	_ = q.k.Member.Walk(ctx, nil, func(_ string, member types.Member) (bool, error) {
		circulating = circulating.Add(*member.DreamBalance)
		if member.StakedDream != nil {
			totalStaked = totalStaked.Add(*member.StakedDream)
		}
		return false, nil
	})

	stakedRatio := math.LegacyZeroDec()
	if circulating.IsPositive() {
		stakedRatio = math.LegacyNewDecFromInt(totalStaked).Quo(math.LegacyNewDecFromInt(circulating))
	}

	return &types.QueryDreamSupplyStatsResponse{
		TotalMinted:     totalMinted,
		TotalBurned:     totalBurned,
		Circulating:     circulating,
		TotalStaked:     totalStaked,
		TreasuryBalance: treasuryBalance,
		StakedRatio:     stakedRatio,
	}, nil
}

// MintBurnRatio returns the current season's mint/burn ratio.
func (q queryServer) MintBurnRatio(ctx context.Context, req *types.QueryMintBurnRatioRequest) (*types.QueryMintBurnRatioResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	seasonMinted, _ := q.k.GetSeasonMinted(ctx)
	seasonBurned, _ := q.k.GetSeasonBurned(ctx)

	ratio := math.LegacyZeroDec()
	if seasonBurned.IsPositive() {
		ratio = math.LegacyNewDecFromInt(seasonMinted).Quo(math.LegacyNewDecFromInt(seasonBurned))
	}

	seasonNum, _ := q.k.SeasonalPoolSeason.Get(ctx)

	return &types.QueryMintBurnRatioResponse{
		SeasonMinted: seasonMinted,
		SeasonBurned: seasonBurned,
		Ratio:        ratio,
		Season:       uint32(seasonNum),
	}, nil
}

// EffectiveApy returns the effective staking APY from the seasonal pool.
func (q queryServer) EffectiveApy(ctx context.Context, req *types.QueryEffectiveApyRequest) (*types.QueryEffectiveApyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	params, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	poolTotal := params.MaxStakingRewardsPerSeason
	poolRemaining, _ := q.k.getSeasonalPoolRemaining(ctx)
	totalStaked, _ := q.k.getSeasonalPoolTotalStaked(ctx)

	effectiveApy := math.LegacyZeroDec()
	if totalStaked.IsPositive() && params.SeasonDurationEpochs > 0 {
		// APY = (remaining / totalStaked) * (365 / seasonDurationEpochs)
		annualizationFactor := math.LegacyNewDec(365).Quo(math.LegacyNewDec(params.SeasonDurationEpochs))
		effectiveApy = math.LegacyNewDecFromInt(poolRemaining).
			Quo(math.LegacyNewDecFromInt(totalStaked)).
			Mul(annualizationFactor)
	}

	return &types.QueryEffectiveApyResponse{
		SeasonalPoolTotal:     poolTotal,
		SeasonalPoolRemaining: poolRemaining,
		TotalStaked:           totalStaked,
		EffectiveApy:          effectiveApy,
	}, nil
}

// TreasuryStatus returns the treasury balance and flow information.
func (q queryServer) TreasuryStatus(ctx context.Context, req *types.QueryTreasuryStatusRequest) (*types.QueryTreasuryStatusResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	params, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	balance, _ := q.k.GetTreasuryBalance(ctx)
	seasonMinted, _ := q.k.GetSeasonMinted(ctx)
	seasonBurned, _ := q.k.GetSeasonBurned(ctx)

	return &types.QueryTreasuryStatusResponse{
		Balance:      balance,
		MaxBalance:   params.MaxTreasuryBalance,
		SeasonInflow: seasonMinted, // approximate: total minted includes non-treasury sources
		SeasonOutflow: math.ZeroInt(), // TODO: track separately when treasury outflow is implemented
		SeasonBurned: seasonBurned,
	}, nil
}
