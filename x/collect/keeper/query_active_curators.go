package keeper

import (
	"context"

	"sparkdream/x/collect/types"

	query "github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ActiveCurators(ctx context.Context, req *types.QueryActiveCuratorsRequest) (*types.QueryActiveCuratorsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	var results []types.Curator
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

	err := q.k.Curator.Walk(ctx, nil,
		func(key string, val types.Curator) (bool, error) {
			if !val.Active {
				return false, nil
			}
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

	return &types.QueryActiveCuratorsResponse{
		Curators:   results,
		Pagination: &query.PageResponse{Total: count},
	}, nil
}
