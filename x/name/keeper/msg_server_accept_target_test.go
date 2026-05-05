package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

func TestAcceptTarget(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ms := keeper.NewMsgServerImpl(k)

	owner := sdk.AccAddress([]byte("owner_address_______")).String()
	target := sdk.AccAddress([]byte("target_address______")).String()
	stranger := sdk.AccAddress([]byte("stranger____________")).String()

	t.Run("only the current target can accept", func(t *testing.T) {
		ctx, _ := f.ctx.CacheContext()
		require.NoError(t, k.Names.Set(ctx, "kob", types.NameRecord{Name: "kob", Owner: owner, Target: target}))

		_, err := ms.AcceptTarget(ctx, &types.MsgAcceptTarget{Authority: stranger, Name: "kob"})
		require.ErrorIs(t, err, types.ErrNotTarget)

		_, err = ms.AcceptTarget(ctx, &types.MsgAcceptTarget{Authority: target, Name: "kob"})
		require.NoError(t, err)

		rec, _ := k.GetName(ctx, "kob")
		require.True(t, rec.TargetAccepted)

		has, err := k.AcceptedTargets.Has(ctx, collections.Join(target, "kob"))
		require.NoError(t, err)
		require.True(t, has)
	})

	t.Run("name with no target rejects acceptance", func(t *testing.T) {
		ctx, _ := f.ctx.CacheContext()
		require.NoError(t, k.Names.Set(ctx, "kob", types.NameRecord{Name: "kob", Owner: owner}))

		_, err := ms.AcceptTarget(ctx, &types.MsgAcceptTarget{Authority: target, Name: "kob"})
		require.ErrorIs(t, err, types.ErrTargetNotSet)
	})

	t.Run("idempotent when already accepted", func(t *testing.T) {
		ctx, _ := f.ctx.CacheContext()
		require.NoError(t, k.Names.Set(ctx, "kob", types.NameRecord{Name: "kob", Owner: owner, Target: target, TargetAccepted: true}))

		_, err := ms.AcceptTarget(ctx, &types.MsgAcceptTarget{Authority: target, Name: "kob"})
		require.NoError(t, err)
	})

	t.Run("missing name returns ErrNameNotFound", func(t *testing.T) {
		ctx, _ := f.ctx.CacheContext()
		_, err := ms.AcceptTarget(ctx, &types.MsgAcceptTarget{Authority: target, Name: "missing"})
		require.ErrorIs(t, err, types.ErrNameNotFound)
	})
}
