package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) MembersByTrustLevel(ctx context.Context, req *types.QueryMembersByTrustLevelRequest) (*types.QueryMembersByTrustLevelResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Collect first member matching the trust level (proto response is singular)
	var foundMember *types.Member
	err := q.k.Member.Walk(ctx, nil, func(address string, member types.Member) (bool, error) {
		if uint64(member.TrustLevel) == req.TrustLevel {
			foundMember = &member
			return true, nil // stop iteration
		}
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if foundMember != nil {
		return &types.QueryMembersByTrustLevelResponse{
			Address:      foundMember.Address,
			DreamBalance: foundMember.DreamBalance,
		}, nil
	}

	return &types.QueryMembersByTrustLevelResponse{}, nil
}
