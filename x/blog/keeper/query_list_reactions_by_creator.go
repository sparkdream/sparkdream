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

	collect := func(reactions []types.Reaction, creator string, key []byte) []types.Reaction {
		if len(key) < 16 {
			return reactions
		}
		postId := binary.BigEndian.Uint64(key[:8])
		replyId := binary.BigEndian.Uint64(key[8:16])
		reaction, found := q.k.GetReaction(ctx, postId, replyId, creator)
		if !found {
			return reactions
		}
		return append(reactions, reaction)
	}

	// Alias contract: the canonical address is paginated as before; reactions
	// stored under the member's past addresses are appended in full after it.
	aliases := q.k.allAddressesOf(ctx, req.Creator)
	creatorPrefixStore := prefix.NewStore(creatorStore, []byte(aliases[0]))

	var reactions []types.Reaction
	pageRes, err := query.Paginate(creatorPrefixStore, req.Pagination, func(key []byte, value []byte) error {
		reactions = collect(reactions, aliases[0], key)
		return nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	for _, alias := range aliases[1:] {
		aliasStore := prefix.NewStore(creatorStore, []byte(alias))
		it := aliasStore.Iterator(nil, nil)
		for ; it.Valid(); it.Next() {
			reactions = collect(reactions, alias, it.Key())
		}
		it.Close()
	}

	return &types.QueryListReactionsByCreatorResponse{Reactions: reactions, Pagination: pageRes}, nil
}
