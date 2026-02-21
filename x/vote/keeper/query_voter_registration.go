package keeper

import (
	"context"
	"errors"

	"sparkdream/x/vote/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListVoterRegistration(ctx context.Context, req *types.QueryAllVoterRegistrationRequest) (*types.QueryAllVoterRegistrationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	voterRegistrations, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.VoterRegistration,
		req.Pagination,
		func(_ string, value types.VoterRegistration) (types.VoterRegistration, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllVoterRegistrationResponse{VoterRegistration: voterRegistrations, Pagination: pageRes}, nil
}

func (q queryServer) GetVoterRegistration(ctx context.Context, req *types.QueryGetVoterRegistrationRequest) (*types.QueryGetVoterRegistrationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.VoterRegistration.Get(ctx, req.Address)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetVoterRegistrationResponse{VoterRegistration: val}, nil
}
