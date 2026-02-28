package keeper

import (
	"context"

	"sparkdream/x/season/types"

	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ListNominations returns all nominations with pagination.
func (q queryServer) ListNominations(ctx context.Context, req *types.QueryListNominationsRequest) (*types.QueryListNominationsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	nominations, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.Nomination,
		req.Pagination,
		func(_ uint64, value types.Nomination) (types.Nomination, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryListNominationsResponse{
		Nominations: nominations,
		Pagination:  pageRes,
	}, nil
}
