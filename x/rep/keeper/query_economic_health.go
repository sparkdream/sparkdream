package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"

	"cosmossdk.io/math"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/rep/types"
)

// DreamSupplyStats returns aggregate DREAM supply statistics.
func (q queryServer) DreamSupplyStats(ctx context.Context, req *types.QueryDreamSupplyStatsRequest) (*types.QueryDreamSupplyStatsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	seasonMinted, err := q.k.GetSeasonMinted(ctx)
	if err != nil {
		seasonMinted = math.ZeroInt()
	}
	seasonBurned, err := q.k.GetSeasonBurned(ctx)
	if err != nil {
		seasonBurned = math.ZeroInt()
	}
	treasuryBalance, err := q.k.GetTreasuryBalance(ctx)
	if err != nil {
		treasuryBalance = math.ZeroInt()
	}

	// Walk all members to compute circulating and staked totals. LockDREAM does
	// not reduce DreamBalance — staked DREAM is a subset of it, tracked
	// separately in StakedDream — so `circulating` is every member's holdings
	// including what they have locked, and `circulating + treasury_balance` is
	// the live DREAM supply.
	circulating := math.ZeroInt()
	totalStaked := math.ZeroInt()
	if err := q.k.Member.Walk(ctx, nil, func(_ string, member types.Member) (bool, error) {
		circulating = circulating.Add(*member.DreamBalance)
		if member.StakedDream != nil {
			totalStaked = totalStaked.Add(*member.StakedDream)
		}
		return false, nil
	}); err != nil {
		return nil, err
	}

	stakedRatio := math.LegacyZeroDec()
	if circulating.IsPositive() {
		stakedRatio = math.LegacyNewDecFromInt(totalStaked).Quo(math.LegacyNewDecFromInt(circulating))
	}

	return &types.QueryDreamSupplyStatsResponse{
		SeasonMinted:    seasonMinted,
		SeasonBurned:    seasonBurned,
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

	seasonMinted, getErr := q.k.GetSeasonMinted(ctx)
	if getErr != nil {
		return nil, getErr
	}
	seasonBurned, getErr := q.k.GetSeasonBurned(ctx)
	if getErr != nil {
		return nil, getErr
	}

	ratio := math.LegacyZeroDec()
	if seasonBurned.IsPositive() {
		ratio = math.LegacyNewDecFromInt(seasonMinted).Quo(math.LegacyNewDecFromInt(seasonBurned))
	}

	seasonNum, getErr := q.k.SeasonalPoolSeason.Get(ctx)
	if getErr != nil && !errors.Is(getErr, collections.ErrNotFound) {
		return nil, getErr
	}

	return &types.QueryMintBurnRatioResponse{
		SeasonMinted: seasonMinted,
		SeasonBurned: seasonBurned,
		Ratio:        ratio,
		Season:       commontypes.Saturate[uint32](seasonNum),
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
	poolRemaining, getErr := q.k.getSeasonalPoolRemaining(ctx)
	if getErr != nil {
		return nil, getErr
	}
	totalStaked, getErr := q.k.getSeasonalPoolTotalStaked(ctx)
	if getErr != nil {
		return nil, getErr
	}

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

	balance, getErr := q.k.GetTreasuryBalance(ctx)
	if getErr != nil {
		return nil, getErr
	}
	seasonInflow, getErr := q.k.GetSeasonTreasuryInflow(ctx)
	if getErr != nil {
		return nil, getErr
	}
	seasonOutflow, getErr := q.k.GetSeasonTreasuryOutflow(ctx)
	if getErr != nil {
		return nil, getErr
	}
	seasonBurned, getErr := q.k.GetSeasonBurned(ctx)
	if getErr != nil {
		return nil, getErr
	}

	return &types.QueryTreasuryStatusResponse{
		Balance:       balance,
		MaxBalance:    params.MaxTreasuryBalance,
		SeasonInflow:  seasonInflow,
		SeasonOutflow: seasonOutflow,
		SeasonBurned:  seasonBurned,
	}, nil
}
