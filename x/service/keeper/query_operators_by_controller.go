package keeper

import (
	"context"

	"sparkdream/x/service/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// OperatorsByController returns the paginated list of live operators
// whose controller is the given address, optionally filtered by status.
//
// Implementation note: paginates over the primary Operators store with
// an in-loop controller filter. The OperatorsByController KeySet exists
// as a secondary index but Cosmos SDK's query.CollectionFilteredPaginate
// doesn't support prefix-scoped pagination on a Triple key directly;
// switching to the index would require manual paging. For early dev the
// linear scan is acceptable — revisit if operator count grows large.
func (q queryServer) OperatorsByController(ctx context.Context, req *types.QueryOperatorsByControllerRequest) (*types.QueryOperatorsByControllerResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if _, err := q.k.addrBytes(req.Controller); err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid controller address")
	}

	filter, err := parseStatusFilter(req.StatusFilter)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	operators, pageRes, err := query.CollectionFilteredPaginate(
		ctx,
		q.k.Operators,
		req.Pagination,
		func(_ collections.Pair[[]byte, string], op types.Operator) (bool, error) {
			if op.Controller != req.Controller {
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

	return &types.QueryOperatorsByControllerResponse{Operators: operators, Pagination: pageRes}, nil
}
