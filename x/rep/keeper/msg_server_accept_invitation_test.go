package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestMsgServerAcceptInvitation(t *testing.T) {
	t.Run("invalid invitee address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.AcceptInvitation(f.ctx, &types.MsgAcceptInvitation{
			Invitee:      "invalid-address",
			InvitationId: 1,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid invitee address")
	})

	t.Run("non-existent invitation", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		invitee := sdk.AccAddress([]byte("invitee"))
		inviteeStr, err := f.addressCodec.BytesToString(invitee)
		require.NoError(t, err)

		_, err = ms.AcceptInvitation(f.ctx, &types.MsgAcceptInvitation{
			Invitee:      inviteeStr,
			InvitationId: 99999,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invitation not found")
	})

	t.Run("successful acceptance", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Give invite credits to inviter (if param exists or manual override)
		inviter := sdk.AccAddress([]byte("inviter"))
		inviterStr, err := f.addressCodec.BytesToString(inviter)
		require.NoError(t, err)

		// Setup inviter member with reputation/credits if needed
		k.Member.Set(ctx, inviter.String(), types.Member{
			Address:           inviter.String(),
			DreamBalance:      keeper.PtrInt(math.NewInt(1000)),
			StakedDream:       keeper.PtrInt(math.NewInt(1000)),
			LifetimeEarned:    keeper.PtrInt(math.ZeroInt()),
			LifetimeBurned:    keeper.PtrInt(math.ZeroInt()),
			ReputationScores:  map[string]string{"tag": "100.0"},
			InvitationCredits: 10,
		})

		invitee := sdk.AccAddress([]byte("invitee"))
		inviteeStr, err := f.addressCodec.BytesToString(invitee)
		require.NoError(t, err)

		// Create invitation manually
		invitationID := uint64(100)
		invitation := types.Invitation{
			Id:             invitationID,
			Inviter:        inviterStr,
			InviteeAddress: inviteeStr,
			StakedDream:    keeper.PtrInt(math.NewInt(10)),
			VouchedTags:    []string{"tag"},
			Status:         types.InvitationStatus_INVITATION_STATUS_PENDING,
		}
		err = k.Invitation.Set(ctx, invitationID, invitation)
		require.NoError(t, err)

		_, err = ms.AcceptInvitation(ctx, &types.MsgAcceptInvitation{
			Invitee:      inviteeStr,
			InvitationId: invitationID,
		})
		require.NoError(t, err)
	})
}
