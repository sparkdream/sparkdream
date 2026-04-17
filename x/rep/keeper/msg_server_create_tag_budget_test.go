package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestMsgServerCreateTagBudget(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.CreateTagBudget(f.ctx, &types.MsgCreateTagBudget{
			Creator:     "invalid",
			Tag:         "golang",
			InitialPool: "1000",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("tag not found", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		creator := sdk.AccAddress([]byte("tb-create-1..........")[:20])
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		_, err := ms.CreateTagBudget(f.ctx, &types.MsgCreateTagBudget{
			Creator:     creatorStr,
			Tag:         "nonexistent-tag",
			InitialPool: "1000",
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrTagNotFound)
	})

	t.Run("invalid initial pool amount", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		creator := sdk.AccAddress([]byte("tb-create-2..........")[:20])
		creatorStr, _ := f.addressCodec.BytesToString(creator)
		require.NoError(t, f.keeper.SetTag(f.ctx, types.Tag{Name: "tb-tag"}))

		_, err := ms.CreateTagBudget(f.ctx, &types.MsgCreateTagBudget{
			Creator:     creatorStr,
			Tag:         "tb-tag",
			InitialPool: "0",
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrInvalidAmount)
	})

	t.Run("successful creation", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		creator := sdk.AccAddress([]byte("tb-create-3..........")[:20])
		creatorStr, _ := f.addressCodec.BytesToString(creator)
		require.NoError(t, f.keeper.SetTag(f.ctx, types.Tag{Name: "tb-tag-ok"}))

		_, err := ms.CreateTagBudget(f.ctx, &types.MsgCreateTagBudget{
			Creator:     creatorStr,
			Tag:         "tb-tag-ok",
			InitialPool: "1000",
			MembersOnly: true,
		})
		require.NoError(t, err)

		var found bool
		iter, _ := f.keeper.TagBudget.Iterate(f.ctx, nil)
		for ; iter.Valid(); iter.Next() {
			budget, _ := iter.Value()
			if budget.Tag == "tb-tag-ok" && budget.GroupAccount == creatorStr {
				found = true
				require.Equal(t, "1000", budget.PoolBalance)
				require.True(t, budget.MembersOnly)
				require.True(t, budget.Active)
				break
			}
		}
		iter.Close()
		require.True(t, found, "budget should have been created")
	})

	t.Run("duplicate active budget", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		creator := sdk.AccAddress([]byte("tb-create-4..........")[:20])
		creatorStr, _ := f.addressCodec.BytesToString(creator)
		require.NoError(t, f.keeper.SetTag(f.ctx, types.Tag{Name: "tb-dup"}))

		_, err := ms.CreateTagBudget(f.ctx, &types.MsgCreateTagBudget{
			Creator:     creatorStr,
			Tag:         "tb-dup",
			InitialPool: "1000",
		})
		require.NoError(t, err)

		_, err = ms.CreateTagBudget(f.ctx, &types.MsgCreateTagBudget{
			Creator:     creatorStr,
			Tag:         "tb-dup",
			InitialPool: "500",
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrTagBudgetAlreadyExists)
	})
}
