package keeper

import (
	"context"
	"errors"

	"sparkdream/x/commons/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GetDecisionPolicy returns the DecisionPolicy registered against a given
// council policy address. Returns codes.NotFound if no record exists for the
// address — callers should treat that as "this policy address is not the
// standard or veto policy of any active council" rather than as an error.
func (q queryServer) GetDecisionPolicy(ctx context.Context, req *types.QueryGetDecisionPolicyRequest) (*types.QueryGetDecisionPolicyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.DecisionPolicies.Get(ctx, req.PolicyAddress)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}
		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetDecisionPolicyResponse{DecisionPolicy: val}, nil
}

// ListDecisionPolicies paginates over every (policy_address, DecisionPolicy)
// pair in storage. Each entry includes its key so consumers can match the
// DecisionPolicy back to the council that owns it (a single council has both
// a standard and a veto policy address, each with its own DecisionPolicy).
func (q queryServer) ListDecisionPolicies(ctx context.Context, req *types.QueryAllDecisionPoliciesRequest) (*types.QueryAllDecisionPoliciesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	entries, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.DecisionPolicies,
		req.Pagination,
		func(key string, value types.DecisionPolicy) (types.DecisionPolicyEntry, error) {
			return types.DecisionPolicyEntry{
				PolicyAddress:  key,
				DecisionPolicy: value,
			}, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllDecisionPoliciesResponse{Entries: entries, Pagination: pageRes}, nil
}
