package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	errorsmod "cosmossdk.io/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) GetFederatedContent(ctx context.Context, req *types.QueryGetFederatedContentRequest) (*types.QueryGetFederatedContentResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	content, err := q.k.Content.Get(ctx, req.Id)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrContentNotFound, "content ID %d not found", req.Id)
	}

	return &types.QueryGetFederatedContentResponse{Content: content}, nil
}
