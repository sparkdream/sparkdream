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

func TestQueryGuildById(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper
	qs := keeper.NewQueryServerImpl(k)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.GuildById(ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("guild not found", func(t *testing.T) {
		_, err := qs.GuildById(ctx, &types.QueryGuildByIdRequest{GuildId: 999})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.NotFound, st.Code())
	})

	t.Run("successful query", func(t *testing.T) {
		founder := TestAddrFounder
		founderStr, _ := f.addressCodec.BytesToString(founder)

		SetupBasicMemberProfile(t, k, ctx, founder)
		guildID := SetupGuild(t, k, ctx, founder, "Query Test Guild", "A guild for testing queries")

		resp, err := qs.GuildById(ctx, &types.QueryGuildByIdRequest{GuildId: guildID})
		require.NoError(t, err)
		require.Equal(t, "Query Test Guild", resp.Name)
		require.Equal(t, "A guild for testing queries", resp.Description)
		require.Equal(t, founderStr, resp.Founder)
		require.False(t, resp.InviteOnly)
		require.Equal(t, uint64(types.GuildStatus_GUILD_STATUS_ACTIVE), resp.Status)
	})

	t.Run("query dissolved guild", func(t *testing.T) {
		founder := TestAddrMember1
		SetupBasicMemberProfile(t, k, ctx, founder)
		guildID := SetupGuild(t, k, ctx, founder, "Dissolved Guild", "A dissolved guild")

		// Set guild to dissolved
		guild, _ := k.Guild.Get(ctx, guildID)
		guild.Status = types.GuildStatus_GUILD_STATUS_DISSOLVED
		k.Guild.Set(ctx, guildID, guild)

		resp, err := qs.GuildById(ctx, &types.QueryGuildByIdRequest{GuildId: guildID})
		require.NoError(t, err)
		require.Equal(t, uint64(types.GuildStatus_GUILD_STATUS_DISSOLVED), resp.Status)
	})
}
