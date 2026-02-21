package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/vote/keeper"
	"sparkdream/x/vote/types"
)

func TestQueryTleLiveness_Empty(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	resp, err := qs.TleLiveness(f.ctx, &types.QueryTleLivenessRequest{})
	require.NoError(t, err)
	require.Empty(t, resp.Validators)
	require.Equal(t, uint32(0), resp.WindowSize)
}

func TestQueryTleLiveness_ReturnsPersistedState(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	// Store two liveness records.
	require.NoError(t, f.keeper.TleValidatorLiveness.Set(f.ctx, "valA", types.TleValidatorLiveness{
		Validator:   "valA",
		TleActive:   true,
		MissedCount: 2,
		WindowSize:  10,
		FlaggedAt:   0,
		RecoveredAt: 50,
	}))
	require.NoError(t, f.keeper.TleValidatorLiveness.Set(f.ctx, "valB", types.TleValidatorLiveness{
		Validator:   "valB",
		TleActive:   false,
		MissedCount: 8,
		WindowSize:  10,
		FlaggedAt:   100,
		RecoveredAt: 0,
	}))

	resp, err := qs.TleLiveness(f.ctx, &types.QueryTleLivenessRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Validators, 2)
	require.Equal(t, uint32(10), resp.WindowSize)
	require.Equal(t, types.DefaultParams().TleMissTolerance, resp.MissTolerance)

	// Build map for easy lookup (order may vary).
	byVal := make(map[string]types.TleValidatorLivenessInfo)
	for _, v := range resp.Validators {
		byVal[v.Validator] = v
	}

	a := byVal["valA"]
	require.Equal(t, uint32(2), a.MissedEpochs)
	require.Equal(t, uint32(8), a.ParticipatedEpochs)
	require.False(t, a.OverTolerance) // 2 <= 10 (default tolerance)
	require.Equal(t, int64(0), a.FlaggedAt)
	require.Equal(t, int64(50), a.RecoveredAt)

	b := byVal["valB"]
	require.Equal(t, uint32(8), b.MissedEpochs)
	require.Equal(t, uint32(2), b.ParticipatedEpochs)
	require.False(t, b.OverTolerance) // 8 <= 10 (default tolerance)
	require.Equal(t, int64(100), b.FlaggedAt)
	require.Equal(t, int64(0), b.RecoveredAt)
}

func TestQueryTleLiveness_NilRequest(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	_, err := qs.TleLiveness(f.ctx, nil)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestQueryTleValidatorLiveness_Found(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	require.NoError(t, f.keeper.TleValidatorLiveness.Set(f.ctx, "valX", types.TleValidatorLiveness{
		Validator:   "valX",
		TleActive:   false,
		MissedCount: 12,
		WindowSize:  20,
		FlaggedAt:   500,
		RecoveredAt: 0,
	}))

	resp, err := qs.TleValidatorLiveness(f.ctx, &types.QueryTleValidatorLivenessRequest{
		Validator: "valX",
	})
	require.NoError(t, err)
	require.Equal(t, "valX", resp.Liveness.Validator)
	require.Equal(t, uint32(12), resp.Liveness.MissedEpochs)
	require.Equal(t, uint32(8), resp.Liveness.ParticipatedEpochs)
	require.Equal(t, uint32(20), resp.Liveness.WindowSize)
	require.True(t, resp.Liveness.OverTolerance) // 12 > 10
	require.Equal(t, int64(500), resp.Liveness.FlaggedAt)
}

func TestQueryTleValidatorLiveness_NotFound(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	_, err := qs.TleValidatorLiveness(f.ctx, &types.QueryTleValidatorLivenessRequest{
		Validator: "nonexistent",
	})
	require.Error(t, err)
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestQueryTleValidatorLiveness_NilRequest(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	_, err := qs.TleValidatorLiveness(f.ctx, nil)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestQueryGetTleEpochParticipation_Found(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	require.NoError(t, f.keeper.TleEpochParticipation.Set(f.ctx, 42, types.TleEpochParticipation{
		Epoch:            42,
		RegisteredCount:  5,
		SubmittedCount:   3,
		MissedValidators: []string{"valA", "valB"},
		CheckedAt:        999,
	}))

	resp, err := qs.GetTleEpochParticipation(f.ctx, &types.QueryGetTleEpochParticipationRequest{
		Epoch: 42,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(42), resp.Participation.Epoch)
	require.Equal(t, uint32(5), resp.Participation.RegisteredCount)
	require.Equal(t, uint32(3), resp.Participation.SubmittedCount)
	require.Equal(t, []string{"valA", "valB"}, resp.Participation.MissedValidators)
	require.Equal(t, int64(999), resp.Participation.CheckedAt)
}

func TestQueryGetTleEpochParticipation_NotFound(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	_, err := qs.GetTleEpochParticipation(f.ctx, &types.QueryGetTleEpochParticipationRequest{
		Epoch: 999,
	})
	require.Error(t, err)
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestQueryGetTleEpochParticipation_NilRequest(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	_, err := qs.GetTleEpochParticipation(f.ctx, nil)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}
