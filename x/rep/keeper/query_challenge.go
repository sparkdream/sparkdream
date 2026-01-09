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

func (q queryServer) ListChallenge(ctx context.Context, req *types.QueryAllChallengeRequest) (*types.QueryAllChallengeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	challenges, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.Challenge,
		req.Pagination,
		func(_ uint64, value types.Challenge) (types.Challenge, error) {
			return value, nil
		},
	)

	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllChallengeResponse{Challenge: challenges, Pagination: pageRes}, nil
}

func (q queryServer) GetChallenge(ctx context.Context, req *types.QueryGetChallengeRequest) (*types.QueryGetChallengeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	challenge, err := q.k.Challenge.Get(ctx, req.Id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, sdkerrors.ErrKeyNotFound
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetChallengeResponse{Challenge: challenge}, nil
}
