package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) UserPosts(ctx context.Context, req *types.QueryUserPostsRequest) (*types.QueryUserPostsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.Author == "" {
		return nil, status.Error(codes.InvalidArgument, "author address required")
	}

	// Posts written from any of the member's past addresses belong to the
	// same author (alias contract). The filter already scans in memory, so
	// matching against the full alias set keeps pagination unchanged.
	authorAliases := q.k.allAddressesOf(ctx, req.Author)

	posts, pageRes, err := query.CollectionFilteredPaginate(
		ctx,
		q.k.Post,
		req.Pagination,
		func(_ uint64, post types.Post) (bool, error) {
			return addressesMatch(authorAliases, post.Author), nil
		},
		func(_ uint64, post types.Post) (types.Post, error) {
			return withholdPostContent(post), nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryUserPostsResponse{Posts: posts, Pagination: pageRes}, nil
}
