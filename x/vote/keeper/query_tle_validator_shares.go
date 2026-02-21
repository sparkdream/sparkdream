package keeper

import (
	"context"

	"sparkdream/x/vote/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) TleValidatorShares(ctx context.Context, req *types.QueryTleValidatorSharesRequest) (*types.QueryTleValidatorSharesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	params, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to get params")
	}

	var shares []types.TleValidatorShare
	err = q.k.TleValidatorShare.Walk(ctx, nil, func(_ string, share types.TleValidatorShare) (bool, error) {
		shares = append(shares, share)
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	registeredValidators := uint64(len(shares))
	var thresholdNeeded uint64
	if params.TleThresholdDenominator > 0 {
		thresholdNeeded = (registeredValidators*uint64(params.TleThresholdNumerator) + uint64(params.TleThresholdDenominator) - 1) / uint64(params.TleThresholdDenominator)
	}

	return &types.QueryTleValidatorSharesResponse{
		Shares:               shares,
		TotalValidators:      registeredValidators,
		RegisteredValidators: registeredValidators,
		ThresholdNeeded:      thresholdNeeded,
	}, nil
}
