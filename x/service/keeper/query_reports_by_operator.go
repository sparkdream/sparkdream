package keeper

import (
	"context"
	"strings"

	"sparkdream/x/service/types"

	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ReportsByOperator returns the paginated list of reports against
// (operator_address, service_type), optionally filtered by report
// status.
func (q queryServer) ReportsByOperator(ctx context.Context, req *types.QueryReportsByOperatorRequest) (*types.QueryReportsByOperatorResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if req.OperatorAddress == "" || req.ServiceType == "" {
		return nil, status.Error(codes.InvalidArgument, "operator_address and service_type required")
	}

	filter, err := parseReportStatusFilter(req.StatusFilter)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	reports, pageRes, err := query.CollectionFilteredPaginate(
		ctx,
		q.k.Reports,
		req.Pagination,
		func(_ uint64, r types.Report) (bool, error) {
			if r.OperatorAddress != req.OperatorAddress || r.ServiceType != req.ServiceType {
				return false, nil
			}
			if filter == types.ReportStatus_REPORT_STATUS_UNSPECIFIED {
				return true, nil
			}
			return r.Status == filter, nil
		},
		func(_ uint64, r types.Report) (types.Report, error) {
			return r, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryReportsByOperatorResponse{Reports: reports, Pagination: pageRes}, nil
}

// parseReportStatusFilter mirrors parseStatusFilter but for ReportStatus.
func parseReportStatusFilter(s string) (types.ReportStatus, error) {
	if s == "" {
		return types.ReportStatus_REPORT_STATUS_UNSPECIFIED, nil
	}
	upper := strings.ToUpper(s)
	if !strings.HasPrefix(upper, "REPORT_STATUS_") {
		upper = "REPORT_STATUS_" + upper
	}
	if v, ok := types.ReportStatus_value[upper]; ok {
		return types.ReportStatus(v), nil
	}
	return types.ReportStatus_REPORT_STATUS_UNSPECIFIED, status.Errorf(codes.InvalidArgument, "unknown status_filter %q", s)
}
