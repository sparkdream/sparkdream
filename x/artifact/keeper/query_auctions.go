package keeper

import (
	"context"

	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/artifact/types"
)

// Auctions lists open auctions, optionally of one class.
func (q queryServer) Auctions(ctx context.Context, req *types.QueryAuctionsRequest) (*types.QueryAuctionsResponse, error) {
	if req == nil {
		return nil, errInvalidRequest
	}
	auctions, page, err := query.CollectionFilteredPaginate(ctx, q.k.Auctions, capPage(req.Pagination),
		func(_ uint64, a types.Auction) (bool, error) {
			return req.ClassId == 0 || a.ClassId == req.ClassId, nil
		},
		func(_ uint64, a types.Auction) (types.Auction, error) { return a, nil })
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryAuctionsResponse{Auctions: auctions, Pagination: page}, nil
}
