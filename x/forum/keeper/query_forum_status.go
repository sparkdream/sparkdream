package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ForumStatus(ctx context.Context, req *types.QueryForumStatusRequest) (*types.QueryForumStatusResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Get params for pause status
	params, err := q.k.Params.Get(ctx)
	if err != nil {
		// No params set yet, assume not paused
		params = types.DefaultParams()
	}

	// Calculate current epoch (24h epochs)
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()
	epochDuration := int64(86400) // 24 hours
	currentEpoch := now / epochDuration

	return &types.QueryForumStatusResponse{
		ForumPaused:      params.ForumPaused,
		ModerationPaused: params.ModerationPaused,
		CurrentEpoch:     currentEpoch,
	}, nil
}
