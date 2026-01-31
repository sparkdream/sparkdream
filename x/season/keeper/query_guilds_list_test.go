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

func TestQueryGuildsList(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper
	qs := keeper.NewQueryServerImpl(k)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.GuildsList(ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("empty list", func(t *testing.T) {
		resp, err := qs.GuildsList(ctx, &types.QueryGuildsListRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		// Empty response when no guilds
	})

	t.Run("list with guilds", func(t *testing.T) {
		founder := TestAddrFounder
		founderStr, _ := f.addressCodec.BytesToString(founder)

		SetupBasicMemberProfile(t, k, ctx, founder)
		guildID := SetupGuild(t, k, ctx, founder, "List Test Guild", TestGuildDesc)

		resp, err := qs.GuildsList(ctx, &types.QueryGuildsListRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, guildID, resp.Id)
		require.Equal(t, "List Test Guild", resp.Name)
		require.Equal(t, founderStr, resp.Founder)
		require.Equal(t, uint64(types.GuildStatus_GUILD_STATUS_ACTIVE), resp.Status)
	})
}
