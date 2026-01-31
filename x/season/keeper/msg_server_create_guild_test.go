package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerCreateGuild(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.CreateGuild(f.ctx, &types.MsgCreateGuild{
			Creator:     "invalid-address",
			Name:        TestGuildName,
			Description: TestGuildDesc,
			InviteOnly:  false,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("guild name too short", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		// Setup member profile
		SetupBasicMemberProfile(t, k, ctx, creator)

		_, err := ms.CreateGuild(ctx, &types.MsgCreateGuild{
			Creator:     creatorStr,
			Name:        "ab", // too short
			Description: TestGuildDesc,
			InviteOnly:  false,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrGuildNameTooShort)
	})

	t.Run("creator not a member (no profile)", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		// Don't setup member profile

		_, err := ms.CreateGuild(ctx, &types.MsgCreateGuild{
			Creator:     creatorStr,
			Name:        TestGuildName,
			Description: TestGuildDesc,
			InviteOnly:  false,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "profile not found")
	})

	t.Run("creator already in a guild", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		// Setup member profile with existing guild
		SetupMemberProfileWithGuild(t, k, ctx, creator, 1)

		_, err := ms.CreateGuild(ctx, &types.MsgCreateGuild{
			Creator:     creatorStr,
			Name:        TestGuildName,
			Description: TestGuildDesc,
			InviteOnly:  false,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrAlreadyInGuild)
	})

	t.Run("guild name already taken", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator1 := TestAddrCreator
		creator2 := TestAddrMember1

		// Setup first guild
		SetupBasicMemberProfile(t, k, ctx, creator1)
		SetupGuild(t, k, ctx, creator1, TestGuildName, TestGuildDesc)

		// Try to create another guild with same name
		SetupBasicMemberProfile(t, k, ctx, creator2)
		creator2Str, _ := f.addressCodec.BytesToString(creator2)

		_, err := ms.CreateGuild(ctx, &types.MsgCreateGuild{
			Creator:     creator2Str,
			Name:        TestGuildName,
			Description: "Another description",
			InviteOnly:  false,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrGuildNameTaken)
	})

	t.Run("successful guild creation", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrMember2
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		// Setup member profile
		SetupBasicMemberProfile(t, k, ctx, creator)

		// Create guild
		_, err := ms.CreateGuild(ctx, &types.MsgCreateGuild{
			Creator:     creatorStr,
			Name:        "Unique Guild Name",
			Description: TestGuildDesc,
			InviteOnly:  true,
		})

		require.NoError(t, err)

		// Verify guild was created
		iter, _ := k.Guild.Iterate(ctx, nil)
		defer iter.Close()

		var foundGuild *types.Guild
		for ; iter.Valid(); iter.Next() {
			guild, _ := iter.Value()
			if guild.Name == "Unique Guild Name" {
				foundGuild = &guild
				break
			}
		}

		require.NotNil(t, foundGuild, "guild should be created")
		require.Equal(t, creatorStr, foundGuild.Founder)
		require.True(t, foundGuild.InviteOnly)
		require.Equal(t, types.GuildStatus_GUILD_STATUS_ACTIVE, foundGuild.Status)

		// Verify member profile was updated
		profile, err := k.MemberProfile.Get(ctx, creatorStr)
		require.NoError(t, err)
		require.Equal(t, foundGuild.Id, profile.GuildId)

		// Verify membership record was created
		membership, err := k.GuildMembership.Get(ctx, creatorStr)
		require.NoError(t, err)
		require.Equal(t, foundGuild.Id, membership.GuildId)
		require.Equal(t, uint64(1), membership.GuildsJoinedThisSeason)
	})

	t.Run("successful guild creation with invite only false", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrMember3
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		// Setup member profile
		SetupBasicMemberProfile(t, k, ctx, creator)

		// Create guild
		_, err := ms.CreateGuild(ctx, &types.MsgCreateGuild{
			Creator:     creatorStr,
			Name:        "Public Guild",
			Description: "A public guild",
			InviteOnly:  false,
		})

		require.NoError(t, err)

		// Find the guild
		iter, _ := k.Guild.Iterate(ctx, nil)
		defer iter.Close()

		var foundGuild *types.Guild
		for ; iter.Valid(); iter.Next() {
			guild, _ := iter.Value()
			if guild.Name == "Public Guild" {
				foundGuild = &guild
				break
			}
		}

		require.NotNil(t, foundGuild)
		require.False(t, foundGuild.InviteOnly)
	})

	t.Run("guild hop cooldown enforced", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		params, _ := k.Params.Get(ctx)
		// Advance to epoch 5 (so current epoch > 0 and we can set LeftEpoch > 0)
		ctx = ctx.WithBlockHeight(5 * params.EpochBlocks)

		// Setup member profile without guild
		SetupBasicMemberProfile(t, k, ctx, creator)

		// Create membership record showing they left a guild at epoch 4 (1 epoch ago)
		membership := types.GuildMembership{
			Member:                 creatorStr,
			GuildId:                0,
			LeftEpoch:              4, // Left at epoch 4, still within cooldown
			GuildsJoinedThisSeason: 1,
		}
		k.GuildMembership.Set(ctx, creatorStr, membership)

		// Try to create a new guild (should fail due to cooldown)
		_, err := ms.CreateGuild(ctx, &types.MsgCreateGuild{
			Creator:     creatorStr,
			Name:        "New Guild After Hop",
			Description: TestGuildDesc,
			InviteOnly:  false,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrGuildHopCooldown)
	})
}
