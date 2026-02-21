package keeper

import (
	"context"

	"sparkdream/x/vote/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) TleStatus(ctx context.Context, req *types.QueryTleStatusRequest) (*types.QueryTleStatusResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	params, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to get params")
	}

	currentEpoch := q.k.seasonKeeper.GetCurrentEpoch(ctx)
	if currentEpoch < 0 {
		currentEpoch = 0
	}

	// Find the latest epoch for which a decryption key is available.
	var latestAvailableEpoch uint64
	_ = q.k.EpochDecryptionKey.Walk(ctx, nil, func(epoch uint64, _ types.EpochDecryptionKey) (bool, error) {
		if epoch > latestAvailableEpoch {
			latestAvailableEpoch = epoch
		}
		return false, nil
	})

	return &types.QueryTleStatusResponse{
		TleEnabled:           params.TleEnabled,
		CurrentEpoch:         uint64(currentEpoch),
		LatestAvailableEpoch: latestAvailableEpoch,
		MasterPublicKey:      params.TleMasterPublicKey,
	}, nil
}
