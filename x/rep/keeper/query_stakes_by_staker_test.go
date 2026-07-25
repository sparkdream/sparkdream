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
		name       string
		setup      func(*fixture)
		staker     string
		wantIDs    []uint64
		wantAmount string // Amount of the first returned stake, when wantIDs is non-empty
		wantErr    error
	}{
		{
			name: "ReturnsAllStakesForStaker",
			setup: func(f *fixture) {
				createStakeForStaker(f.keeper, f.ctx, 1, "staker1", types.StakeTargetType_STAKE_TARGET_INITIATIVE, 10)
				createStakeForStaker(f.keeper, f.ctx, 2, "staker2", types.StakeTargetType_STAKE_TARGET_INITIATIVE, 20)
				createStakeForStaker(f.keeper, f.ctx, 3, "staker1", types.StakeTargetType_STAKE_TARGET_PROJECT, 30)
			},
			staker:     "staker1",
			wantIDs:    []uint64{1, 3},
			wantAmount: "2000",
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
			staker:     "delegate1",
			wantIDs:    []uint64{1, 2},
			wantAmount: "2000",
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
				return
			}

			require.NoError(t, err)
			require.NotNil(t, response)

			gotIDs := make([]uint64, len(response.Stakes))
			for i, s := range response.Stakes {
				gotIDs[i] = s.Id
				// Every returned stake must belong to the requested staker.
				require.Equal(t, tc.staker, s.Staker)
			}
			require.ElementsMatch(t, tc.wantIDs, gotIDs)

			if len(tc.wantIDs) > 0 {
				require.Equal(t, tc.wantAmount, response.Stakes[0].Amount.String())
			}
		})
	}
}

func TestStakesByStaker_MultipleStakes(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	// Create multiple stakes for the same staker, plus one for another staker
	// that must not leak into the result.
	createStakeForStaker(f.keeper, f.ctx, 1, "whale1", types.StakeTargetType_STAKE_TARGET_INITIATIVE, 100)
	createStakeForStaker(f.keeper, f.ctx, 2, "whale1", types.StakeTargetType_STAKE_TARGET_INITIATIVE, 200)
	createStakeForStaker(f.keeper, f.ctx, 3, "whale1", types.StakeTargetType_STAKE_TARGET_PROJECT, 300)
	createStakeForStaker(f.keeper, f.ctx, 4, "minnow1", types.StakeTargetType_STAKE_TARGET_INITIATIVE, 100)

	// Query should return all three of whale1's stakes and none of minnow1's.
	response, err := qs.StakesByStaker(f.ctx, &types.QueryStakesByStakerRequest{Staker: "whale1"})
	require.NoError(t, err)
	require.NotNil(t, response)
	require.Len(t, response.Stakes, 3)

	gotIDs := make([]uint64, len(response.Stakes))
	for i, s := range response.Stakes {
		gotIDs[i] = s.Id
		require.Equal(t, "whale1", s.Staker)
	}
	require.ElementsMatch(t, []uint64{1, 2, 3}, gotIDs)
}
