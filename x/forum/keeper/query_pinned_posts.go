package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// PinnedPosts lists pinned root posts, paginated over the PostsByPinned index
// (prefix-walked by category when category_id != 0). The legacy single-post
// fields describe the first post of the page.
func (q queryServer) PinnedPosts(ctx context.Context, req *types.QueryPinnedPostsRequest) (*types.QueryPinnedPostsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	var opts []func(*query.CollectionsPaginateOptions[collections.Pair[uint64, uint64]])
	if req.CategoryId != 0 {
		opts = append(opts, query.WithCollectionPaginationPairPrefix[uint64, uint64](req.CategoryId))
	}
	posts, pageRes, err := query.CollectionFilteredPaginate(
		ctx,
		q.k.PostsByPinned,
		req.Pagination,
		func(key collections.Pair[uint64, uint64], _ collections.NoValue) (bool, error) {
			p, err := q.k.Post.Get(ctx, key.K2())
			return err == nil && p.Pinned && p.CategoryId == key.K1(), nil
		},
		func(key collections.Pair[uint64, uint64], _ collections.NoValue) (types.Post, error) {
			return q.k.Post.Get(ctx, key.K2())
		},
		opts...,
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	resp := &types.QueryPinnedPostsResponse{Posts: posts, Pagination: pageRes}
	if len(posts) > 0 {
		resp.PostId = posts[0].PostId
		resp.Priority = uint64(posts[0].PinPriority)
		resp.PinnedBy = posts[0].PinnedBy
	}
	return resp, nil
}
