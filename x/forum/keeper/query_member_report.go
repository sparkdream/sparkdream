package keeper

import (
	"context"
	"errors"

	"sparkdream/x/forum/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListMemberReport(ctx context.Context, req *types.QueryAllMemberReportRequest) (*types.QueryAllMemberReportResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	memberReports, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.MemberReport,
		req.Pagination,
		func(_ string, value types.MemberReport) (types.MemberReport, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllMemberReportResponse{MemberReport: memberReports, Pagination: pageRes}, nil
}

func (q queryServer) GetMemberReport(ctx context.Context, req *types.QueryGetMemberReportRequest) (*types.QueryGetMemberReportResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.MemberReport.Get(ctx, req.Member)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetMemberReportResponse{MemberReport: val}, nil
}
