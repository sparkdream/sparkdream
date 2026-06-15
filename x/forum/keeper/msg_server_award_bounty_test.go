package keeper_test

import (
	"context"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
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

	t.Run("single winner receives full escrow", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)
		bounty := f.createTestBounty(t, testCreator, post.PostId, "1000000")

		bounty.Awards = append(bounty.Awards, &types.BountyAward{
			PostId:    100,
			Recipient: testCreator2,
		})
		f.keeper.Bounty.Set(f.ctx, bounty.Id, bounty)

		type transfer struct {
			recipient string
			amount    string
		}
		var transfers []transfer
		f.bankKeeper.SendCoinsFromModuleToAccountFn = func(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
			transfers = append(transfers, transfer{
				recipient: recipientAddr.String(),
				amount:    amt.String(),
			})
			return nil
		}
		defer func() { f.bankKeeper.SendCoinsFromModuleToAccountFn = nil }()

		msg := &types.MsgAwardBounty{
			Creator:  testCreator,
			BountyId: bounty.Id,
		}
		_, err := f.msgServer.AwardBounty(f.ctx, msg)
		require.NoError(t, err)

		denom := f.keeper.BondDenom(f.ctx)
		require.Len(t, transfers, 1)
		require.Equal(t, testCreator2, transfers[0].recipient)
		require.Equal(t, "1000000"+denom, transfers[0].amount)
	})

	t.Run("escrow split equally with largest-remainder distribution", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)
		bounty := f.createTestBounty(t, testCreator, post.PostId, "1000000")

		// 1000000 / 3 = 333333 rem 1 — first award gets the extra unit
		recipients := []string{testCreator2, testSentinel, testCreator2}
		for i, r := range recipients {
			bounty.Awards = append(bounty.Awards, &types.BountyAward{
				PostId:    uint64(100 + i),
				Recipient: r,
			})
		}
		f.keeper.Bounty.Set(f.ctx, bounty.Id, bounty)

		var amounts []string
		f.bankKeeper.SendCoinsFromModuleToAccountFn = func(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
			amounts = append(amounts, amt.AmountOf(f.keeper.BondDenom(ctx)).String())
			return nil
		}
		defer func() { f.bankKeeper.SendCoinsFromModuleToAccountFn = nil }()

		msg := &types.MsgAwardBounty{
			Creator:  testCreator,
			BountyId: bounty.Id,
		}
		_, err := f.msgServer.AwardBounty(f.ctx, msg)
		require.NoError(t, err)

		// Full escrow paid out, no dust left behind
		require.Equal(t, []string{"333334", "333333", "333333"}, amounts)

		// Paid shares persisted on the awards for the audit trail
		updated, err := f.keeper.Bounty.Get(f.ctx, bounty.Id)
		require.NoError(t, err)
		require.Equal(t, "333334", updated.Awards[0].Amount)
		require.Equal(t, "333333", updated.Awards[1].Amount)
		require.Equal(t, "333333", updated.Awards[2].Amount)
	})
}
