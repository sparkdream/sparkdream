package keeper

import (
	"context"
	"errors"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListInterimTemplate(ctx context.Context, req *types.QueryAllInterimTemplateRequest) (*types.QueryAllInterimTemplateResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	interimTemplates, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.InterimTemplate,
		req.Pagination,
		func(_ string, value types.InterimTemplate) (types.InterimTemplate, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllInterimTemplateResponse{InterimTemplate: interimTemplates, Pagination: pageRes}, nil
}

func (q queryServer) GetInterimTemplate(ctx context.Context, req *types.QueryGetInterimTemplateRequest) (*types.QueryGetInterimTemplateResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.InterimTemplate.Get(ctx, req.TemplateId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetInterimTemplateResponse{InterimTemplate: val}, nil
}
