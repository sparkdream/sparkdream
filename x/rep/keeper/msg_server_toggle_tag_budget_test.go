package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestMsgServerToggleTagBudget(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.ToggleTagBudget(f.ctx, &types.MsgToggleTagBudget{
			Creator:  "invalid",
			BudgetId: 1,
			Active:   false,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("budget not found", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		creator := sdk.AccAddress([]byte("tb-tog-1............")[:20])
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		_, err := ms.ToggleTagBudget(f.ctx, &types.MsgToggleTagBudget{
			Creator:  creatorStr,
			BudgetId: 999,
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrTagBudgetNotFound)
	})

	t.Run("not group account", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		owner := sdk.AccAddress([]byte("tb-tog-own..........")[:20])
		ownerStr, _ := f.addressCodec.BytesToString(owner)
		other := sdk.AccAddress([]byte("tb-tog-other........")[:20])
		otherStr, _ := f.addressCodec.BytesToString(other)

		id, _ := f.keeper.TagBudgetSeq.Next(f.ctx)
		require.NoError(t, f.keeper.TagBudget.Set(f.ctx, id, types.TagBudget{
			Id: id, GroupAccount: ownerStr, Tag: "x", PoolBalance: "100", Active: true,
		}))

		_, err := ms.ToggleTagBudget(f.ctx, &types.MsgToggleTagBudget{
			Creator:  otherStr,
			BudgetId: id,
			Active:   false,
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotGroupAccount)
	})

	t.Run("pause and resume", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		owner := sdk.AccAddress([]byte("tb-tog-2............")[:20])
		ownerStr, _ := f.addressCodec.BytesToString(owner)

		id, _ := f.keeper.TagBudgetSeq.Next(f.ctx)
		require.NoError(t, f.keeper.TagBudget.Set(f.ctx, id, types.TagBudget{
			Id: id, GroupAccount: ownerStr, Tag: "x", PoolBalance: "100", Active: true,
		}))

		_, err := ms.ToggleTagBudget(f.ctx, &types.MsgToggleTagBudget{
			Creator:  ownerStr,
			BudgetId: id,
			Active:   false,
		})
		require.NoError(t, err)
		got, _ := f.keeper.TagBudget.Get(f.ctx, id)
		require.False(t, got.Active)

		_, err = ms.ToggleTagBudget(f.ctx, &types.MsgToggleTagBudget{
			Creator:  ownerStr,
			BudgetId: id,
			Active:   true,
		})
		require.NoError(t, err)
		got, _ = f.keeper.TagBudget.Get(f.ctx, id)
		require.True(t, got.Active)
	})
}
