package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	errorsmod "cosmossdk.io/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// FederatedContentBody returns a federated record's stored body, including
// bodies GetFederatedContent and ListFederatedContent withhold because
// media_flags != 0.
func (q queryServer) FederatedContentBody(ctx context.Context, req *types.QueryFederatedContentBodyRequest) (*types.QueryFederatedContentBodyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	content, err := q.k.Content.Get(ctx, req.Id)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrContentNotFound, "content ID %d not found", req.Id)
	}

	return &types.QueryFederatedContentBodyResponse{
		Body:        content.Body,
		MediaFlags:  content.MediaFlags,
		ContentHash: content.ContentHash,
	}, nil
}
