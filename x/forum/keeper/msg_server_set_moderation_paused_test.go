package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
)

func TestMsgServerSetModerationPaused(t *testing.T) {
	f := initFixture(t)
	authority, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())

	t.Run("invalid creator address", func(t *testing.T) {
		msg := &types.MsgSetModerationPaused{
			Creator: "invalid",
			Paused:  true,
		}
		_, err := f.msgServer.SetModerationPaused(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("not governance authority", func(t *testing.T) {
		msg := &types.MsgSetModerationPaused{
			Creator: testCreator,
			Paused:  true,
		}
		_, err := f.msgServer.SetModerationPaused(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotGovAuthority)
	})

	t.Run("governance authority pauses moderation", func(t *testing.T) {
		msg := &types.MsgSetModerationPaused{
			Creator: authority,
			Paused:  true,
		}
		_, err := f.msgServer.SetModerationPaused(f.ctx, msg)
		require.NoError(t, err)

		// Verify params updated
		params, err := f.keeper.Params.Get(f.ctx)
		require.NoError(t, err)
		require.True(t, params.ModerationPaused)
	})

	t.Run("governance authority unpauses moderation", func(t *testing.T) {
		// First pause
		params := types.DefaultParams()
		params.ModerationPaused = true
		f.keeper.Params.Set(f.ctx, params)

		msg := &types.MsgSetModerationPaused{
			Creator: authority,
			Paused:  false,
		}
		_, err := f.msgServer.SetModerationPaused(f.ctx, msg)
		require.NoError(t, err)

		// Verify params updated
		updatedParams, err := f.keeper.Params.Get(f.ctx)
		require.NoError(t, err)
		require.False(t, updatedParams.ModerationPaused)
	})
}
