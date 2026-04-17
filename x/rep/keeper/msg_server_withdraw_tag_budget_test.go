package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestMsgServerWithdrawTagBudget(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.WithdrawTagBudget(f.ctx, &types.MsgWithdrawTagBudget{
			Creator:  "invalid",
			BudgetId: 1,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("budget not found", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		creator := sdk.AccAddress([]byte("tb-wd-1.............")[:20])
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		_, err := ms.WithdrawTagBudget(f.ctx, &types.MsgWithdrawTagBudget{
			Creator:  creatorStr,
			BudgetId: 999,
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrTagBudgetNotFound)
	})

	t.Run("not group account", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		owner := sdk.AccAddress([]byte("tb-wd-own...........")[:20])
		ownerStr, _ := f.addressCodec.BytesToString(owner)
		other := sdk.AccAddress([]byte("tb-wd-other.........")[:20])
		otherStr, _ := f.addressCodec.BytesToString(other)

		id, _ := f.keeper.TagBudgetSeq.Next(f.ctx)
		require.NoError(t, f.keeper.TagBudget.Set(f.ctx, id, types.TagBudget{
			Id: id, GroupAccount: ownerStr, Tag: "x", PoolBalance: "100", Active: true,
		}))

		_, err := ms.WithdrawTagBudget(f.ctx, &types.MsgWithdrawTagBudget{
			Creator:  otherStr,
			BudgetId: id,
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotGroupAccount)
	})

	t.Run("empty pool", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		owner := sdk.AccAddress([]byte("tb-wd-2.............")[:20])
		ownerStr, _ := f.addressCodec.BytesToString(owner)

		id, _ := f.keeper.TagBudgetSeq.Next(f.ctx)
		require.NoError(t, f.keeper.TagBudget.Set(f.ctx, id, types.TagBudget{
			Id: id, GroupAccount: ownerStr, Tag: "x", PoolBalance: "0", Active: true,
		}))

		_, err := ms.WithdrawTagBudget(f.ctx, &types.MsgWithdrawTagBudget{
			Creator:  ownerStr,
			BudgetId: id,
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrTagBudgetInsufficient)
	})

	t.Run("successful withdraw", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		owner := sdk.AccAddress([]byte("tb-wd-3.............")[:20])
		ownerStr, _ := f.addressCodec.BytesToString(owner)

		id, _ := f.keeper.TagBudgetSeq.Next(f.ctx)
		require.NoError(t, f.keeper.TagBudget.Set(f.ctx, id, types.TagBudget{
			Id: id, GroupAccount: ownerStr, Tag: "x", PoolBalance: "1000", Active: true,
		}))

		_, err := ms.WithdrawTagBudget(f.ctx, &types.MsgWithdrawTagBudget{
			Creator:  ownerStr,
			BudgetId: id,
		})
		require.NoError(t, err)

		updated, err := f.keeper.TagBudget.Get(f.ctx, id)
		require.NoError(t, err)
		require.Equal(t, "0", updated.PoolBalance)
		require.False(t, updated.Active)
	})
}
