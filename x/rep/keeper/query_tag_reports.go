package keeper

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/types"
)

func (q queryServer) TagReports(ctx context.Context, req *types.QueryTagReportsRequest) (*types.QueryTagReportsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	var firstReport *types.TagReport

	err := q.k.TagReport.Walk(ctx, nil, func(_ string, report types.TagReport) (bool, error) {
		firstReport = &report
		return true, nil
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
