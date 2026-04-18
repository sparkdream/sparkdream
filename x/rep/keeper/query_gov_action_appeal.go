package keeper

import (
	"context"
	"errors"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListGovActionAppeal(ctx context.Context, req *types.QueryAllGovActionAppealRequest) (*types.QueryAllGovActionAppealResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	govActionAppeals, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.GovActionAppeal,
		req.Pagination,
		func(_ uint64, value types.GovActionAppeal) (types.GovActionAppeal, error) {
			return value, nil
		},
	)

	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllGovActionAppealResponse{GovActionAppeal: govActionAppeals, Pagination: pageRes}, nil
}

func (q queryServer) GetGovActionAppeal(ctx context.Context, req *types.QueryGetGovActionAppealRequest) (*types.QueryGetGovActionAppealResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	govActionAppeal, err := q.k.GovActionAppeal.Get(ctx, req.Id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, sdkerrors.ErrKeyNotFound
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetGovActionAppealResponse{GovActionAppeal: govActionAppeal}, nil
}
