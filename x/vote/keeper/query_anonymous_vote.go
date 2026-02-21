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

func (q queryServer) ListAnonymousVote(ctx context.Context, req *types.QueryAllAnonymousVoteRequest) (*types.QueryAllAnonymousVoteResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	anonymousVotes, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.AnonymousVote,
		req.Pagination,
		func(_ string, value types.AnonymousVote) (types.AnonymousVote, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllAnonymousVoteResponse{AnonymousVote: anonymousVotes, Pagination: pageRes}, nil
}

func (q queryServer) GetAnonymousVote(ctx context.Context, req *types.QueryGetAnonymousVoteRequest) (*types.QueryGetAnonymousVoteResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.AnonymousVote.Get(ctx, req.Index)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetAnonymousVoteResponse{AnonymousVote: val}, nil
}
