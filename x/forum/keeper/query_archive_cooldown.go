package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ArchiveCooldown(ctx context.Context, req *types.QueryArchiveCooldownRequest) (*types.QueryArchiveCooldownResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.RootId == 0 {
		return nil, status.Error(codes.InvalidArgument, "root_id required")
	}

	// Get the archive metadata
	metadata, err := q.k.ArchiveMetadata.Get(ctx, req.RootId)
	if err != nil {
		// No metadata means no cooldown
		return &types.QueryArchiveCooldownResponse{
			InCooldown:   false,
			CooldownEnds: 0,
		}, nil
	}

	// Calculate cooldown end
	params, err := q.k.Params.Get(ctx)
	if err != nil {
		params = types.DefaultParams()
	}
	archiveCooldown := params.ArchiveCooldown
	if archiveCooldown == 0 {
		archiveCooldown = types.DefaultArchiveCooldown
	}
	cooldownEnds := metadata.LastArchivedAt + archiveCooldown

	// Check if still in cooldown
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()
	inCooldown := now < cooldownEnds

	return &types.QueryArchiveCooldownResponse{
		InCooldown:   inCooldown,
		CooldownEnds: cooldownEnds,
	}, nil
}
