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

func TestQueryGuildsByFounder(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper
	qs := keeper.NewQueryServerImpl(k)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.GuildsByFounder(ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("empty founder address", func(t *testing.T) {
		_, err := qs.GuildsByFounder(ctx, &types.QueryGuildsByFounderRequest{Founder: ""})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("founder with no guilds", func(t *testing.T) {
		founder := TestAddrMember1
		founderStr, _ := f.addressCodec.BytesToString(founder)

		resp, err := qs.GuildsByFounder(ctx, &types.QueryGuildsByFounderRequest{Founder: founderStr})
		require.NoError(t, err)
		require.Empty(t, resp.Name)
	})

	t.Run("founder with guild", func(t *testing.T) {
		founder := TestAddrFounder
		founderStr, _ := f.addressCodec.BytesToString(founder)

		SetupBasicMemberProfile(t, k, ctx, founder)
		guildID := SetupGuild(t, k, ctx, founder, "Founder Query Guild", TestGuildDesc)

		resp, err := qs.GuildsByFounder(ctx, &types.QueryGuildsByFounderRequest{Founder: founderStr})
		require.NoError(t, err)
		require.Equal(t, guildID, resp.Id)
		require.Equal(t, "Founder Query Guild", resp.Name)
	})
}
