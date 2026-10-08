package keeper

import (
	"context"
	"encoding/binary"

	"cosmossdk.io/store/prefix"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/types"
)

// TrustTree exports the trust tree root and its non-empty leaves in index
// order. Leaves are the level-0 nodes; empty ones are never stored (setNode
// deletes zero hashes), so the page holds exactly the occupied slots.
func (q queryServer) TrustTree(ctx context.Context, req *types.QueryTrustTreeRequest) (*types.QueryTrustTreeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	leafPrefix := append(append([]byte(nil), trustTreeNodePrefix...), 0)
	store := prefix.NewStore(runtime.KVStoreAdapter(q.k.storeService.OpenKVStore(ctx)), leafPrefix)

	var leaves []types.TrustTreeLeaf
	pageRes, err := query.Paginate(store, req.Pagination, func(key, value []byte) error {
		if len(key) != 8 {
			return status.Errorf(codes.Internal, "malformed trust tree leaf key %x", key)
		}
		leaves = append(leaves, types.TrustTreeLeaf{
			Index: binary.BigEndian.Uint64(key),
			Hash:  value,
		})
		return nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// A tree that hasn't been built yet has no root; report it as empty.
	root, _ := q.k.GetMemberTrustTreeRoot(ctx)

	return &types.QueryTrustTreeResponse{
		Root:       root,
		Depth:      uint32(trustTreeDepth),
		LeafCount:  q.k.peekNextLeafIndex(ctx),
		Leaves:     leaves,
		Pagination: pageRes,
	}, nil
}
