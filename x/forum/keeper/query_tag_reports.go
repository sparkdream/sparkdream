package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) TagReports(ctx context.Context, req *types.QueryTagReportsRequest) (*types.QueryTagReportsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Get first tag report (simplified - in production would return list with pagination)
	var firstReport *types.TagReport

	err := q.k.TagReport.Walk(ctx, nil, func(key string, report types.TagReport) (bool, error) {
		firstReport = &report
		return true, nil // Stop after first
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if firstReport != nil {
		return &types.QueryTagReportsResponse{
			TagName:     firstReport.TagName,
			UnderReview: firstReport.UnderReview,
		}, nil
	}

	return &types.QueryTagReportsResponse{}, nil
}
