package keeper

import (
	"context"

	"sparkdream/x/blog/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) AnonymousPostMeta(ctx context.Context, req *types.QueryAnonymousPostMetaRequest) (*types.QueryAnonymousPostMetaResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	meta, found := q.k.GetAnonymousPostMeta(ctx, req.PostId)
	if !found {
		return &types.QueryAnonymousPostMetaResponse{Metadata: nil}, nil
	}

	return &types.QueryAnonymousPostMetaResponse{Metadata: &meta}, nil
}
