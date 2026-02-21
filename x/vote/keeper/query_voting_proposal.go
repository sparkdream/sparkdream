package keeper

import (
	"context"
	"errors"

	"sparkdream/x/vote/types"

	"cosmossdk.io/collections"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListVotingProposal(ctx context.Context, req *types.QueryAllVotingProposalRequest) (*types.QueryAllVotingProposalResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	votingProposals, pageRes, err := query.CollectionPaginate(
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

	return &types.QueryAllVotingProposalResponse{VotingProposal: votingProposals, Pagination: pageRes}, nil
}

func (q queryServer) GetVotingProposal(ctx context.Context, req *types.QueryGetVotingProposalRequest) (*types.QueryGetVotingProposalResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	votingProposal, err := q.k.VotingProposal.Get(ctx, req.Id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, sdkerrors.ErrKeyNotFound
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetVotingProposalResponse{VotingProposal: votingProposal}, nil
}
