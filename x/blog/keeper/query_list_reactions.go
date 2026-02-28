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

func (q queryServer) ListReactions(ctx context.Context, req *types.QueryListReactionsRequest) (*types.QueryListReactionsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	storeAdapter := runtime.KVStoreAdapter(q.k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte(types.ReactionKey))
	targetPrefix := append(GetPostIDBytes(req.PostId), GetReplyIDBytes(req.ReplyId)...)
	targetStore := prefix.NewStore(store, targetPrefix)

	var reactions []types.Reaction
	pageRes, err := query.Paginate(targetStore, req.Pagination, func(key []byte, value []byte) error {
		var reaction types.Reaction
		if err := q.k.cdc.Unmarshal(value, &reaction); err != nil {
			return err
		}
		reactions = append(reactions, reaction)
		return nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryListReactionsResponse{Reactions: reactions, Pagination: pageRes}, nil
}
