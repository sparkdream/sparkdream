package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerSetDisplayName(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.SetDisplayName(f.ctx, &types.MsgSetDisplayName{
			Creator: "invalid-address",
			Name:    "ValidName",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("display name too short", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		_, err := ms.SetDisplayName(ctx, &types.MsgSetDisplayName{
			Creator: creatorStr,
			Name:    "", // Empty, below min length
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrDisplayNameTooShort)
	})

	t.Run("display name too long", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		// Create a name longer than max (50 chars)
		longName := "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz123"

		_, err := ms.SetDisplayName(ctx, &types.MsgSetDisplayName{
			Creator: creatorStr,
			Name:    longName,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrDisplayNameTooLong)
	})

	t.Run("successful first time set creates profile", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrMember1
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		// No profile exists yet

		_, err := ms.SetDisplayName(ctx, &types.MsgSetDisplayName{
			Creator: creatorStr,
			Name:    "NewUser",
		})

		require.NoError(t, err)

		// Verify profile was created
		profile, err := k.MemberProfile.Get(ctx, creatorStr)
		require.NoError(t, err)
		require.Equal(t, "NewUser", profile.DisplayName)
		require.Equal(t, uint64(1), profile.SeasonLevel)
	})

	t.Run("cooldown enforced for existing profile", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrMember2
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		params, _ := k.Params.Get(ctx)
		// Advance to epoch 5
		ctx = ctx.WithBlockHeight(5 * params.EpochBlocks)

		// Setup profile with recent change
		profile := types.MemberProfile{
			Address:                    creatorStr,
			DisplayName:                "OldName",
			LastDisplayNameChangeEpoch: 5, // Just changed at epoch 5
		}
		k.MemberProfile.Set(ctx, creatorStr, profile)

		_, err := ms.SetDisplayName(ctx, &types.MsgSetDisplayName{
			Creator: creatorStr,
			Name:    "NewName",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrDisplayNameCooldown)
	})

	t.Run("successful change after cooldown", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrMember3
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		params, _ := k.Params.Get(ctx)

		// Setup profile with old change
		profile := types.MemberProfile{
			Address:                    creatorStr,
			DisplayName:                "OldName",
			LastDisplayNameChangeEpoch: 0, // Changed long ago
		}
		k.MemberProfile.Set(ctx, creatorStr, profile)

		// Advance time past cooldown
		ctx = ctx.WithBlockHeight(int64(params.DisplayNameChangeCooldownEpochs+1) * params.EpochBlocks)

		_, err := ms.SetDisplayName(ctx, &types.MsgSetDisplayName{
			Creator: creatorStr,
			Name:    "NewName",
		})

		require.NoError(t, err)

		// Verify name was changed
		profile, _ = k.MemberProfile.Get(ctx, creatorStr)
		require.Equal(t, "NewName", profile.DisplayName)
	})

	t.Run("blocked term rejected (impersonation filter)", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		ms := keeper.NewMsgServerImpl(f.keeper)

		creator := TestAddrMember1
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		// Each value must be rejected by the substring filter (case-insensitive).
		for _, name := range []string{"admin", "Admin_Bob", "MODERATOR", "cool-team", "OfficialAccount"} {
			_, err := ms.SetDisplayName(ctx, &types.MsgSetDisplayName{
				Creator: creatorStr,
				Name:    name,
			})
			require.Error(t, err, "name %q should be rejected", name)
			require.ErrorIs(t, err, types.ErrDisplayNameBlocked, "name %q", name)
		}
	})

	t.Run("moderated display name blocks change", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		params, _ := k.Params.Get(ctx)
		ctx = ctx.WithBlockHeight(int64(params.DisplayNameChangeCooldownEpochs+1) * params.EpochBlocks)

		// Setup profile
		profile := types.MemberProfile{
			Address:     creatorStr,
			DisplayName: "",
		}
		k.MemberProfile.Set(ctx, creatorStr, profile)

		// Setup active moderation
		moderation := types.DisplayNameModeration{
			Member:       creatorStr,
			RejectedName: "BadName",
			Active:       true,
		}
		k.DisplayNameModeration.Set(ctx, creatorStr, moderation)

		_, err := ms.SetDisplayName(ctx, &types.MsgSetDisplayName{
			Creator: creatorStr,
			Name:    "NewName",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrDisplayNameModerated)
	})
}
