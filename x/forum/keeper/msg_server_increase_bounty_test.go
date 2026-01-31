package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
)

func TestMsgServerIncreaseBounty(t *testing.T) {
	f := initFixture(t)

	t.Run("invalid creator address", func(t *testing.T) {
		msg := &types.MsgIncreaseBounty{
			Creator:  "invalid",
			BountyId: 1,
			AdditionalAmount:   "100000",
		}
		_, err := f.msgServer.IncreaseBounty(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("bounty not found", func(t *testing.T) {
		msg := &types.MsgIncreaseBounty{
			Creator:  testCreator,
			BountyId: 999,
			AdditionalAmount:   "100000",
		}
		_, err := f.msgServer.IncreaseBounty(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrBountyNotFound)
	})

	t.Run("not bounty creator", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)
		bounty := f.createTestBounty(t, testCreator, post.PostId, "1000000")

		msg := &types.MsgIncreaseBounty{
			Creator:  testCreator2,
			BountyId: bounty.Id,
			AdditionalAmount:   "100000",
		}
		_, err := f.msgServer.IncreaseBounty(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotBountyCreator)
	})

	t.Run("bounty not active", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)
		bounty := f.createTestBounty(t, testCreator, post.PostId, "1000000")
		bounty.Status = types.BountyStatus_BOUNTY_STATUS_AWARDED
		f.keeper.Bounty.Set(f.ctx, bounty.Id, bounty)

		msg := &types.MsgIncreaseBounty{
			Creator:  testCreator,
			BountyId: bounty.Id,
			AdditionalAmount:   "100000",
		}
		_, err := f.msgServer.IncreaseBounty(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrBountyNotActive)
	})

	t.Run("invalid amount", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)
		bounty := f.createTestBounty(t, testCreator, post.PostId, "1000000")

		msg := &types.MsgIncreaseBounty{
			Creator:  testCreator,
			BountyId: bounty.Id,
			AdditionalAmount:   "invalid",
		}
		_, err := f.msgServer.IncreaseBounty(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrInvalidAmount)
	})

	t.Run("successful increase", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)
		bounty := f.createTestBounty(t, testCreator, post.PostId, "1000000")

		msg := &types.MsgIncreaseBounty{
			Creator:  testCreator,
			BountyId: bounty.Id,
			AdditionalAmount:   "500000",
		}
		_, err := f.msgServer.IncreaseBounty(f.ctx, msg)
		require.NoError(t, err)

		// Verify bounty amount increased
		updatedBounty, err := f.keeper.Bounty.Get(f.ctx, bounty.Id)
		require.NoError(t, err)
		require.Equal(t, "1500000", updatedBounty.Amount)
	})
}
