package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) MemberReports(ctx context.Context, req *types.QueryMemberReportsRequest) (*types.QueryMemberReportsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	var firstReport *types.MemberReport

	err := q.k.MemberReport.Walk(ctx, nil, func(key string, report types.MemberReport) (bool, error) {
		firstReport = &report
		return true, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if firstReport != nil {
		return &types.QueryMemberReportsResponse{
			Member: firstReport.Member,
			Status: uint64(firstReport.Status),
		}, nil
	}

	return &types.QueryMemberReportsResponse{}, nil
}
