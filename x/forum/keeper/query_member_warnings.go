package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) MemberWarnings(ctx context.Context, req *types.QueryMemberWarningsRequest) (*types.QueryMemberWarningsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.Member == "" {
		return nil, status.Error(codes.InvalidArgument, "member address required")
	}

	// Find first warning for this member (simplified - in production would return list)
	var memberWarning *types.MemberWarning

	err := q.k.MemberWarning.Walk(ctx, nil, func(key uint64, warning types.MemberWarning) (bool, error) {
		if warning.Member == req.Member {
			memberWarning = &warning
			return true, nil // Stop after first
		}
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if memberWarning != nil {
		return &types.QueryMemberWarningsResponse{
			WarningNumber: memberWarning.WarningNumber,
			Reason:        memberWarning.Reason,
			IssuedAt:      memberWarning.IssuedAt,
		}, nil
	}

	return &types.QueryMemberWarningsResponse{}, nil
}
