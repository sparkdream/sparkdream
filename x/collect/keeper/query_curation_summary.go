package keeper

import (
	"context"

	"sparkdream/x/collect/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) CurationSummary(ctx context.Context, req *types.QueryCurationSummaryRequest) (*types.QueryCurationSummaryResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	summary, err := q.k.CurationSummary.Get(ctx, req.CollectionId)
	if err != nil {
		return nil, status.Error(codes.NotFound, "curation summary not found")
	}

	return &types.QueryCurationSummaryResponse{Summary: summary}, nil
}
