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

func TestQueryTitles(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper
	qs := keeper.NewQueryServerImpl(k)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.Titles(ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("empty list", func(t *testing.T) {
		resp, err := qs.Titles(ctx, &types.QueryTitlesRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Empty(t, resp.Titles)
	})

	t.Run("list with titles", func(t *testing.T) {
		// Create a title
		title := types.Title{
			TitleId:     "explorer",
			Name:        "Explorer",
			Description: "Awarded for exploration",
			Rarity:      types.Rarity_RARITY_UNCOMMON,
		}
		err := k.Title.Set(ctx, "explorer", title)
		require.NoError(t, err)

		resp, err := qs.Titles(ctx, &types.QueryTitlesRequest{})
		require.NoError(t, err)
		require.Len(t, resp.Titles, 1)
		require.Equal(t, "explorer", resp.Titles[0].TitleId)
		require.Equal(t, "Explorer", resp.Titles[0].Name)
		require.Equal(t, types.Rarity_RARITY_UNCOMMON, resp.Titles[0].Rarity)
	})
}
