package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestMsgServerTopUpTagBudget(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.TopUpTagBudget(f.ctx, &types.MsgTopUpTagBudget{
			Creator:  "invalid",
			BudgetId: 1,
			Amount:   "100",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("budget not found", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		creator := sdk.AccAddress([]byte("tb-top-1............")[:20])
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		_, err := ms.TopUpTagBudget(f.ctx, &types.MsgTopUpTagBudget{
			Creator:  creatorStr,
			BudgetId: 999,
			Amount:   "100",
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrTagBudgetNotFound)
	})

	t.Run("invalid amount", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		creator := sdk.AccAddress([]byte("tb-top-2............")[:20])
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		id, err := f.keeper.TagBudgetSeq.Next(f.ctx)
		require.NoError(t, err)
		require.NoError(t, f.keeper.TagBudget.Set(f.ctx, id, types.TagBudget{
			Id: id, GroupAccount: creatorStr, Tag: "x", PoolBalance: "100", Active: true,
		}))

		_, err = ms.TopUpTagBudget(f.ctx, &types.MsgTopUpTagBudget{
			Creator:  creatorStr,
			BudgetId: id,
			Amount:   "invalid",
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrInvalidAmount)
	})

	t.Run("successful top up", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		creator := sdk.AccAddress([]byte("tb-top-3............")[:20])
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		id, err := f.keeper.TagBudgetSeq.Next(f.ctx)
		require.NoError(t, err)
		require.NoError(t, f.keeper.TagBudget.Set(f.ctx, id, types.TagBudget{
			Id: id, GroupAccount: creatorStr, Tag: "x", PoolBalance: "1000", Active: true,
		}))

		_, err = ms.TopUpTagBudget(f.ctx, &types.MsgTopUpTagBudget{
			Creator:  creatorStr,
			BudgetId: id,
			Amount:   "500",
		})
		require.NoError(t, err)

		updated, err := f.keeper.TagBudget.Get(f.ctx, id)
		require.NoError(t, err)
		require.Equal(t, "1500", updated.PoolBalance)
	})
}
