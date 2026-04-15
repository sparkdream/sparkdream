package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListPeers(ctx context.Context, req *types.QueryListPeersRequest) (*types.QueryListPeersResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	peers, pageRes, err := query.CollectionPaginate(ctx, q.k.Peers, req.Pagination, func(key string, value types.Peer) (types.Peer, error) {
		return value, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryListPeersResponse{
		Peers:      peers,
		Pagination: pageRes,
	}, nil
}
