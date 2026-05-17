package keeper

import (
	"context"
	"strings"

	"sparkdream/x/service/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Operators returns all live operator records (paginated, optionally
// filtered by status_filter).
func (q queryServer) Operators(ctx context.Context, req *types.QueryOperatorsRequest) (*types.QueryOperatorsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
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

	return &types.QueryOperatorsResponse{Operators: operators, Pagination: pageRes}, nil
}

// parseStatusFilter maps a case-insensitive string like "active" or
// "OPERATOR_STATUS_ACTIVE" to an OperatorStatus enum. Empty → UNSPECIFIED
// (no filter).
func parseStatusFilter(s string) (types.OperatorStatus, error) {
	if s == "" {
		return types.OperatorStatus_OPERATOR_STATUS_UNSPECIFIED, nil
	}
	upper := strings.ToUpper(s)
	if !strings.HasPrefix(upper, "OPERATOR_STATUS_") {
		upper = "OPERATOR_STATUS_" + upper
	}
	if v, ok := types.OperatorStatus_value[upper]; ok {
		return types.OperatorStatus(v), nil
	}
	return types.OperatorStatus_OPERATOR_STATUS_UNSPECIFIED, status.Errorf(codes.InvalidArgument, "unknown status_filter %q", s)
}
