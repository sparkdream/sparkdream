package keeper

import (
	"context"
	"encoding/binary"

	"cosmossdk.io/store/prefix"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/blog/types"
)

// ListPostsByTag returns paginated posts that carry the given tag. Uses the
// tag → postID secondary index maintained by Create/Update/Delete — tombstoned
// posts are not indexed so they are naturally excluded.
func (q queryServer) ListPostsByTag(ctx context.Context, req *types.QueryListPostsByTagRequest) (*types.QueryListPostsByTagResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if req.Tag == "" {
		return nil, status.Error(codes.InvalidArgument, "tag cannot be empty")
	}

	storeAdapter := runtime.KVStoreAdapter(q.k.storeService.OpenKVStore(ctx))
	tagStore := prefix.NewStore(storeAdapter, []byte(types.TagPostKey))
	tagPrefix := []byte(req.Tag + "/")
	tagPrefixStore := prefix.NewStore(tagStore, tagPrefix)

	var posts []types.Post
	pageRes, err := query.Paginate(tagPrefixStore, req.Pagination, func(key []byte, _ []byte) error {
		if len(key) < 8 {
			return nil
		}
		postID := binary.BigEndian.Uint64(key[:8])
		post, found := q.k.GetPost(ctx, postID)
		if !found {
			return nil
		}
		if post.Status == types.PostStatus_POST_STATUS_DELETED {
			return nil
		}
		if post.Status == types.PostStatus_POST_STATUS_HIDDEN {
			return nil
		}
		posts = append(posts, post)
		return nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryListPostsByTagResponse{Posts: posts, Pagination: pageRes}, nil
}
