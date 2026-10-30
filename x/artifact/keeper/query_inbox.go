package keeper

import (
	"context"
	"cosmossdk.io/collections"
	"errors"

	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/artifact/types"
	commontypes "sparkdream/x/common/types"
)

func (q queryServer) Inbox(ctx context.Context, req *types.QueryInboxRequest) (*types.QueryInboxResponse, error) {
	if req == nil || req.Address == "" {
		return nil, errInvalidRequest
	}
	addrs := q.k.allAddressesOf(ctx, req.Address)
	items, page, err := query.CollectionFilteredPaginate(ctx, q.k.InboxByRecipient, capPage(req.Pagination),
		func(key AddrTokenKey, kind uint64) (bool, error) {
			_, ok := q.inboxItem(ctx, key.K2(), key.K3(), commontypes.Saturate[types.InboxKind](kind))
			return ok, nil
		},
		func(key AddrTokenKey, kind uint64) (types.InboxItem, error) {
			item, _ := q.inboxItem(ctx, key.K2(), key.K3(), commontypes.Saturate[types.InboxKind](kind))
			return item, nil
		}, addrPrefix(addrs[0]))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if lastPage(page) {
		for _, a := range addrs[1:] {
			err = q.k.InboxByRecipient.Walk(ctx, addrRange(a, 0), func(key AddrTokenKey, kind uint64) (bool, error) {
				if item, ok := q.inboxItem(ctx, key.K2(), key.K3(), commontypes.Saturate[types.InboxKind](kind)); ok {
					items = append(items, item)
				}
				return false, nil
			})
			if err != nil {
				return nil, status.Error(codes.Internal, err.Error())
			}
		}
	}
	count, getErr := q.k.InboxCount.Get(ctx, req.Address)
	if getErr != nil && !errors.Is(getErr, collections.ErrNotFound) {
		return nil, getErr
	}
	return &types.QueryInboxResponse{Items: items, InboxCount: commontypes.Saturate[uint32](count), Pagination: page}, nil
}
