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

func createStakeForStaker(k keeper.Keeper, ctx context.Context, id uint64, staker string, targetType types.StakeTargetType, targetID uint64) types.Stake {
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

func TestStakesByStaker(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(*fixture)
		staker         string
		wantStakeID    uint64
		wantTargetType uint64
		wantAmount     string
		wantErr        error
	}{
		{
			name: "ReturnsFirstStakeForStaker",
			setup: func(f *fixture) {
				createStakeForStaker(f.keeper, f.ctx, 1, "staker1", types.StakeTargetType_STAKE_TARGET_INITIATIVE, 10)
				createStakeForStaker(f.keeper, f.ctx, 2, "staker2", types.StakeTargetType_STAKE_TARGET_INITIATIVE, 20)
				createStakeForStaker(f.keeper, f.ctx, 3, "staker1", types.StakeTargetType_STAKE_TARGET_PROJECT, 30)
			},
			staker:         "staker1",
			wantStakeID:    1,
			wantTargetType: uint64(types.StakeTargetType_STAKE_TARGET_INITIATIVE),
			wantAmount:     "2000",
		},
		{
			name: "EmptyResponseWhenNoStakesForStaker",
			setup: func(f *fixture) {
				createStakeForStaker(f.keeper, f.ctx, 1, "staker1", types.StakeTargetType_STAKE_TARGET_INITIATIVE, 10)
				createStakeForStaker(f.keeper, f.ctx, 2, "staker2", types.StakeTargetType_STAKE_TARGET_INITIATIVE, 20)
			},
			staker:  "nonexistent",
			wantErr: nil,
		},
		{
			name:    "EmptyResponseWhenNoStakesExist",
			setup:   func(f *fixture) {},
			staker:  "staker1",
			wantErr: nil,
		},
		{
			name: "ReturnsStakeForProjectTarget",
			setup: func(f *fixture) {
				createStakeForStaker(f.keeper, f.ctx, 1, "delegate1", types.StakeTargetType_STAKE_TARGET_PROJECT, 100)
				createStakeForStaker(f.keeper, f.ctx, 2, "delegate1", types.StakeTargetType_STAKE_TARGET_INITIATIVE, 200)
			},
			staker:         "delegate1",
			wantStakeID:    1,
			wantTargetType: uint64(types.StakeTargetType_STAKE_TARGET_PROJECT),
			wantAmount:     "2000",
		},
		{
			name:    "InvalidRequestNil",
			setup:   func(f *fixture) {},
			staker:  "",
			wantErr: status.Error(codes.InvalidArgument, "invalid request"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initFixture(t)
			qs := keeper.NewQueryServerImpl(f.keeper)

			if tc.setup != nil {
				tc.setup(f)
			}

			var req *types.QueryStakesByStakerRequest
			if tc.staker != "" || tc.wantErr == nil {
				req = &types.QueryStakesByStakerRequest{Staker: tc.staker}
			}

			response, err := qs.StakesByStaker(f.ctx, req)

			if tc.wantErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tc.wantErr)
			} else if tc.wantStakeID > 0 {
				require.NoError(t, err)
				require.NotNil(t, response)
				require.Equal(t, tc.wantStakeID, response.StakeId)
				require.Equal(t, tc.wantTargetType, response.TargetType)
				require.Equal(t, tc.wantAmount, response.Amount.String())
			} else {
				require.NoError(t, err)
				require.NotNil(t, response)
				require.Equal(t, uint64(0), response.StakeId)
				require.Equal(t, uint64(0), response.TargetType)
				if response.Amount != nil {
					require.Equal(t, "0", response.Amount.String())
				}
			}
		})
	}
}

func TestStakesByStaker_MultipleStakes(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	// Create multiple stakes for the same staker
	createStakeForStaker(f.keeper, f.ctx, 1, "whale1", types.StakeTargetType_STAKE_TARGET_INITIATIVE, 100)
	createStakeForStaker(f.keeper, f.ctx, 2, "whale1", types.StakeTargetType_STAKE_TARGET_INITIATIVE, 200)
	createStakeForStaker(f.keeper, f.ctx, 3, "whale1", types.StakeTargetType_STAKE_TARGET_PROJECT, 300)

	// Query should return first stake (id 1)
	response, err := qs.StakesByStaker(f.ctx, &types.QueryStakesByStakerRequest{Staker: "whale1"})
	require.NoError(t, err)
	require.NotNil(t, response)
	require.Equal(t, uint64(1), response.StakeId)
	require.Equal(t, uint64(types.StakeTargetType_STAKE_TARGET_INITIATIVE), response.TargetType)
	require.Equal(t, "2000", response.Amount.String())
}
