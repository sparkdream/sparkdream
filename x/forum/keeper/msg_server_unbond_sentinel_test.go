package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
)

func TestMsgServerUnbondSentinel(t *testing.T) {
	f := initFixture(t)

	t.Run("invalid creator address", func(t *testing.T) {
		msg := &types.MsgUnbondSentinel{
			Creator: "invalid",
			Amount:  "1000",
		}
		_, err := f.msgServer.UnbondSentinel(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("sentinel not found", func(t *testing.T) {
		msg := &types.MsgUnbondSentinel{
			Creator: testCreator,
			Amount:  "1000",
		}
		_, err := f.msgServer.UnbondSentinel(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrSentinelNotFound)
	})

	t.Run("invalid unbond amount", func(t *testing.T) {
		f.createTestSentinel(t, testCreator, "2000")

		msg := &types.MsgUnbondSentinel{
			Creator: testCreator,
			Amount:  "invalid",
		}
		_, err := f.msgServer.UnbondSentinel(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrInvalidAmount)
	})

	t.Run("zero unbond amount", func(t *testing.T) {
		f.createTestSentinel(t, testCreator2, "2000")

		msg := &types.MsgUnbondSentinel{
			Creator: testCreator2,
			Amount:  "0",
		}
		_, err := f.msgServer.UnbondSentinel(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrInvalidAmount)
	})

	t.Run("unbond more than bonded", func(t *testing.T) {
		sentinel := f.createTestSentinel(t, testSentinel, "1000")
		require.Equal(t, "1000", sentinel.CurrentBond)

		msg := &types.MsgUnbondSentinel{
			Creator: testSentinel,
			Amount:  "2000",
		}
		_, err := f.msgServer.UnbondSentinel(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrInsufficientBond)
	})

	t.Run("successful unbond", func(t *testing.T) {
		addr := testCreator
		sentinel := types.SentinelActivity{
			Address:            addr,
			CurrentBond:        "3000",
			TotalCommittedBond: "0",
			BondStatus:         types.SentinelBondStatus_SENTINEL_BOND_STATUS_NORMAL,
		}
		f.keeper.SentinelActivity.Set(f.ctx, addr, sentinel)

		msg := &types.MsgUnbondSentinel{
			Creator: addr,
			Amount:  "1000",
		}
		_, err := f.msgServer.UnbondSentinel(f.ctx, msg)
		require.NoError(t, err)

		// Verify bond decreased
		updatedSentinel, err := f.keeper.SentinelActivity.Get(f.ctx, addr)
		require.NoError(t, err)
		require.Equal(t, "2000", updatedSentinel.CurrentBond)
	})

	t.Run("cannot unbond committed bond", func(t *testing.T) {
		addr := testCreator2
		sentinel := types.SentinelActivity{
			Address:            addr,
			CurrentBond:        "2000",
			TotalCommittedBond: "1500", // 1500 committed
			BondStatus:         types.SentinelBondStatus_SENTINEL_BOND_STATUS_NORMAL,
		}
		f.keeper.SentinelActivity.Set(f.ctx, addr, sentinel)

		msg := &types.MsgUnbondSentinel{
			Creator: addr,
			Amount:  "1000", // Only 500 available (2000 - 1500)
		}
		_, err := f.msgServer.UnbondSentinel(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrInsufficientBond)
	})

	t.Run("cannot unbond with pending hides", func(t *testing.T) {
		addr := testSentinel
		sentinel := types.SentinelActivity{
			Address:          addr,
			CurrentBond:      "2000",
			PendingHideCount: 2,
			BondStatus:       types.SentinelBondStatus_SENTINEL_BOND_STATUS_NORMAL,
		}
		f.keeper.SentinelActivity.Set(f.ctx, addr, sentinel)

		msg := &types.MsgUnbondSentinel{
			Creator: addr,
			Amount:  "500",
		}
		_, err := f.msgServer.UnbondSentinel(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrCannotUnbondPendingHides)
	})
}
