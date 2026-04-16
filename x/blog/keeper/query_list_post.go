package keeper

import (
	"context"

	"sparkdream/x/blog/types"

	"cosmossdk.io/store/prefix"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListPost(ctx context.Context, req *types.QueryListPostRequest) (*types.QueryListPostResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	storeAdapter := runtime.KVStoreAdapter(q.k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte(types.PostKey))

	var posts []types.Post
	// NOTE: Known behavior — hidden posts are skipped inside the pagination callback,
	// so a page may return fewer results than the requested page size. The pagination
	// cursor (next_key) is still correct for fetching subsequent pages. Clients should
	// not rely on len(posts) == page_size to detect the last page; use the absence of
	// next_key in the pagination response instead.
	pageRes, err := query.Paginate(store, req.Pagination, func(key []byte, value []byte) error {
		var post types.Post
		if err := q.k.cdc.Unmarshal(value, &post); err != nil {
			return err
		}

		// Exclude hidden posts from public listing; tombstoned posts are included
		// (clients should check status field and display accordingly)
		if post.Status == types.PostStatus_POST_STATUS_HIDDEN {
			return nil
		}

		posts = append(posts, post)
		return nil
	})

	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryListPostResponse{Post: posts, Pagination: pageRes}, nil
}
