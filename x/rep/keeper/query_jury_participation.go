package keeper

import (
	"context"
	"errors"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListJuryParticipation(ctx context.Context, req *types.QueryAllJuryParticipationRequest) (*types.QueryAllJuryParticipationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	juryParticipations, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.JuryParticipation,
		req.Pagination,
		func(_ string, value types.JuryParticipation) (types.JuryParticipation, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllJuryParticipationResponse{JuryParticipation: juryParticipations, Pagination: pageRes}, nil
}

func (q queryServer) GetJuryParticipation(ctx context.Context, req *types.QueryGetJuryParticipationRequest) (*types.QueryGetJuryParticipationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.JuryParticipation.Get(ctx, req.Juror)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetJuryParticipationResponse{JuryParticipation: val}, nil
}
