package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) MemberStanding(ctx context.Context, req *types.QueryMemberStandingRequest) (*types.QueryMemberStandingResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.Member == "" {
		return nil, status.Error(codes.InvalidArgument, "member address required")
	}

	// Count warnings for this member
	var warningCount uint64

	err := q.k.MemberWarning.Walk(ctx, nil, func(key uint64, warning types.MemberWarning) (bool, error) {
		if warning.Member == req.Member {
			warningCount++
		}
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Check for active reports
	activeReport := false
	report, err := q.k.MemberReport.Get(ctx, req.Member)
	if err == nil && report.Status == types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING {
		activeReport = true
	}

	// Get trust tier (from helper)
	trustTier := q.k.GetRepTier(ctx, req.Member)

	return &types.QueryMemberStandingResponse{
		WarningCount: warningCount,
		ActiveReport: activeReport,
		TrustTier:    trustTier,
	}, nil
}
