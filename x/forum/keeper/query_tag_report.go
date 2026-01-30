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

func (q queryServer) ListTagReport(ctx context.Context, req *types.QueryAllTagReportRequest) (*types.QueryAllTagReportResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	tagReports, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.TagReport,
		req.Pagination,
		func(_ string, value types.TagReport) (types.TagReport, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllTagReportResponse{TagReport: tagReports, Pagination: pageRes}, nil
}

func (q queryServer) GetTagReport(ctx context.Context, req *types.QueryGetTagReportRequest) (*types.QueryGetTagReportResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.TagReport.Get(ctx, req.TagName)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetTagReportResponse{TagReport: val}, nil
}
