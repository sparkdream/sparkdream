package keeper

import (
	"context"

	"sparkdream/x/service/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Report returns a single report by ID.
func (q queryServer) Report(ctx context.Context, req *types.QueryReportRequest) (*types.QueryReportResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	report, err := q.k.Reports.Get(ctx, req.ReportId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "report %d not found", req.ReportId)
	}

	return &types.QueryReportResponse{Report: report}, nil
}
