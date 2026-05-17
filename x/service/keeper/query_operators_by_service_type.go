package keeper

import (
	"context"

	"sparkdream/x/service/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// OperatorsByServiceType returns the paginated list of live operators
// of the given service type, optionally filtered by status. See the
// note on OperatorsByController re: in-loop filter vs secondary index.
func (q queryServer) OperatorsByServiceType(ctx context.Context, req *types.QueryOperatorsByServiceTypeRequest) (*types.QueryOperatorsByServiceTypeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.ServiceType == "" {
		return nil, status.Error(codes.InvalidArgument, "service_type required")
	}

	filter, err := parseStatusFilter(req.StatusFilter)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	operators, pageRes, err := query.CollectionFilteredPaginate(
		ctx,
		q.k.Operators,
		req.Pagination,
		func(key collections.Pair[[]byte, string], op types.Operator) (bool, error) {
			if key.K2() != req.ServiceType {
				return false, nil
			}
			if filter == types.OperatorStatus_OPERATOR_STATUS_UNSPECIFIED {
				return true, nil
			}
			return op.Status == filter, nil
		},
		func(_ collections.Pair[[]byte, string], op types.Operator) (types.Operator, error) {
			return op, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryOperatorsByServiceTypeResponse{Operators: operators, Pagination: pageRes}, nil
}
