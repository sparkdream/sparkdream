package keeper

import (
	"context"
	"errors"

	"sparkdream/x/vote/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListSealedVote(ctx context.Context, req *types.QueryAllSealedVoteRequest) (*types.QueryAllSealedVoteResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	sealedVotes, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.SealedVote,
		req.Pagination,
		func(_ string, value types.SealedVote) (types.SealedVote, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllSealedVoteResponse{SealedVote: sealedVotes, Pagination: pageRes}, nil
}

func (q queryServer) GetSealedVote(ctx context.Context, req *types.QueryGetSealedVoteRequest) (*types.QueryGetSealedVoteResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.SealedVote.Get(ctx, req.Index)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetSealedVoteResponse{SealedVote: val}, nil
}
