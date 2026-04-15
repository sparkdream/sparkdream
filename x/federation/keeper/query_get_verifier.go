package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	errorsmod "cosmossdk.io/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) GetVerifier(ctx context.Context, req *types.QueryGetVerifierRequest) (*types.QueryGetVerifierResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	verifier, err := q.k.Verifiers.Get(ctx, req.Address)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrVerifierNotFound, "verifier %s not found", req.Address)
	}

	return &types.QueryGetVerifierResponse{Verifier: verifier}, nil
}
