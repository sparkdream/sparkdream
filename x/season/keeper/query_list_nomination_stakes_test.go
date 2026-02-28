package keeper_test

import (
	"fmt"
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestQueryListNominationStakes(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.ListNominationStakes(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("empty result - no stakes for nomination", func(t *testing.T) {
		resp, err := qs.ListNominationStakes(f.ctx, &types.QueryListNominationStakesRequest{
			NominationId: 999,
		})
		require.NoError(t, err)
		require.Len(t, resp.Stakes, 0)
	})

	t.Run("returns matching stakes only", func(t *testing.T) {
		// Create stakes for nomination 1
		stake1 := types.NominationStake{
			NominationId:  1,
			Staker:        "cosmos1staker1",
			Amount:        math.LegacyNewDec(100),
			StakedAtBlock: 500,
		}
		stake2 := types.NominationStake{
			NominationId:  1,
			Staker:        "cosmos1staker2",
			Amount:        math.LegacyNewDec(200),
			StakedAtBlock: 600,
		}
		// Create a stake for nomination 2 (should NOT be returned)
		stake3 := types.NominationStake{
			NominationId:  2,
			Staker:        "cosmos1staker3",
			Amount:        math.LegacyNewDec(300),
			StakedAtBlock: 700,
		}

		err := f.keeper.NominationStake.Set(f.ctx, fmt.Sprintf("%d/%s", stake1.NominationId, stake1.Staker), stake1)
		require.NoError(t, err)
		err = f.keeper.NominationStake.Set(f.ctx, fmt.Sprintf("%d/%s", stake2.NominationId, stake2.Staker), stake2)
		require.NoError(t, err)
		err = f.keeper.NominationStake.Set(f.ctx, fmt.Sprintf("%d/%s", stake3.NominationId, stake3.Staker), stake3)
		require.NoError(t, err)

		// Query stakes for nomination 1
		resp, err := qs.ListNominationStakes(f.ctx, &types.QueryListNominationStakesRequest{
			NominationId: 1,
		})
		require.NoError(t, err)
		require.Len(t, resp.Stakes, 2)

		// Verify all returned stakes belong to nomination 1
		for _, s := range resp.Stakes {
			require.Equal(t, uint64(1), s.NominationId)
		}

		// Query stakes for nomination 2
		resp, err = qs.ListNominationStakes(f.ctx, &types.QueryListNominationStakesRequest{
			NominationId: 2,
		})
		require.NoError(t, err)
		require.Len(t, resp.Stakes, 1)
		require.Equal(t, "cosmos1staker3", resp.Stakes[0].Staker)
	})
}
