package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/cosmos/cosmos-sdk/types/query"
)

func (q queryServer) GetContentChallenge(ctx context.Context, req *types.QueryGetContentChallengeRequest) (*types.QueryGetContentChallengeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	cc, err := q.k.ContentChallenge.Get(ctx, req.Id)
	if err != nil {
		return nil, status.Error(codes.NotFound, "content challenge not found")
	}

	return &types.QueryGetContentChallengeResponse{ContentChallenge: cc}, nil
}

func (q queryServer) ListContentChallenge(ctx context.Context, req *types.QueryAllContentChallengeRequest) (*types.QueryAllContentChallengeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	challenges, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.ContentChallenge,
		req.Pagination,
		func(_ uint64, value types.ContentChallenge) (types.ContentChallenge, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllContentChallengeResponse{
		ContentChallenge: challenges,
		Pagination:       pageRes,
	}, nil
}

func (q queryServer) ContentChallengesByTarget(ctx context.Context, req *types.QueryContentChallengesByTargetRequest) (*types.QueryContentChallengesByTargetResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	targetKey := collections.Join(int32(req.TargetType), req.TargetId)
	ccID, err := q.k.ContentChallengesByTarget.Get(ctx, targetKey)
	if err != nil {
		return nil, status.Error(codes.NotFound, "no active content challenge for this target")
	}

	cc, err := q.k.ContentChallenge.Get(ctx, ccID)
	if err != nil {
		return nil, status.Error(codes.NotFound, "content challenge not found")
	}

	return &types.QueryContentChallengesByTargetResponse{ContentChallenge: cc}, nil
}
