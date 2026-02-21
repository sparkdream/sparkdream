package keeper

import (
	"context"
	"encoding/hex"

	"sparkdream/x/vote/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ProposalNullifierUsed(ctx context.Context, req *types.QueryProposalNullifierUsedRequest) (*types.QueryProposalNullifierUsedResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	nullifierBytes, err := hex.DecodeString(req.Nullifier)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid nullifier hex")
	}

	key := proposalNullifierKey(req.Epoch, nullifierBytes)
	pn, err := q.k.UsedProposalNullifier.Get(ctx, key)
	if err != nil {
		return &types.QueryProposalNullifierUsedResponse{Used: false}, nil
	}

	return &types.QueryProposalNullifierUsedResponse{
		Used:   true,
		UsedAt: pn.UsedAt,
	}, nil
}
