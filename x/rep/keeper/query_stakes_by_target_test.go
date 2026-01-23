package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func createStakeForTarget(k keeper.Keeper, ctx context.Context, id uint64, staker string, targetType types.StakeTargetType, targetID uint64) types.Stake {
	amount := math.NewInt(int64((id + 1) * 1000))
	stake := types.Stake{
		Id:         id,
		Staker:     staker,
		TargetType: targetType,
		TargetId:   targetID,
		Amount:     amount,
		CreatedAt:  int64(id * 1000),
	}
	_ = k.Stake.Set(ctx, id, stake)
	_ = k.StakeSeq.Set(ctx, id)
	return stake
}

func TestStakesByTarget(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(*fixture)
		targetType  types.StakeTargetType
		targetID    uint64
		wantStakeID uint64
		wantStaker  string
		wantAmount  string
		wantErr     error
	}{
		{
			name: "ReturnsFirstStakeForTarget",
			setup: func(f *fixture) {
				createStakeForTarget(f.keeper, f.ctx, 1, "staker1", types.StakeTargetType_STAKE_TARGET_INITIATIVE, 100)
				createStakeForTarget(f.keeper, f.ctx, 2, "staker2", types.StakeTargetType_STAKE_TARGET_INITIATIVE, 200)
				createStakeForTarget(f.keeper, f.ctx, 3, "staker3", types.StakeTargetType_STAKE_TARGET_INITIATIVE, 100)
			},
			targetType:  types.StakeTargetType_STAKE_TARGET_INITIATIVE,
			targetID:    100,
			wantStakeID: 1,
			wantStaker:  "staker1",
			wantAmount:  "2000",
		},
		{
			name: "EmptyResponseWhenNoStakesForTarget",
			setup: func(f *fixture) {
				createStakeForTarget(f.keeper, f.ctx, 1, "staker1", types.StakeTargetType_STAKE_TARGET_INITIATIVE, 100)
				createStakeForTarget(f.keeper, f.ctx, 2, "staker2", types.StakeTargetType_STAKE_TARGET_PROJECT, 200)
			},
			targetType: types.StakeTargetType_STAKE_TARGET_INITIATIVE,
			targetID:   999,
			wantErr:    nil,
		},
		{
			name:       "EmptyResponseWhenNoStakesExist",
			setup:      func(f *fixture) {},
			targetType: types.StakeTargetType_STAKE_TARGET_INITIATIVE,
			targetID:   1,
			wantErr:    nil,
		},
		{
			name: "ReturnsStakeForProjectTarget",
			setup: func(f *fixture) {
				createStakeForTarget(f.keeper, f.ctx, 1, "delegate1", types.StakeTargetType_STAKE_TARGET_PROJECT, 500)
				createStakeForTarget(f.keeper, f.ctx, 2, "delegate2", types.StakeTargetType_STAKE_TARGET_PROJECT, 500)
			},
			targetType:  types.StakeTargetType_STAKE_TARGET_PROJECT,
			targetID:    500,
			wantStakeID: 1,
			wantStaker:  "delegate1",
			wantAmount:  "2000",
		},
		{
			name:       "InvalidRequestNil",
			setup:      func(f *fixture) {},
			targetType: 0,
			targetID:   0,
			wantErr:    status.Error(codes.InvalidArgument, "invalid request"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initFixture(t)
			qs := keeper.NewQueryServerImpl(f.keeper)

			if tc.setup != nil {
				tc.setup(f)
			}

			var req *types.QueryStakesByTargetRequest
			if tc.wantErr == nil {
				req = &types.QueryStakesByTargetRequest{
					TargetType: uint64(tc.targetType),
					TargetId:   tc.targetID,
				}
			}

			response, err := qs.StakesByTarget(f.ctx, req)

			if tc.wantErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tc.wantErr)
			} else if tc.wantStakeID > 0 {
				require.NoError(t, err)
				require.NotNil(t, response)
				require.NotEmpty(t, response.Stakes)
				require.Equal(t, tc.wantStakeID, response.Stakes[0].Id)
				require.Equal(t, tc.wantStaker, response.Stakes[0].Staker)
				require.Equal(t, tc.wantAmount, response.Stakes[0].Amount.String())
			} else {
				require.NoError(t, err)
				require.NotNil(t, response)
				require.Empty(t, response.Stakes)
			}
		})
	}
}

func TestStakesByTarget_MultipleStakes(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	// Create multiple stakes for the same target
	createStakeForTarget(f.keeper, f.ctx, 1, "supporter1", types.StakeTargetType_STAKE_TARGET_INITIATIVE, 1000)
	createStakeForTarget(f.keeper, f.ctx, 2, "supporter2", types.StakeTargetType_STAKE_TARGET_INITIATIVE, 1000)
	createStakeForTarget(f.keeper, f.ctx, 3, "supporter3", types.StakeTargetType_STAKE_TARGET_INITIATIVE, 1000)

	// Query should return all stakes for target 1000
	response, err := qs.StakesByTarget(f.ctx, &types.QueryStakesByTargetRequest{
		TargetType: uint64(types.StakeTargetType_STAKE_TARGET_INITIATIVE),
		TargetId:   1000,
	})
	require.NoError(t, err)
	require.NotNil(t, response)
	require.Len(t, response.Stakes, 3)
	require.Equal(t, uint64(1), response.Stakes[0].Id)
	require.Equal(t, "supporter1", response.Stakes[0].Staker)
	require.Equal(t, "2000", response.Stakes[0].Amount.String())
}
