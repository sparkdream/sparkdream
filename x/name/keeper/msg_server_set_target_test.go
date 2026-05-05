package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

func TestSetTarget(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ms := keeper.NewMsgServerImpl(k)

	owner := sdk.AccAddress([]byte("owner_address_______")).String()
	target1 := sdk.AccAddress([]byte("target_one__________")).String()
	target2 := sdk.AccAddress([]byte("target_two__________")).String()
	stranger := sdk.AccAddress([]byte("stranger____________")).String()

	seedName := func(c sdk.Context, n string, ownerAddr string) {
		require.NoError(t, k.Names.Set(c, n, types.NameRecord{Name: n, Owner: ownerAddr}))
	}

	t.Run("owner can set and clear target", func(t *testing.T) {
		ctx, _ := f.ctx.CacheContext()
		seedName(ctx, "kob", owner)

		_, err := ms.SetTarget(ctx, &types.MsgSetTarget{Authority: owner, Name: "kob", Target: target1})
		require.NoError(t, err)
		rec, _ := k.GetName(ctx, "kob")
		require.Equal(t, target1, rec.Target)
		require.False(t, rec.TargetAccepted)

		_, err = ms.SetTarget(ctx, &types.MsgSetTarget{Authority: owner, Name: "kob", Target: ""})
		require.NoError(t, err)
		rec, _ = k.GetName(ctx, "kob")
		require.Equal(t, "", rec.Target)
	})

	t.Run("non-owner cannot set target", func(t *testing.T) {
		ctx, _ := f.ctx.CacheContext()
		seedName(ctx, "kob", owner)

		_, err := ms.SetTarget(ctx, &types.MsgSetTarget{Authority: stranger, Name: "kob", Target: target1})
		require.Error(t, err)
	})

	t.Run("changing target revokes acceptance and clears target's primary", func(t *testing.T) {
		ctx, _ := f.ctx.CacheContext()
		seedName(ctx, "kob", owner)

		// First target accepts and sets primary
		_, err := ms.SetTarget(ctx, &types.MsgSetTarget{Authority: owner, Name: "kob", Target: target1})
		require.NoError(t, err)
		_, err = ms.AcceptTarget(ctx, &types.MsgAcceptTarget{Authority: target1, Name: "kob"})
		require.NoError(t, err)
		_, err = ms.SetPrimary(ctx, &types.MsgSetPrimary{Authority: target1, Name: "kob"})
		require.NoError(t, err)

		info, _ := k.Owners.Get(ctx, target1)
		require.Equal(t, "kob", info.PrimaryName)

		// Owner re-points to a new target — must revoke first acceptance and clear primary
		_, err = ms.SetTarget(ctx, &types.MsgSetTarget{Authority: owner, Name: "kob", Target: target2})
		require.NoError(t, err)

		rec, _ := k.GetName(ctx, "kob")
		require.Equal(t, target2, rec.Target)
		require.False(t, rec.TargetAccepted)

		has, err := k.AcceptedTargets.Has(ctx, collections.Join(target1, "kob"))
		require.NoError(t, err)
		require.False(t, has, "old target should be removed from AcceptedTargets")

		info, _ = k.Owners.Get(ctx, target1)
		require.Equal(t, "", info.PrimaryName, "old target's primary must be cleared")
	})

	t.Run("invalid target address rejected", func(t *testing.T) {
		ctx, _ := f.ctx.CacheContext()
		seedName(ctx, "kob", owner)

		_, err := ms.SetTarget(ctx, &types.MsgSetTarget{Authority: owner, Name: "kob", Target: "not-a-bech32"})
		require.Error(t, err)
	})

	t.Run("missing name returns ErrNameNotFound", func(t *testing.T) {
		ctx, _ := f.ctx.CacheContext()
		_, err := ms.SetTarget(ctx, &types.MsgSetTarget{Authority: owner, Name: "missing", Target: target1})
		require.ErrorIs(t, err, types.ErrNameNotFound)
	})
}
