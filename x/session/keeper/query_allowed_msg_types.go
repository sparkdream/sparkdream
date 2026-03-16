package keeper

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/session/types"
)

func (q queryServer) AllowedMsgTypes(ctx context.Context, req *types.QueryAllowedMsgTypesRequest) (*types.QueryAllowedMsgTypesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	params, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	return &types.QueryAllowedMsgTypesResponse{
		MaxAllowedMsgTypes: params.MaxAllowedMsgTypes,
		AllowedMsgTypes:    params.AllowedMsgTypes,
	}, nil
}
