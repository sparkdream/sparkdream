package keeper

import (
	"context"

	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/artifact/types"
)

func (q queryServer) Outbox(ctx context.Context, req *types.QueryOutboxRequest) (*types.QueryOutboxResponse, error) {
	if req == nil || req.Address == "" {
		return nil, errInvalidRequest
	}
	items, page, err := query.CollectionFilteredPaginate(ctx, q.k.OutboxBySender, capPage(req.Pagination),
		func(key AddrTokenKey, kind uint64) (bool, error) {
			_, ok := q.inboxItem(ctx, key.K2(), key.K3(), types.InboxKind(kind))
			return ok, nil
		},
		func(key AddrTokenKey, kind uint64) (types.InboxItem, error) {
			item, _ := q.inboxItem(ctx, key.K2(), key.K3(), types.InboxKind(kind))
			return item, nil
		}, addrPrefix(req.Address))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryOutboxResponse{Items: items, Pagination: page}, nil
}
