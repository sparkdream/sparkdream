package keeper

import (
	"context"

	"sparkdream/x/collect/types"

	query "github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) SponsorshipRequests(ctx context.Context, req *types.QuerySponsorshipRequestsRequest) (*types.QuerySponsorshipRequestsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	var results []types.SponsorshipRequest
	pageReq := req.Pagination
	if pageReq == nil {
		pageReq = &query.PageRequest{Limit: 100}
	}
	limit := pageReq.Limit
	if limit == 0 {
		limit = 100
	}
	offset := pageReq.Offset
	var count uint64

	err := q.k.SponsorshipRequest.Walk(ctx, nil,
		func(key uint64, val types.SponsorshipRequest) (bool, error) {
			if count < offset {
				count++
				return false, nil
			}
			if uint64(len(results)) >= limit {
				return true, nil
			}
			results = append(results, val)
			count++
			return false, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QuerySponsorshipRequestsResponse{
		SponsorshipRequests: results,
		Pagination:          &query.PageResponse{Total: count},
	}, nil
}
