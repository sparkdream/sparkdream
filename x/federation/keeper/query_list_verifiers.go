package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListVerifiers(ctx context.Context, req *types.QueryListVerifiersRequest) (*types.QueryListVerifiersResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	verifiers, pageRes, err := query.CollectionPaginate(ctx, q.k.Verifiers, req.Pagination, func(key string, value types.FederationVerifier) (types.FederationVerifier, error) {
		return value, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryListVerifiersResponse{
		Verifiers:  verifiers,
		Pagination: pageRes,
	}, nil
}
