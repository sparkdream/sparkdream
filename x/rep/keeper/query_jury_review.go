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

func (q queryServer) ListJuryReview(ctx context.Context, req *types.QueryAllJuryReviewRequest) (*types.QueryAllJuryReviewResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	juryReviews, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.JuryReview,
		req.Pagination,
		func(_ uint64, value types.JuryReview) (types.JuryReview, error) {
			return value, nil
		},
	)

	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllJuryReviewResponse{JuryReview: juryReviews, Pagination: pageRes}, nil
}

func (q queryServer) GetJuryReview(ctx context.Context, req *types.QueryGetJuryReviewRequest) (*types.QueryGetJuryReviewResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	juryReview, err := q.k.JuryReview.Get(ctx, req.Id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, sdkerrors.ErrKeyNotFound
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetJuryReviewResponse{JuryReview: juryReview}, nil
}
