package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerDissolveGuild(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.DissolveGuild(f.ctx, &types.MsgDissolveGuild{
			Creator: "invalid-address",
			GuildId: 1,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("guild not found", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		ms := keeper.NewMsgServerImpl(f.keeper)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		_, err := ms.DissolveGuild(ctx, &types.MsgDissolveGuild{
			Creator: creatorStr,
			GuildId: 999, // non-existent
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrGuildNotFound)
	})

	t.Run("not guild founder", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		nonFounder := TestAddrMember1

		// Setup guild
		SetupBasicMemberProfile(t, k, ctx, founder)
		guildID := SetupGuild(t, k, ctx, founder, TestGuildName, TestGuildDesc)

		// Make guild old enough
		guild, _ := k.Guild.Get(ctx, guildID)
		guild.CreatedBlock = 0 // Created at block 0
		k.Guild.Set(ctx, guildID, guild)

		// Advance blocks
		params, _ := k.Params.Get(ctx)
		ctx = ctx.WithBlockHeight(int64(params.MinGuildAgeEpochs+1) * params.EpochBlocks)

		nonFounderStr, _ := f.addressCodec.BytesToString(nonFounder)

		_, err := ms.DissolveGuild(ctx, &types.MsgDissolveGuild{
			Creator: nonFounderStr,
			GuildId: guildID,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotGuildFounder)
	})

	t.Run("guild already dissolved", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		founderStr, _ := f.addressCodec.BytesToString(founder)

		// Setup guild and mark as dissolved
		SetupBasicMemberProfile(t, k, ctx, founder)
		guildID := SetupGuild(t, k, ctx, founder, TestGuildName, TestGuildDesc)

		guild, _ := k.Guild.Get(ctx, guildID)
		guild.Status = types.GuildStatus_GUILD_STATUS_DISSOLVED
		k.Guild.Set(ctx, guildID, guild)

		_, err := ms.DissolveGuild(ctx, &types.MsgDissolveGuild{
			Creator: founderStr,
			GuildId: guildID,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrGuildDissolved)
	})

	t.Run("guild too young to dissolve", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		founderStr, _ := f.addressCodec.BytesToString(founder)

		// Setup guild created at current block
		SetupBasicMemberProfile(t, k, ctx, founder)
		guildID := SetupGuild(t, k, ctx, founder, TestGuildName, TestGuildDesc)

		// Guild was just created, so it's too young
		_, err := ms.DissolveGuild(ctx, &types.MsgDissolveGuild{
			Creator: founderStr,
			GuildId: guildID,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrGuildTooYoung)
	})

	t.Run("successful dissolution", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		member := TestAddrMember1
		founderStr, _ := f.addressCodec.BytesToString(founder)
		memberStr, _ := f.addressCodec.BytesToString(member)

		// Setup guild with members
		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupBasicMemberProfile(t, k, ctx, member)
		guildID := SetupGuild(t, k, ctx, founder, TestGuildName, TestGuildDesc)

		// Update profiles to be in guild
		founderProfile, _ := k.MemberProfile.Get(ctx, founderStr)
		founderProfile.GuildId = guildID
		k.MemberProfile.Set(ctx, founderStr, founderProfile)

		memberProfile, _ := k.MemberProfile.Get(ctx, memberStr)
		memberProfile.GuildId = guildID
		k.MemberProfile.Set(ctx, memberStr, memberProfile)

		// Create membership records
		founderMembership := types.GuildMembership{
			Member:    founderStr,
			GuildId:   guildID,
			LeftEpoch: 0,
		}
		k.GuildMembership.Set(ctx, founderStr, founderMembership)

		memberMembership := types.GuildMembership{
			Member:    memberStr,
			GuildId:   guildID,
			LeftEpoch: 0,
		}
		k.GuildMembership.Set(ctx, memberStr, memberMembership)

		// Make guild old enough
		guild, _ := k.Guild.Get(ctx, guildID)
		guild.CreatedBlock = 0
		k.Guild.Set(ctx, guildID, guild)

		// Advance time
		params, _ := k.Params.Get(ctx)
		ctx = ctx.WithBlockHeight(int64(params.MinGuildAgeEpochs+1) * params.EpochBlocks)

		// Dissolve guild
		_, err := ms.DissolveGuild(ctx, &types.MsgDissolveGuild{
			Creator: founderStr,
			GuildId: guildID,
		})

		require.NoError(t, err)

		// Verify guild is dissolved
		guild, err = k.Guild.Get(ctx, guildID)
		require.NoError(t, err)
		require.Equal(t, types.GuildStatus_GUILD_STATUS_DISSOLVED, guild.Status)
		require.Empty(t, guild.Officers)
		require.Empty(t, guild.PendingInvites)

		// Verify founder's profile was updated
		founderProfile, err = k.MemberProfile.Get(ctx, founderStr)
		require.NoError(t, err)
		require.Equal(t, uint64(0), founderProfile.GuildId)

		// Verify member's profile was updated
		memberProfile, err = k.MemberProfile.Get(ctx, memberStr)
		require.NoError(t, err)
		require.Equal(t, uint64(0), memberProfile.GuildId)

		// Verify membership records were updated
		founderMembership, err = k.GuildMembership.Get(ctx, founderStr)
		require.NoError(t, err)
		require.Equal(t, uint64(0), founderMembership.GuildId)
		require.NotEqual(t, int64(0), founderMembership.LeftEpoch)

		memberMembership, err = k.GuildMembership.Get(ctx, memberStr)
		require.NoError(t, err)
		require.Equal(t, uint64(0), memberMembership.GuildId)
		require.NotEqual(t, int64(0), memberMembership.LeftEpoch)
	})

	t.Run("dissolution clears officers and pending invites", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		officer := TestAddrOfficer
		founderStr, _ := f.addressCodec.BytesToString(founder)

		// Setup guild with officer and pending invites
		SetupBasicMemberProfile(t, k, ctx, founder)
		guildID := SetupGuild(t, k, ctx, founder, "Guild With Officers", TestGuildDesc)

		guild, _ := k.Guild.Get(ctx, guildID)
		guild.CreatedBlock = 0
		guild.Officers = []string{officer.String()}
		guild.PendingInvites = []string{"pending1", "pending2"}
		k.Guild.Set(ctx, guildID, guild)

		// Advance time
		params, _ := k.Params.Get(ctx)
		ctx = ctx.WithBlockHeight(int64(params.MinGuildAgeEpochs+1) * params.EpochBlocks)

		// Dissolve guild
		_, err := ms.DissolveGuild(ctx, &types.MsgDissolveGuild{
			Creator: founderStr,
			GuildId: guildID,
		})

		require.NoError(t, err)

		// Verify officers and pending invites were cleared
		guild, _ = k.Guild.Get(ctx, guildID)
		require.Empty(t, guild.Officers)
		require.Empty(t, guild.PendingInvites)
	})
}
