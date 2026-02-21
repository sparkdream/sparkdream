package keeper

import (
	"context"
	"encoding/hex"

	"sparkdream/x/vote/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) NullifierUsed(ctx context.Context, req *types.QueryNullifierUsedRequest) (*types.QueryNullifierUsedResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	nullifierBytes, err := hex.DecodeString(req.Nullifier)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid nullifier hex")
	}

	key := nullifierKey(req.ProposalId, nullifierBytes)
	nullifier, err := q.k.UsedNullifier.Get(ctx, key)
	if err != nil {
		return &types.QueryNullifierUsedResponse{Used: false}, nil
	}

	return &types.QueryNullifierUsedResponse{
		Used:   true,
		UsedAt: nullifier.UsedAt,
	}, nil
}
