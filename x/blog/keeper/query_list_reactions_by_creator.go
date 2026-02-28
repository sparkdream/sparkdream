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

func (q queryServer) ListReactionsByCreator(ctx context.Context, req *types.QueryListReactionsByCreatorRequest) (*types.QueryListReactionsByCreatorResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	storeAdapter := runtime.KVStoreAdapter(q.k.storeService.OpenKVStore(ctx))
	creatorStore := prefix.NewStore(storeAdapter, []byte(types.ReactionCreatorKey))
	creatorPrefixStore := prefix.NewStore(creatorStore, []byte(req.Creator))

	var reactions []types.Reaction
	pageRes, err := query.Paginate(creatorPrefixStore, req.Pagination, func(key []byte, value []byte) error {
		if len(key) < 16 {
			return nil
		}
		postId := binary.BigEndian.Uint64(key[:8])
		replyId := binary.BigEndian.Uint64(key[8:16])
		reaction, found := q.k.GetReaction(ctx, postId, replyId, req.Creator)
		if !found {
			return nil
		}
		reactions = append(reactions, reaction)
		return nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryListReactionsByCreatorResponse{Reactions: reactions, Pagination: pageRes}, nil
}
