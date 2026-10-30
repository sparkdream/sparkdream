package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/forum/types"
)

type bountyMoves struct {
	burned map[string]string
	paid   map[string]string
}

func (f *fixture) trackBountyMoves(t *testing.T) *bountyMoves {
	t.Helper()
	m := &bountyMoves{burned: map[string]string{}, paid: map[string]string{}}
	denom := f.keeper.BondDenom(f.ctx)
	f.bankKeeper.BurnCoinsFn = func(_ context.Context, _ string, amt sdk.Coins) error {
		m.burned["total"] = amt.AmountOf(denom).String()
		return nil
	}
	f.bankKeeper.SendCoinsFromModuleToAccountFn = func(_ context.Context, _ string, to sdk.AccAddress, amt sdk.Coins) error {
		m.paid[to.String()] = amt.AmountOf(denom).String()
		return nil
	}
	t.Cleanup(func() {
		f.bankKeeper.BurnCoinsFn = nil
		f.bankKeeper.SendCoinsFromModuleToAccountFn = nil
	})
	return m
}

func (f *fixture) expireBountyNow(t *testing.T, id uint64) {
	t.Helper()
	b, err := f.keeper.Bounty.Get(f.ctx, id)
	require.NoError(t, err)
	require.NoError(t, f.keeper.BountiesByExpiry.Remove(f.ctx, collections.Join(b.ExpiresAt, id)))
	b.ExpiresAt = f.sdkCtx().BlockTime().Unix() - 1
	require.NoError(t, f.keeper.Bounty.Set(f.ctx, id, b))
	require.NoError(t, f.keeper.BountiesByExpiry.Set(f.ctx, collections.Join(b.ExpiresAt, id)))
	require.NoError(t, f.keeper.ExpireBounties(f.ctx, f.sdkCtx().BlockTime().Unix()))
}

// FRM-11: an unawarded bounty that expires burns BountyExpiryBurnBps and pays
// the rest to the most-upvoted eligible reply; the creator's own reply never
// qualifies.
func TestBountyExpiryBurnsAndPaysTopReply(t *testing.T) {
	f := initFixture(t)
	f.at(reviewNow)
	root := f.createTestPost(t, testCreator, 0, 0)
	bounty := f.createTestBounty(t, testCreator, root.PostId, "1000000")

	own := f.createTestPost(t, testCreator, root.PostId, 0)
	own.UpvoteCount = 10
	require.NoError(t, f.keeper.Post.Set(f.ctx, own.PostId, own))
	best := f.createTestPost(t, testCreator2, root.PostId, 0)
	best.UpvoteCount = 3
	require.NoError(t, f.keeper.Post.Set(f.ctx, best.PostId, best))
	other := f.createTestPost(t, testSentinel, root.PostId, 0)
	other.UpvoteCount = 1
	require.NoError(t, f.keeper.Post.Set(f.ctx, other.PostId, other))

	moves := f.trackBountyMoves(t)
	f.expireBountyNow(t, bounty.Id)

	require.Equal(t, "500000", moves.burned["total"])
	require.Equal(t, map[string]string{testCreator2: "500000"}, moves.paid)
	b, err := f.keeper.Bounty.Get(f.ctx, bounty.Id)
	require.NoError(t, err)
	require.Equal(t, types.BountyStatus_BOUNTY_STATUS_EXPIRED, b.Status)
	require.Len(t, b.Awards, 1)
	require.Equal(t, best.PostId, b.Awards[0].PostId)
	has, err := f.keeper.ActiveBountyByThread.Has(f.ctx, root.PostId)
	require.NoError(t, err)
	require.False(t, has)
}

