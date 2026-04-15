package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListOutboundAttestations(ctx context.Context, req *types.QueryListOutboundAttestationsRequest) (*types.QueryListOutboundAttestationsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	attestations, pageRes, err := query.CollectionPaginate(ctx, q.k.OutboundAttestations, req.Pagination, func(key uint64, value types.OutboundAttestation) (types.OutboundAttestation, error) {
		return value, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryListOutboundAttestationsResponse{
		Attestations: attestations,
		Pagination:   pageRes,
	}, nil
}
