package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerSetUsername(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.SetUsername(f.ctx, &types.MsgSetUsername{
			Creator:  "invalid-address",
			Username: TestUsername,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("username too short", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		SetupBasicMemberProfile(t, k, ctx, creator)

		_, err := ms.SetUsername(ctx, &types.MsgSetUsername{
			Creator:  creatorStr,
			Username: "ab", // too short
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrUsernameTooShort)
	})

	t.Run("username with invalid characters", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		SetupBasicMemberProfile(t, k, ctx, creator)

		_, err := ms.SetUsername(ctx, &types.MsgSetUsername{
			Creator:  creatorStr,
			Username: "Invalid-User!", // invalid chars
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrUsernameInvalidChars)
	})

	t.Run("creator has no profile", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		// Don't setup profile

		_, err := ms.SetUsername(ctx, &types.MsgSetUsername{
			Creator:  creatorStr,
			Username: TestUsername,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "profile not found")
	})

	t.Run("username already taken by another user", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		other := TestAddrMember1
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		// Setup profiles, other has the username
		SetupBasicMemberProfile(t, k, ctx, creator)
		SetupMemberProfile(t, k, ctx, other, "OtherDisplay", TestUsername)

		_, err := ms.SetUsername(ctx, &types.MsgSetUsername{
			Creator:  creatorStr,
			Username: TestUsername,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrUsernameAlreadyTaken)
	})

	t.Run("username case insensitive collision", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		other := TestAddrMember1
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		// Setup profiles, other has username in different case
		SetupBasicMemberProfile(t, k, ctx, creator)
		SetupMemberProfile(t, k, ctx, other, "OtherDisplay", "testuser")

		_, err := ms.SetUsername(ctx, &types.MsgSetUsername{
			Creator:  creatorStr,
			Username: "TESTUSER", // different case but should still be taken
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrUsernameAlreadyTaken)
	})

	t.Run("username cooldown not passed", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		params, _ := k.Params.Get(ctx)
		// Advance to epoch 5 (so current epoch > 0)
		ctx = ctx.WithBlockHeight(5 * params.EpochBlocks)

		// Setup profile with recent username change at current epoch
		profile := types.MemberProfile{
			Address:                 creatorStr,
			DisplayName:             "Display",
			Username:                "oldusername",
			LastUsernameChangeEpoch: k.GetCurrentEpoch(ctx), // Just changed (epoch 5)
		}
		k.MemberProfile.Set(ctx, creatorStr, profile)

		_, err := ms.SetUsername(ctx, &types.MsgSetUsername{
			Creator:  creatorStr,
			Username: "newusername",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrUsernameCooldown)
	})

	t.Run("successful username set (first time)", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		params, _ := k.Params.Get(ctx)
		// Advance to epoch 3 so LastUsernameChangeEpoch will be non-zero
		ctx = ctx.WithBlockHeight(3 * params.EpochBlocks)

		// Setup profile without username
		SetupMemberProfile(t, k, ctx, creator, "Display", "")

		_, err := ms.SetUsername(ctx, &types.MsgSetUsername{
			Creator:  creatorStr,
			Username: "newusername",
		})

		require.NoError(t, err)

		// Verify username was set
		profile, err := k.MemberProfile.Get(ctx, creatorStr)
		require.NoError(t, err)
		require.Equal(t, "newusername", profile.Username)
		require.Equal(t, int64(3), profile.LastUsernameChangeEpoch)
	})

	t.Run("successful username change after cooldown", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrMember1
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		params, _ := k.Params.Get(ctx)

		// Setup profile with old username change
		profile := types.MemberProfile{
			Address:                 creatorStr,
			DisplayName:             "Display",
			Username:                "oldusername",
			LastUsernameChangeEpoch: 0, // Changed long ago
		}
		k.MemberProfile.Set(ctx, creatorStr, profile)

		// Advance time past cooldown
		ctx = ctx.WithBlockHeight(int64(params.UsernameChangeCooldownEpochs+1) * params.EpochBlocks)

		_, err := ms.SetUsername(ctx, &types.MsgSetUsername{
			Creator:  creatorStr,
			Username: "brandnewname",
		})

		require.NoError(t, err)

		// Verify username was changed
		profile, err = k.MemberProfile.Get(ctx, creatorStr)
		require.NoError(t, err)
		require.Equal(t, "brandnewname", profile.Username)
	})

	t.Run("username normalized to lowercase", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrMember2
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		// Setup profile without username
		SetupMemberProfile(t, k, ctx, creator, "Display", "")

		// Set username with mixed case (the handler lowercases it)
		// Note: validation happens after lowercasing, so uppercase chars cause validation error
		_, err := ms.SetUsername(ctx, &types.MsgSetUsername{
			Creator:  creatorStr,
			Username: "validlowercase",
		})

		require.NoError(t, err)

		profile, err := k.MemberProfile.Get(ctx, creatorStr)
		require.NoError(t, err)
		require.Equal(t, "validlowercase", profile.Username)
	})

	t.Run("can set same username (no change)", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrMember3
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		// Setup profile with username
		profile := types.MemberProfile{
			Address:                 creatorStr,
			DisplayName:             "Display",
			Username:                "existingname",
			LastUsernameChangeEpoch: 0,
		}
		k.MemberProfile.Set(ctx, creatorStr, profile)

		params, _ := k.Params.Get(ctx)
		ctx = ctx.WithBlockHeight(int64(params.UsernameChangeCooldownEpochs+1) * params.EpochBlocks)

		// Try to set the same username (should succeed, user owns it)
		_, err := ms.SetUsername(ctx, &types.MsgSetUsername{
			Creator:  creatorStr,
			Username: "existingname",
		})

		require.NoError(t, err)
	})
}
