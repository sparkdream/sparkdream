package keeper

import (
	"context"
	"errors"

	"sparkdream/x/season/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListVoteXpRecord(ctx context.Context, req *types.QueryAllVoteXpRecordRequest) (*types.QueryAllVoteXpRecordResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	voteXpRecords, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.VoteXpRecord,
		req.Pagination,
		func(_ string, value types.VoteXpRecord) (types.VoteXpRecord, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllVoteXpRecordResponse{VoteXpRecord: voteXpRecords, Pagination: pageRes}, nil
}

func (q queryServer) GetVoteXpRecord(ctx context.Context, req *types.QueryGetVoteXpRecordRequest) (*types.QueryGetVoteXpRecordResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.VoteXpRecord.Get(ctx, req.SeasonMemberProposal)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetVoteXpRecordResponse{VoteXpRecord: val}, nil
}
