package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
)

func TestMsgServerAwardBounty(t *testing.T) {
	f := initFixture(t)

	t.Run("invalid creator address", func(t *testing.T) {
		msg := &types.MsgAwardBounty{
			Creator:  "invalid",
			BountyId: 1,
		}
		_, err := f.msgServer.AwardBounty(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("bounty not found", func(t *testing.T) {
		msg := &types.MsgAwardBounty{
			Creator:  testCreator,
			BountyId: 999,
		}
		_, err := f.msgServer.AwardBounty(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrBountyNotFound)
	})

	t.Run("not bounty creator", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)
		bounty := f.createTestBounty(t, testCreator, post.PostId, "1000000")

		msg := &types.MsgAwardBounty{
			Creator:  testCreator2,
			BountyId: bounty.Id,
		}
		_, err := f.msgServer.AwardBounty(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotBountyCreator)
	})

	t.Run("bounty not active", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)
		bounty := f.createTestBounty(t, testCreator, post.PostId, "1000000")
		bounty.Status = types.BountyStatus_BOUNTY_STATUS_CANCELLED
		f.keeper.Bounty.Set(f.ctx, bounty.Id, bounty)

		msg := &types.MsgAwardBounty{
			Creator:  testCreator,
			BountyId: bounty.Id,
		}
		_, err := f.msgServer.AwardBounty(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrBountyNotActive)
	})

	t.Run("no awards assigned yet", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)
		bounty := f.createTestBounty(t, testCreator, post.PostId, "1000000")

		msg := &types.MsgAwardBounty{
			Creator:  testCreator,
			BountyId: bounty.Id,
		}
		_, err := f.msgServer.AwardBounty(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrBountyNotActive)
	})

	t.Run("successful award with assignments", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)
		bounty := f.createTestBounty(t, testCreator, post.PostId, "1000000")

		// Add award to bounty
		bounty.Awards = append(bounty.Awards, &types.BountyAward{
			PostId:    100,
			Recipient: testCreator2,
			Amount:    "500000",
		})
		f.keeper.Bounty.Set(f.ctx, bounty.Id, bounty)

		msg := &types.MsgAwardBounty{
			Creator:  testCreator,
			BountyId: bounty.Id,
		}
		_, err := f.msgServer.AwardBounty(f.ctx, msg)
		require.NoError(t, err)

		// Verify bounty status changed
		updatedBounty, err := f.keeper.Bounty.Get(f.ctx, bounty.Id)
		require.NoError(t, err)
		require.Equal(t, types.BountyStatus_BOUNTY_STATUS_AWARDED, updatedBounty.Status)
	})
}
