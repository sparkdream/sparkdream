package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/vote/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) EpochDecryptionKeyQuery(ctx context.Context, req *types.QueryEpochDecryptionKeyQueryRequest) (*types.QueryEpochDecryptionKeyQueryResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	params, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to get params")
	}

	// Count shares received for this epoch.
	var sharesReceived uint64
	prefix := fmt.Sprintf("/")
	_ = prefix
	_ = q.k.TleDecryptionShare.Walk(ctx, nil, func(_ string, share types.TleDecryptionShare) (bool, error) {
		if share.Epoch == req.Epoch {
			sharesReceived++
		}
		return false, nil
	})

	// Compute threshold needed.
	var sharesNeeded uint64
	if params.TleThresholdDenominator > 0 {
		// Count total registered validators.
		var totalValidators uint64
		_ = q.k.TleValidatorShare.Walk(ctx, nil, func(_ string, _ types.TleValidatorShare) (bool, error) {
			totalValidators++
			return false, nil
		})
		sharesNeeded = (totalValidators*uint64(params.TleThresholdNumerator) + uint64(params.TleThresholdDenominator) - 1) / uint64(params.TleThresholdDenominator)
	}

	epochKey, err := q.k.EpochDecryptionKey.Get(ctx, req.Epoch)
	available := err == nil && len(epochKey.DecryptionKey) > 0

	var decryptionKey []byte
	if available {
		decryptionKey = epochKey.DecryptionKey
	}

	return &types.QueryEpochDecryptionKeyQueryResponse{
		Epoch:          req.Epoch,
		Available:      available,
		DecryptionKey:  decryptionKey,
		SharesReceived: sharesReceived,
		SharesNeeded:   sharesNeeded,
	}, nil
}
