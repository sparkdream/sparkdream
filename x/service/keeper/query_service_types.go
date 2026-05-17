package keeper

import (
	"context"

	"sparkdream/x/service/types"

	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ServiceTypes returns the paginated list of service-type registry
// entries, optionally filtered to only enabled ones.
func (q queryServer) ServiceTypes(ctx context.Context, req *types.QueryServiceTypesRequest) (*types.QueryServiceTypesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	configs, pageRes, err := query.CollectionFilteredPaginate(
		ctx,
		q.k.ServiceTypes,
		req.Pagination,
		func(_ string, cfg types.ServiceTypeConfig) (bool, error) {
			if req.EnabledOnly && !cfg.Enabled {
				return false, nil
			}
			return true, nil
		},
		func(_ string, cfg types.ServiceTypeConfig) (types.ServiceTypeConfig, error) {
			return cfg, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryServiceTypesResponse{Configs: configs, Pagination: pageRes}, nil
}
