package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) BountyByThread(ctx context.Context, req *types.QueryBountyByThreadRequest) (*types.QueryBountyByThreadResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.ThreadId == 0 {
		return nil, status.Error(codes.InvalidArgument, "thread_id required")
	}

	// Find bounty for this thread
	var foundBounty *types.Bounty

	err := q.k.Bounty.Walk(ctx, nil, func(key uint64, bounty types.Bounty) (bool, error) {
		if bounty.ThreadId == req.ThreadId {
			foundBounty = &bounty
			return true, nil // Stop after first
		}
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if foundBounty != nil {
		return &types.QueryBountyByThreadResponse{
			BountyId: foundBounty.Id,
			Amount:   foundBounty.Amount,
			Status:   uint64(foundBounty.Status),
		}, nil
	}

	return &types.QueryBountyByThreadResponse{}, nil
}
