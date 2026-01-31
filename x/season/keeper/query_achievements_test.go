package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestQueryAchievements(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper
	qs := keeper.NewQueryServerImpl(k)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.Achievements(ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("empty list", func(t *testing.T) {
		resp, err := qs.Achievements(ctx, &types.QueryAchievementsRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Empty(t, resp.Id)
	})

	t.Run("list with achievements", func(t *testing.T) {
		// Create an achievement
		achievement := types.Achievement{
			AchievementId: "first_win",
			Name:          "First Victory",
			Description:   "Win your first challenge",
			Rarity:        types.Rarity_RARITY_COMMON,
			XpReward:      50,
		}
		err := k.Achievement.Set(ctx, "first_win", achievement)
		require.NoError(t, err)

		resp, err := qs.Achievements(ctx, &types.QueryAchievementsRequest{})
		require.NoError(t, err)
		require.Equal(t, "first_win", resp.Id)
		require.Equal(t, "First Victory", resp.Name)
		require.Equal(t, uint64(types.Rarity_RARITY_COMMON), resp.Rarity)
		require.Equal(t, uint64(50), resp.XpReward)
	})
}
