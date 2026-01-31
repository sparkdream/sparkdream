package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
)

func TestMsgServerBondSentinel(t *testing.T) {
	f := initFixture(t)

	t.Run("invalid creator address", func(t *testing.T) {
		msg := &types.MsgBondSentinel{
			Creator: "invalid",
			Amount:  "1000",
		}
		_, err := f.msgServer.BondSentinel(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("invalid bond amount", func(t *testing.T) {
		msg := &types.MsgBondSentinel{
			Creator: testCreator,
			Amount:  "invalid",
		}
		_, err := f.msgServer.BondSentinel(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrInvalidAmount)
	})

	t.Run("negative bond amount", func(t *testing.T) {
		msg := &types.MsgBondSentinel{
			Creator: testCreator,
			Amount:  "-1000",
		}
		_, err := f.msgServer.BondSentinel(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrInvalidAmount)
	})

	t.Run("bond amount too small", func(t *testing.T) {
		msg := &types.MsgBondSentinel{
			Creator: testCreator,
			Amount:  "100", // Below minimum of 1000
		}
		_, err := f.msgServer.BondSentinel(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrBondAmountTooSmall)
	})

	t.Run("successful bond", func(t *testing.T) {
		msg := &types.MsgBondSentinel{
			Creator: testCreator,
			Amount:  "2000",
		}
		_, err := f.msgServer.BondSentinel(f.ctx, msg)
		require.NoError(t, err)

		// Verify sentinel activity was created
		sentinel, err := f.keeper.SentinelActivity.Get(f.ctx, testCreator)
		require.NoError(t, err)
		require.Equal(t, "2000", sentinel.CurrentBond)
		require.Equal(t, types.SentinelBondStatus_SENTINEL_BOND_STATUS_NORMAL, sentinel.BondStatus)
	})

	t.Run("add to existing bond", func(t *testing.T) {
		// First create a sentinel with initial bond
		sentinel := f.createTestSentinel(t, testCreator2, "1000")
		require.Equal(t, "1000", sentinel.CurrentBond)

		msg := &types.MsgBondSentinel{
			Creator: testCreator2,
			Amount:  "1500",
		}
		_, err := f.msgServer.BondSentinel(f.ctx, msg)
		require.NoError(t, err)

		// Verify bond was increased
		updatedSentinel, err := f.keeper.SentinelActivity.Get(f.ctx, testCreator2)
		require.NoError(t, err)
		require.Equal(t, "2500", updatedSentinel.CurrentBond)
	})

	t.Run("demotion cooldown", func(t *testing.T) {
		sentinel := f.createTestSentinel(t, testSentinel, "1000")
		sentinel.DemotionCooldownUntil = f.sdkCtx().BlockTime().Unix() + 86400 // Cooldown active
		f.keeper.SentinelActivity.Set(f.ctx, testSentinel, sentinel)

		msg := &types.MsgBondSentinel{
			Creator: testSentinel,
			Amount:  "1000",
		}
		_, err := f.msgServer.BondSentinel(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrDemotionCooldown)
	})
}
