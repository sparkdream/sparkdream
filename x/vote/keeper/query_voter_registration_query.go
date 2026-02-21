package keeper

import (
	"context"

	"sparkdream/x/vote/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) VoterRegistrationQuery(ctx context.Context, req *types.QueryVoterRegistrationQueryRequest) (*types.QueryVoterRegistrationQueryResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	reg, err := q.k.VoterRegistration.Get(ctx, req.Address)
	if err != nil {
		return nil, status.Error(codes.NotFound, "voter registration not found")
	}

	return &types.QueryVoterRegistrationQueryResponse{Registration: reg}, nil
}
