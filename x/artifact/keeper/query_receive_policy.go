package keeper

import (
	"context"

	"sparkdream/x/artifact/types"
)

func (q queryServer) ReceivePolicy(ctx context.Context, req *types.QueryReceivePolicyRequest) (*types.QueryReceivePolicyResponse, error) {
	if req == nil || req.Address == "" {
		return nil, errInvalidRequest
	}
	policy, isDefault := q.k.EffectiveReceivePolicy(ctx, req.Address)
	return &types.QueryReceivePolicyResponse{Policy: policy, IsDefault: isDefault}, nil
}
