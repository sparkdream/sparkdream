package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestMsgServerInviteMember(t *testing.T) {
	t.Run("invalid inviter address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.InviteMember(f.ctx, &types.MsgInviteMember{
			Inviter:        "invalid-address",
			InviteeAddress: "addr",
			StakedDream:    keeper.PtrInt(math.NewInt(100)),
			VouchedTags:    []string{"tag"},
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid inviter address")
	})

	t.Run("invalid invitee address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		inviter := sdk.AccAddress([]byte("inviter"))
		inviterStr, err := f.addressCodec.BytesToString(inviter)
		require.NoError(t, err)

		_, err = ms.InviteMember(f.ctx, &types.MsgInviteMember{
			Inviter:        inviterStr,
			InviteeAddress: "invalid-address",
			StakedDream:    keeper.PtrInt(math.NewInt(100)),
			VouchedTags:    []string{"tag"},
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid invitee address")
	})

	t.Run("inviter not a member", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		inviter := sdk.AccAddress([]byte("inviter"))
		inviterStr, err := f.addressCodec.BytesToString(inviter)
		require.NoError(t, err)

		invitee := sdk.AccAddress([]byte("invitee"))
		inviteeStr, err := sdk.AccAddressFromBech32(invitee.String())
		require.NoError(t, err)

		_, err = ms.InviteMember(f.ctx, &types.MsgInviteMember{
			Inviter:        inviterStr,
			InviteeAddress: inviteeStr.String(),
			StakedDream:    keeper.PtrInt(math.NewInt(100)),
			VouchedTags:    []string{"tag"},
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "member not found")
	})

	t.Run("successful invitation", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: create inviter with DREAM balance
		inviter := sdk.AccAddress([]byte("inviter"))
		k.Member.Set(ctx, inviter.String(), types.Member{
			Address:           inviter.String(),
			DreamBalance:      keeper.PtrInt(math.NewInt(1000)),
			StakedDream:       keeper.PtrInt(math.ZeroInt()),
			LifetimeBurned:    keeper.PtrInt(math.ZeroInt()),
			ReputationScores:  map[string]string{"tag": "100.0"},
			InvitationCredits: 5,
		})

		inviterStr, err := f.addressCodec.BytesToString(inviter)
		require.NoError(t, err)

		invitee := sdk.AccAddress([]byte("invitee"))
		inviteeStr, err := sdk.AccAddressFromBech32(invitee.String())
		require.NoError(t, err)

		// Create invitation
		_, err = ms.InviteMember(ctx, &types.MsgInviteMember{
			Inviter:        inviterStr,
			InviteeAddress: inviteeStr.String(),
			StakedDream:    keeper.PtrInt(math.NewInt(100)),
			VouchedTags:    []string{"tag"},
		})
		require.NoError(t, err)

		// Verify invitation exists
		var invitation types.Invitation
		found := false
		k.Invitation.Walk(ctx, nil, func(id uint64, inv types.Invitation) (bool, error) {
			invitation = inv
			found = true
			return true, nil
		})
		require.True(t, found)
		require.Equal(t, inviterStr, invitation.Inviter)
	})
}
