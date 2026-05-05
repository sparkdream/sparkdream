package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

func TestTransferName(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	mockRK := f.mockRep
	ms := keeper.NewMsgServerImpl(k)

	owner := sdk.AccAddress([]byte("owner_addr__________"))
	ownerStr := owner.String()
	recipient := sdk.AccAddress([]byte("recipient_addr______"))
	recipientStr := recipient.String()
	nonMember := sdk.AccAddress([]byte("non_member__________")).String()
	stranger := sdk.AccAddress([]byte("stranger____________")).String()

	seedName := func(c sdk.Context, n string) {
		require.NoError(t, k.Names.Set(c, n, types.NameRecord{Name: n, Owner: ownerStr}))
		require.NoError(t, k.OwnerNames.Set(c, collections.Join(ownerStr, n)))
		require.NoError(t, k.Owners.Set(c, ownerStr, types.OwnerInfo{Address: ownerStr, LastActiveTime: c.BlockTime().Unix(), PrimaryName: n}))
	}

	t.Run("success: transfers name and clears old primary", func(t *testing.T) {
		ctx, _ := f.ctx.CacheContext()
		mockRK.Reset()
		mockRK.SetActiveMember(recipientStr)
		seedName(ctx, "kob")

		_, err := ms.TransferName(ctx, &types.MsgTransferName{Authority: ownerStr, Name: "kob", NewOwner: recipientStr})
		require.NoError(t, err)

		rec, _ := k.GetName(ctx, "kob")
		require.Equal(t, recipientStr, rec.Owner)

		oldHas, _ := k.OwnerNames.Has(ctx, collections.Join(ownerStr, "kob"))
		require.False(t, oldHas)
		newHas, _ := k.OwnerNames.Has(ctx, collections.Join(recipientStr, "kob"))
		require.True(t, newHas)

		oldInfo, _ := k.Owners.Get(ctx, ownerStr)
		require.Equal(t, "", oldInfo.PrimaryName, "old owner's primary must be cleared")
	})

	t.Run("rejects non-owner", func(t *testing.T) {
		ctx, _ := f.ctx.CacheContext()
		mockRK.Reset()
		mockRK.SetActiveMember(recipientStr)
		seedName(ctx, "kob")

		_, err := ms.TransferName(ctx, &types.MsgTransferName{Authority: stranger, Name: "kob", NewOwner: recipientStr})
		require.Error(t, err)
	})

	t.Run("rejects transfer to non-member", func(t *testing.T) {
		ctx, _ := f.ctx.CacheContext()
		mockRK.Reset() // nobody is an active member
		seedName(ctx, "kob")

		_, err := ms.TransferName(ctx, &types.MsgTransferName{Authority: ownerStr, Name: "kob", NewOwner: nonMember})
		require.ErrorIs(t, err, types.ErrRecipientNotMember)
	})

	t.Run("rejects transfer to self", func(t *testing.T) {
		ctx, _ := f.ctx.CacheContext()
		mockRK.Reset()
		mockRK.SetActiveMember(ownerStr)
		seedName(ctx, "kob")

		_, err := ms.TransferName(ctx, &types.MsgTransferName{Authority: ownerStr, Name: "kob", NewOwner: ownerStr})
		require.ErrorIs(t, err, types.ErrCannotTransferToSelf)
	})

	t.Run("rejects transfer when active dispute exists", func(t *testing.T) {
		ctx, _ := f.ctx.CacheContext()
		mockRK.Reset()
		mockRK.SetActiveMember(recipientStr)
		seedName(ctx, "kob")
		require.NoError(t, k.SetDispute(ctx, types.Dispute{Name: "kob", Active: true}))

		_, err := ms.TransferName(ctx, &types.MsgTransferName{Authority: ownerStr, Name: "kob", NewOwner: recipientStr})
		require.ErrorIs(t, err, types.ErrCannotTransferDisputed)
	})

	t.Run("rejects transfer when recipient at name cap", func(t *testing.T) {
		ctx, _ := f.ctx.CacheContext()
		mockRK.Reset()
		mockRK.SetActiveMember(recipientStr)

		// Force cap to 1 and pre-load recipient with one owned name.
		params := types.DefaultParams()
		params.MaxNamesPerAddress = 1
		require.NoError(t, k.SetParams(ctx, params))

		require.NoError(t, k.OwnerNames.Set(ctx, collections.Join(recipientStr, "existing")))
		seedName(ctx, "kob")

		_, err := ms.TransferName(ctx, &types.MsgTransferName{Authority: ownerStr, Name: "kob", NewOwner: recipientStr})
		require.ErrorIs(t, err, types.ErrTooManyNames)
	})
}
