package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) AppealCooldown(ctx context.Context, req *types.QueryAppealCooldownRequest) (*types.QueryAppealCooldownResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.PostId == 0 {
		return nil, status.Error(codes.InvalidArgument, "post_id required")
	}

	// Get the hide record for this post
	hideRecord, err := q.k.HideRecord.Get(ctx, req.PostId)
	if err != nil {
		// No hide record means no cooldown
		return &types.QueryAppealCooldownResponse{
			InCooldown:   false,
			CooldownEnds: 0,
		}, nil
	}

	// Load params for configurable cooldown
	params, err := q.k.Params.Get(ctx)
	if err != nil {
		params = types.DefaultParams()
	}

	// Calculate cooldown end
	cooldownEnds := hideRecord.HiddenAt + params.HideAppealCooldown

	// Check if still in cooldown
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()
	inCooldown := now < cooldownEnds

	return &types.QueryAppealCooldownResponse{
		InCooldown:   inCooldown,
		CooldownEnds: cooldownEnds,
	}, nil
}
