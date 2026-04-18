package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
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

	activeReport := false
	report, err := q.k.MemberReport.Get(ctx, req.Member)
	if err == nil && report.Status == types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING {
		activeReport = true
	}

	var trustTier uint64
	if addrBytes, err := q.k.addressCodec.StringToBytes(req.Member); err == nil {
		trustTier, _ = q.k.GetReputationTier(ctx, sdk.AccAddress(addrBytes))
	}

	return &types.QueryMemberStandingResponse{
		WarningCount: warningCount,
		ActiveReport: activeReport,
		TrustTier:    trustTier,
	}, nil
}
