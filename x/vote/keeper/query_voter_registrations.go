package keeper

import (
	"context"

	"sparkdream/x/vote/types"

	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) VoterRegistrations(ctx context.Context, req *types.QueryVoterRegistrationsRequest) (*types.QueryVoterRegistrationsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	registrations, pageResp, err := query.CollectionPaginate(
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

	return &types.QueryVoterRegistrationsResponse{
		Registrations: registrations,
		Pagination:    pageResp,
	}, nil
}