// FRM-11: moderating the thread root suspends the bounty timer; a reversal
// resumes it with the time it had left; a hide that stands cancels it with
// the moderation fee burned and the rest refunded.
func TestBountyModerationSuspendResumeCancel(t *testing.T) {
	f := initFixture(t)
	f.at(reviewNow)
	cat := f.createTestCategory(t, "bounties")
	root := f.createTestPost(t, testCreator, 0, cat.CategoryId)
	bounty := f.createTestBounty(t, testCreator, root.PostId, "1000000")
	authority := f.govAuthority(t)
	hide := func() {
		_, err := f.msgServer.HidePost(f.ctx, &types.MsgHidePost{
			Creator:    authority,
			PostId:     root.PostId,
			ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_POLICY_VIOLATION),
		})
		require.NoError(t, err)
	}

	// Hide 1000 s before expiry: suspended with 1000 s left, out of the index.
	f.at(bounty.ExpiresAt - 1000)
	hide()
	b, err := f.keeper.Bounty.Get(f.ctx, bounty.Id)
	require.NoError(t, err)
	require.Equal(t, types.BountyStatus_BOUNTY_STATUS_MODERATION_PENDING, b.Status)
	require.Equal(t, int64(1000), b.TimeRemainingAtSuspension)
	has, err := f.keeper.BountiesByExpiry.Has(f.ctx, collections.Join(bounty.ExpiresAt, bounty.Id))
	require.NoError(t, err)
	require.False(t, has)

	// Past the original deadline nothing expires while suspended.
	f.at(bounty.ExpiresAt + 5000)
	require.NoError(t, f.keeper.ExpireBounties(f.ctx, f.sdkCtx().BlockTime().Unix()))
	b, _ = f.keeper.Bounty.Get(f.ctx, bounty.Id)
	require.Equal(t, types.BountyStatus_BOUNTY_STATUS_MODERATION_PENDING, b.Status)

	// Council unhide: resumes with the captured 1000 s.
	_, err = f.msgServer.UnhidePost(f.ctx, &types.MsgUnhidePost{Creator: authority, PostId: root.PostId})
	require.NoError(t, err)
	now := f.sdkCtx().BlockTime().Unix()
	b, _ = f.keeper.Bounty.Get(f.ctx, bounty.Id)
	require.Equal(t, types.BountyStatus_BOUNTY_STATUS_ACTIVE, b.Status)
	require.Equal(t, now+1000, b.ExpiresAt)
	require.Zero(t, b.ModerationSuspendedAt)
	has, err = f.keeper.BountiesByExpiry.Has(f.ctx, collections.Join(now+1000, bounty.Id))
	require.NoError(t, err)
	require.True(t, has)

	// Hide again and let the hide stand: cancelled, 2.5% burned.
	hide()
	moves := f.trackBountyMoves(t)
	hr, err := f.keeper.HideRecord.Get(f.ctx, root.PostId)
	require.NoError(t, err)
	f.at(hr.HiddenAt + int64(types.DefaultHiddenExpiration) + 1)
	require.NoError(t, f.keeper.ExpireHiddenPosts(f.ctx, f.sdkCtx().BlockTime().Unix()))
	b, _ = f.keeper.Bounty.Get(f.ctx, bounty.Id)
	require.Equal(t, types.BountyStatus_BOUNTY_STATUS_CANCELLED, b.Status)
	require.Equal(t, "25000", moves.burned["total"])
	require.Equal(t, "975000", moves.paid[testCreator])
	has, err = f.keeper.ActiveBountyByThread.Has(f.ctx, root.PostId)
	require.NoError(t, err)
	require.False(t, has)
}

// FRM-11: a thread with an open bounty cannot be archived.
func TestFreezeRefusesThreadWithOpenBounty(t *testing.T) {
	f := initFixture(t)
	f.at(reviewNow)
	cat := f.createTestCategory(t, "archive")
	root := f.createTestPost(t, testCreator, 0, cat.CategoryId)
	f.createTestBounty(t, testCreator, root.PostId, "1000000")
	f.at(reviewNow + types.DefaultArchiveThreshold + 1)
	_, err := f.msgServer.FreezeThread(f.ctx, &types.MsgFreezeThread{Creator: testCreator2, RootId: root.PostId})
	require.ErrorIs(t, err, types.ErrBountyAlreadyExists)
}
