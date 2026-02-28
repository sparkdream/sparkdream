package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"cosmossdk.io/math"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) AuthorBond(ctx context.Context, req *types.QueryAuthorBondRequest) (*types.QueryAuthorBondResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	targetType := types.StakeTargetType(req.TargetType)
	if !types.IsAuthorBondType(targetType) {
		return nil, status.Errorf(codes.InvalidArgument, "target_type must be an author bond type (7, 8, or 9), got %d", req.TargetType)
	}

	bond, err := q.k.GetAuthorBond(ctx, targetType, req.TargetId)
	if err != nil {
		// Return zero bond if not found (rather than error)
		return &types.QueryAuthorBondResponse{
			BondAmount: math.ZeroInt(),
			Author:     "",
			StakeId:    0,
		}, nil
	}

	return &types.QueryAuthorBondResponse{
		BondAmount: bond.Amount,
		Author:     bond.Staker,
		StakeId:    bond.Id,
	}, nil
}
