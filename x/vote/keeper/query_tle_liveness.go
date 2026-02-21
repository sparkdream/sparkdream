package keeper

import (
	"context"
	"errors"

	"sparkdream/x/vote/types"

	"cosmossdk.io/collections"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TleLiveness returns liveness summary for all registered TLE validators.
// Reads from persisted TleValidatorLiveness records (O(validators), not O(window * validators)).
func (q queryServer) TleLiveness(ctx context.Context, req *types.QueryTleLivenessRequest) (*types.QueryTleLivenessResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	params, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to get params")
	}

	// Read persisted liveness records.
	var validators []types.TleValidatorLivenessInfo
	var windowSize uint32
	err = q.k.TleValidatorLiveness.Walk(ctx, nil, func(_ string, record types.TleValidatorLiveness) (bool, error) {
		var participated uint32
		if record.WindowSize > record.MissedCount {
			participated = record.WindowSize - record.MissedCount
		}
		validators = append(validators, types.TleValidatorLivenessInfo{
			Validator:          record.Validator,
			MissedEpochs:       record.MissedCount,
			ParticipatedEpochs: participated,
			WindowSize:         record.WindowSize,
			OverTolerance:      record.MissedCount > params.TleMissTolerance,
			FlaggedAt:          record.FlaggedAt,
			RecoveredAt:        record.RecoveredAt,
		})
		windowSize = record.WindowSize
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryTleLivenessResponse{
		Validators:    validators,
		WindowSize:    windowSize,
		MissTolerance: params.TleMissTolerance,
	}, nil
}

// TleValidatorLiveness returns liveness data for a single TLE validator.
// Reads from persisted TleValidatorLiveness record (O(1)).
func (q queryServer) TleValidatorLiveness(ctx context.Context, req *types.QueryTleValidatorLivenessRequest) (*types.QueryTleValidatorLivenessResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Read persisted liveness record.
	record, err := q.k.TleValidatorLiveness.Get(ctx, req.Validator)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "validator not registered for TLE")
		}
		return nil, status.Error(codes.Internal, "internal error")
	}

	params, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to get params")
	}

	var participated uint32
	if record.WindowSize > record.MissedCount {
		participated = record.WindowSize - record.MissedCount
	}

	return &types.QueryTleValidatorLivenessResponse{
		Liveness: types.TleValidatorLivenessInfo{
			Validator:          record.Validator,
			MissedEpochs:       record.MissedCount,
			ParticipatedEpochs: participated,
			WindowSize:         record.WindowSize,
			OverTolerance:      record.MissedCount > params.TleMissTolerance,
			FlaggedAt:          record.FlaggedAt,
			RecoveredAt:        record.RecoveredAt,
		},
	}, nil
}

// GetTleEpochParticipation returns the raw participation record for a specific epoch.
func (q queryServer) GetTleEpochParticipation(ctx context.Context, req *types.QueryGetTleEpochParticipationRequest) (*types.QueryGetTleEpochParticipationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	record, err := q.k.TleEpochParticipation.Get(ctx, req.Epoch)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "no participation record for epoch")
		}
		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetTleEpochParticipationResponse{
		Participation: record,
	}, nil
}
