package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// LockedThreads lists locked thread roots, paginated over the LockedRoots
// index. The legacy single-thread fields describe the first post of the page.
func (q queryServer) LockedThreads(ctx context.Context, req *types.QueryLockedThreadsRequest) (*types.QueryLockedThreadsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	posts, pageRes, err := query.CollectionFilteredPaginate(
		ctx,
		q.k.LockedRoots,
		req.Pagination,
		func(rootID uint64, _ collections.NoValue) (bool, error) {
			p, err := q.k.Post.Get(ctx, rootID)
			return err == nil && p.ParentId == 0 && p.Locked, nil
		},
		func(rootID uint64, _ collections.NoValue) (types.Post, error) {
			return q.k.Post.Get(ctx, rootID)
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	resp := &types.QueryLockedThreadsResponse{Posts: posts, Pagination: pageRes}
	if len(posts) > 0 {
		resp.RootId = posts[0].PostId
		resp.LockedBy = posts[0].LockedBy
		resp.LockedAt = posts[0].LockedAt
	}
	return resp, nil
}
