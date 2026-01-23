package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestMsgServerTransferDream(t *testing.T) {
	t.Run("invalid sender address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.TransferDream(f.ctx, &types.MsgTransferDream{
			Sender:    "invalid-address",
			Recipient: "addr",
			Amount:    keeper.PtrInt(math.NewInt(100)),
			Purpose:   types.TransferPurpose_TRANSFER_PURPOSE_TIP,
			Reference: "Thanks",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid sender address")
	})

	t.Run("invalid recipient address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		sender := sdk.AccAddress([]byte("sender"))
		senderStr, err := f.addressCodec.BytesToString(sender)
		require.NoError(t, err)

		_, err = ms.TransferDream(f.ctx, &types.MsgTransferDream{
			Sender:    senderStr,
			Recipient: "invalid-address",
			Amount:    keeper.PtrInt(math.NewInt(100)),
			Purpose:   types.TransferPurpose_TRANSFER_PURPOSE_TIP,
			Reference: "Thanks",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid recipient address")
	})

	t.Run("insufficient balance", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: sender with insufficient DREAM
		sender := sdk.AccAddress([]byte("sender"))
		k.Member.Set(ctx, sender.String(), types.Member{
			Address:          sender.String(),
			DreamBalance:     PtrInt(math.NewInt(50)),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		})

		recipient := sdk.AccAddress([]byte("recipient"))
		recipientStr, err := sdk.AccAddressFromBech32(recipient.String())
		require.NoError(t, err)

		senderStr, err := f.addressCodec.BytesToString(sender)
		require.NoError(t, err)

		_, err = ms.TransferDream(ctx, &types.MsgTransferDream{
			Sender:    senderStr,
			Recipient: recipientStr.String(),
			Amount:    keeper.PtrInt(math.NewInt(100)),
			Purpose:   types.TransferPurpose_TRANSFER_PURPOSE_TIP,
			Reference: "Thanks",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "member not found")
	})

	t.Run("successful transfer - tip", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: sender and recipient
		sender := sdk.AccAddress([]byte("sender"))
		k.Member.Set(ctx, sender.String(), types.Member{
			Address:          sender.String(),
			DreamBalance:     PtrInt(math.NewInt(500)),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		})

		recipient := sdk.AccAddress([]byte("recipient"))
		k.Member.Set(ctx, recipient.String(), types.Member{
			Address:          recipient.String(),
			DreamBalance:     PtrInt(math.NewInt(100)),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		})

		recipientStr, err := sdk.AccAddressFromBech32(recipient.String())
		require.NoError(t, err)

		senderStr, err := f.addressCodec.BytesToString(sender)
		require.NoError(t, err)

		// Transfer DREAM
		_, err = ms.TransferDream(ctx, &types.MsgTransferDream{
			Sender:    senderStr,
			Recipient: recipientStr.String(),
			Amount:    keeper.PtrInt(math.NewInt(50)),
			Purpose:   types.TransferPurpose_TRANSFER_PURPOSE_TIP,
			Reference: "Thanks for help",
		})
		require.NoError(t, err)

		// Verify balances (sender - 50 - 3% tax = ~48.5, recipient + 50)
		senderAfter, err := k.GetMember(ctx, sender)
		require.NoError(t, err)
		require.True(t, senderAfter.DreamBalance.LTE(math.NewInt(450))) // Decreased

		recipientAfter, err := k.GetMember(ctx, recipient)
		require.NoError(t, err)
		require.Equal(t, math.NewInt(149).String(), recipientAfter.DreamBalance.String()) // Increased
	})
}
