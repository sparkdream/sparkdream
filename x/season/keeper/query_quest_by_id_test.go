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

func TestQueryQuestById(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper
	qs := keeper.NewQueryServerImpl(k)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.QuestById(ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("empty quest id", func(t *testing.T) {
		_, err := qs.QuestById(ctx, &types.QueryQuestByIdRequest{QuestId: ""})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("quest not found", func(t *testing.T) {
		_, err := qs.QuestById(ctx, &types.QueryQuestByIdRequest{QuestId: "nonexistent"})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.NotFound, st.Code())
	})

	t.Run("successful query", func(t *testing.T) {
		SetupQuest(t, k, ctx, "test_quest", 100, true)

		resp, err := qs.QuestById(ctx, &types.QueryQuestByIdRequest{QuestId: "test_quest"})
		require.NoError(t, err)
		require.Equal(t, "Test Quest test_quest", resp.Name)
		require.Equal(t, uint64(100), resp.XpReward)
		require.True(t, resp.Active)
	})

	t.Run("query inactive quest", func(t *testing.T) {
		SetupQuest(t, k, ctx, "inactive_quest", 50, false)

		resp, err := qs.QuestById(ctx, &types.QueryQuestByIdRequest{QuestId: "inactive_quest"})
		require.NoError(t, err)
		require.False(t, resp.Active)
	})
}
