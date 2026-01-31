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

func TestQueryQuestsList(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper
	qs := keeper.NewQueryServerImpl(k)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.QuestsList(ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("empty list", func(t *testing.T) {
		resp, err := qs.QuestsList(ctx, &types.QueryQuestsListRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Empty(t, resp.Id)
	})

	t.Run("list with quests", func(t *testing.T) {
		SetupQuest(t, k, ctx, "daily_login", 25, true)

		resp, err := qs.QuestsList(ctx, &types.QueryQuestsListRequest{})
		require.NoError(t, err)
		require.Equal(t, "daily_login", resp.Id)
		require.Equal(t, "Test Quest daily_login", resp.Name)
		require.True(t, resp.Active)
	})
}
