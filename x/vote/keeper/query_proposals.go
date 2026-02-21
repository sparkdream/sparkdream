package keeper

import (
	"context"

	"sparkdream/x/vote/types"

	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) Proposals(ctx context.Context, req *types.QueryProposalsRequest) (*types.QueryProposalsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	proposals, pageResp, err := query.CollectionPaginate(
		ctx,
		q.k.VotingProposal,
		req.Pagination,
		func(_ uint64, value types.VotingProposal) (types.VotingProposal, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryProposalsResponse{
		Proposals:  proposals,
		Pagination: pageResp,
	}, nil
}
