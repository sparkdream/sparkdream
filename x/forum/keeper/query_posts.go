package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) Posts(ctx context.Context, req *types.QueryPostsRequest) (*types.QueryPostsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	posts, pageRes, err := query.CollectionFilteredPaginate(
		ctx,
		q.k.Post,
		req.Pagination,
		func(_ uint64, post types.Post) (bool, error) {
			// Only include root posts (threads)
			if post.ParentId != 0 {
				return false, nil
			}
			if req.CategoryId != 0 && post.CategoryId != req.CategoryId {
				return false, nil
			}
			if req.Status != 0 && uint64(post.Status) != req.Status {
				return false, nil
			}
			return true, nil
		},
		func(_ uint64, post types.Post) (types.Post, error) {
			return post, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryPostsResponse{Posts: posts, Pagination: pageRes}, nil
}
