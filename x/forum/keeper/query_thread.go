package keeper

import (
	"context"
	"errors"

	"sparkdream/x/forum/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) Thread(ctx context.Context, req *types.QueryThreadRequest) (*types.QueryThreadResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Verify the thread root exists and is actually a root post
	rootPost, err := q.k.Post.Get(ctx, req.RootId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "thread not found")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	if rootPost.ParentId != 0 {
		return nil, status.Error(codes.InvalidArgument, "not a root post")
	}

	// Return root + all posts whose root_id matches the requested root.
	posts, pageRes, err := query.CollectionFilteredPaginate(
		ctx,
		q.k.Post,
		req.Pagination,
		func(_ uint64, post types.Post) (bool, error) {
			if post.PostId == req.RootId {
				return true, nil
			}
			return post.RootId == req.RootId, nil
		},
		func(_ uint64, post types.Post) (types.Post, error) {
			return post, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryThreadResponse{Posts: posts, Pagination: pageRes}, nil
}
