package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) GetReputationAttestation(ctx context.Context, req *types.QueryGetReputationAttestationRequest) (*types.QueryGetReputationAttestationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	att, err := q.k.RepAttestations.Get(ctx, collections.Join(req.LocalAddress, req.PeerId))
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrAttestationNotFound, "no attestation for %s from peer %s", req.LocalAddress, req.PeerId)
	}

	return &types.QueryGetReputationAttestationResponse{Attestation: att}, nil
}
