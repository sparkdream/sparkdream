package keeper

import (
	"context"
	"encoding/binary"

	"sparkdream/x/blog/types"

	"cosmossdk.io/store/prefix"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListPostsByCreator(ctx context.Context, req *types.QueryListPostsByCreatorRequest) (*types.QueryListPostsByCreatorResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	storeAdapter := runtime.KVStoreAdapter(q.k.storeService.OpenKVStore(ctx))
	creatorStore := prefix.NewStore(storeAdapter, []byte(types.CreatorPostKey))
	creatorPrefix := []byte(req.Creator + "/")
	creatorPrefixStore := prefix.NewStore(creatorStore, creatorPrefix)

	var posts []types.Post
	pageRes, err := query.Paginate(creatorPrefixStore, req.Pagination, func(key []byte, value []byte) error {
		if len(key) < 8 {
			return nil
		}
		postId := binary.BigEndian.Uint64(key[:8])
		post, found := q.k.GetPost(ctx, postId)
		if !found {
			return nil
		}
		if post.Status == types.PostStatus_POST_STATUS_DELETED {
			return nil
		}
		if post.Status == types.PostStatus_POST_STATUS_HIDDEN && !req.IncludeHidden {
			return nil
		}
		posts = append(posts, post)
		return nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryListPostsByCreatorResponse{Posts: posts, Pagination: pageRes}, nil
}
