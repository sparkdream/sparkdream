package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
)

func TestMsgServerDismissFlags(t *testing.T) {
	f := initFixture(t)
	authority, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())

	t.Run("invalid creator address", func(t *testing.T) {
		msg := &types.MsgDismissFlags{
			Creator: "invalid",
			PostId:  1,
		}
		_, err := f.msgServer.DismissFlags(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("not authorized", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		// Create flag record
		flag := types.PostFlag{
			PostId:        post.PostId,
			TotalWeight:   "100",
			InReviewQueue: true,
		}
		f.keeper.PostFlag.Set(f.ctx, post.PostId, flag)

		msg := &types.MsgDismissFlags{
			Creator: testCreator2, // Not sentinel or authority
			PostId:  post.PostId,
		}
		_, err := f.msgServer.DismissFlags(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrUnauthorized)
	})

	t.Run("flag not found", func(t *testing.T) {
		msg := &types.MsgDismissFlags{
			Creator: authority,
			PostId:  999,
		}
		_, err := f.msgServer.DismissFlags(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrFlagNotFound)
	})

	t.Run("governance authority dismisses flags", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		// Create flag record
		flag := types.PostFlag{
			PostId:        post.PostId,
			TotalWeight:   "100",
			InReviewQueue: true,
		}
		f.keeper.PostFlag.Set(f.ctx, post.PostId, flag)

		msg := &types.MsgDismissFlags{
			Creator: authority,
			PostId:  post.PostId,
		}
		_, err := f.msgServer.DismissFlags(f.ctx, msg)
		require.NoError(t, err)

		// Verify flag was removed
		_, err = f.keeper.PostFlag.Get(f.ctx, post.PostId)
		require.Error(t, err)
	})

	t.Run("sentinel dismisses flags in review queue", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)
		f.createTestSentinel(t, testSentinel, "1000")

		// Create flag record in review queue
		flag := types.PostFlag{
			PostId:        post.PostId,
			TotalWeight:   "100",
			InReviewQueue: true,
		}
		f.keeper.PostFlag.Set(f.ctx, post.PostId, flag)

		msg := &types.MsgDismissFlags{
			Creator: testSentinel,
			PostId:  post.PostId,
		}
		_, err := f.msgServer.DismissFlags(f.ctx, msg)
		require.NoError(t, err)

		// Verify flag was removed
		_, err = f.keeper.PostFlag.Get(f.ctx, post.PostId)
		require.Error(t, err)
	})

	t.Run("sentinel cannot dismiss flags not in review queue", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)
		f.createTestSentinel(t, testCreator2, "1000")

		// Create flag record NOT in review queue
		flag := types.PostFlag{
			PostId:        post.PostId,
			TotalWeight:   "50",
			InReviewQueue: false,
		}
		f.keeper.PostFlag.Set(f.ctx, post.PostId, flag)

		msg := &types.MsgDismissFlags{
			Creator: testCreator2,
			PostId:  post.PostId,
		}
		_, err := f.msgServer.DismissFlags(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotInReviewQueue)
	})

	t.Run("demoted sentinel cannot dismiss flags", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		// Create demoted sentinel
		sentinel := types.SentinelActivity{
			Address:     testSentinel,
			CurrentBond: "100",
			BondStatus:  types.SentinelBondStatus_SENTINEL_BOND_STATUS_DEMOTED,
		}
		f.keeper.SentinelActivity.Set(f.ctx, testSentinel, sentinel)

		// Create flag record
		flag := types.PostFlag{
			PostId:        post.PostId,
			TotalWeight:   "100",
			InReviewQueue: true,
		}
		f.keeper.PostFlag.Set(f.ctx, post.PostId, flag)

		msg := &types.MsgDismissFlags{
			Creator: testSentinel,
			PostId:  post.PostId,
		}
		_, err := f.msgServer.DismissFlags(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrUnauthorized)
	})
}
