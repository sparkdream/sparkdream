package keeper

import (
	"context"

	"sparkdream/x/blog/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) AnonymousReplyMeta(ctx context.Context, req *types.QueryAnonymousReplyMetaRequest) (*types.QueryAnonymousReplyMetaResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	meta, found := q.k.GetAnonymousReplyMeta(ctx, req.ReplyId)
	if !found {
		return &types.QueryAnonymousReplyMetaResponse{Metadata: nil}, nil
	}

	return &types.QueryAnonymousReplyMetaResponse{Metadata: &meta}, nil
}
