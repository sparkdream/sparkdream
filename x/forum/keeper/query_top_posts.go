package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TopPosts lists ACTIVE root posts by upvote count, highest first, paginated
// over the PostsByUpvotes index (the default order is descending; a request
// with pagination.reverse = true walks lowest first). category_id != 0 keeps
// only that category; time_range > 0 keeps posts created within the last
// time_range seconds. The legacy single-post fields describe the first post
// of the page.
func (q queryServer) TopPosts(ctx context.Context, req *types.QueryTopPostsRequest) (*types.QueryTopPostsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	pagination := req.Pagination
	if pagination == nil {
		pagination = &query.PageRequest{}
	}
	// Highest first unless the caller explicitly reverses.
	page := *pagination
	page.Reverse = !pagination.Reverse

	var since int64
	if req.TimeRange > 0 {
		since = sdk.UnwrapSDKContext(ctx).BlockTime().Unix() - req.TimeRange
	}

	posts, pageRes, err := query.CollectionFilteredPaginate(
		ctx,
		q.k.PostsByUpvotes,
		&page,
		func(key collections.Pair[uint64, uint64], _ collections.NoValue) (bool, error) {
			p, err := q.k.Post.Get(ctx, key.K2())
			if err != nil || p.ParentId != 0 || p.Status != types.PostStatus_POST_STATUS_ACTIVE {
				return false, nil
			}
			if req.CategoryId != 0 && p.CategoryId != req.CategoryId {
				return false, nil
			}
			return since == 0 || p.CreatedAt >= since, nil
		},
		func(key collections.Pair[uint64, uint64], _ collections.NoValue) (types.Post, error) {
			return q.k.getWithheldPost(ctx, key.K2())
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	resp := &types.QueryTopPostsResponse{Posts: posts, Pagination: pageRes}
	if len(posts) > 0 {
		resp.PostId = posts[0].PostId
		resp.UpvoteCount = posts[0].UpvoteCount
		resp.DownvoteCount = posts[0].DownvoteCount
	}
	return resp, nil
}
