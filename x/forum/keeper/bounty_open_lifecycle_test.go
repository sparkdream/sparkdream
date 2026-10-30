package keeper_test

import (
	"context"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
)

// createBounty creates a 100-SPARK bounty on thread by testCreator.
func (f *fixture) createBounty(t *testing.T, threadID uint64) uint64 {
	t.Helper()
	_, err := f.msgServer.CreateBounty(f.ctx, &types.MsgCreateBounty{Creator: testCreator, ThreadId: threadID, Amount: "100000000"})
	require.NoError(t, err)
	id, err := f.keeper.ActiveBountyByThread.Get(f.ctx, threadID)
	require.NoError(t, err)
	return id
}

// FRM-25: a suspended bounty is still the thread's open bounty. A second
// bounty cannot replace it (that dropped its only index entry and stranded
// the escrow), and a genesis round-trip keeps its thread index.
func TestSuspendedBountyStaysTheThreadsOpenBounty(t *testing.T) {
	f := initFixture(t)
	cat := f.createTestCategory(t, "General")
	thread := f.createTestPost(t, testCreator, 0, cat.CategoryId)
	id := f.createBounty(t, thread.PostId)

	bounty, err := f.keeper.Bounty.Get(f.ctx, id)
	require.NoError(t, err)
	bounty.Status = types.BountyStatus_BOUNTY_STATUS_MODERATION_PENDING
	require.NoError(t, f.keeper.Bounty.Set(f.ctx, id, bounty))

	_, err = f.msgServer.CreateBounty(f.ctx, &types.MsgCreateBounty{Creator: testCreator, ThreadId: thread.PostId, Amount: "100000000"})
	require.ErrorIs(t, err, types.ErrBountyAlreadyExists)
	indexed, err := f.keeper.ActiveBountyByThread.Get(f.ctx, thread.PostId)
	require.NoError(t, err)
	require.Equal(t, id, indexed)

	gs, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NoError(t, gs.Validate())
	g := initFixture(t)
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *gs))
	indexed, err = g.keeper.ActiveBountyByThread.Get(g.ctx, thread.PostId)
	require.NoError(t, err, "a suspended bounty keeps its thread index across genesis")
	require.Equal(t, id, indexed)
}

// FRM-29: a bounty needs an active member and an active, unlocked root;
// a refused attempt escrows nothing.
func TestCreateBountyGates(t *testing.T) {
	f := initFixture(t)
	cat := f.createTestCategory(t, "General")
	var escrows int
	f.bankKeeper.SendCoinsFromAccountToModuleFn = func(context.Context, sdk.AccAddress, string, sdk.Coins) error {
		escrows++
		return nil
	}
	create := func(threadID uint64) error {
		_, err := f.msgServer.CreateBounty(f.ctx, &types.MsgCreateBounty{Creator: testCreator, ThreadId: threadID, Amount: "100000000"})
		return err
	}
	for name, mutate := range map[string]func(*types.Post){
		"hidden":   func(p *types.Post) { p.Status = types.PostStatus_POST_STATUS_HIDDEN },
		"deleted":  func(p *types.Post) { p.Status = types.PostStatus_POST_STATUS_DELETED },
		"archived": func(p *types.Post) { p.Status = types.PostStatus_POST_STATUS_ARCHIVED },
		"locked":   func(p *types.Post) { p.Locked = true },
	} {
		thread := f.createTestPost(t, testCreator, 0, cat.CategoryId)
		mutate(&thread)
		require.NoError(t, f.keeper.Post.Set(f.ctx, thread.PostId, thread))
		require.ErrorIs(t, create(thread.PostId), types.ErrBountyRootIneligible, name)
	}

	thread := f.createTestPost(t, testCreator, 0, cat.CategoryId)
	f.repKeeper.IsActiveMemberFn = func(context.Context, sdk.AccAddress) bool { return false }
	require.ErrorIs(t, create(thread.PostId), types.ErrNotMember)
	require.Zero(t, escrows, "refused attempts escrow nothing")

	f.repKeeper.IsActiveMemberFn = nil
	require.NoError(t, create(thread.PostId))
	require.Equal(t, 1, escrows)
}

// FRM-34: when the root's moderation stands, assigned awards are paid like
// at expiry instead of the escrow going back to the creator.
func TestModerationStoodPaysAssignedAwards(t *testing.T) {
	f := initFixture(t)
	cat := f.createTestCategory(t, "General")
	thread := f.createTestPost(t, testCreator, 0, cat.CategoryId)
	reply := f.createTestPost(t, testCreator2, thread.PostId, cat.CategoryId)
	id := f.createBounty(t, thread.PostId)

	bounty, err := f.keeper.Bounty.Get(f.ctx, id)
	require.NoError(t, err)
	bounty.Awards = []*types.BountyAward{{PostId: reply.PostId, Recipient: testCreator2}}
	require.NoError(t, f.keeper.Bounty.Set(f.ctx, id, bounty))

	paid := map[string]sdk.Coins{}
	f.bankKeeper.SendCoinsFromModuleToAccountFn = func(_ context.Context, _ string, to sdk.AccAddress, amt sdk.Coins) error {
		paid[to.String()] = paid[to.String()].Add(amt...)
		return nil
	}

	// A lock that stood: the root is locked and its lock record is settled.
	thread.Locked = true
	require.NoError(t, f.keeper.Post.Set(f.ctx, thread.PostId, thread))
	require.NoError(t, f.keeper.ReconcileThreadBountyForTest(f.ctx, thread.PostId))

	bounty, err = f.keeper.Bounty.Get(f.ctx, id)
	require.NoError(t, err)
	require.Equal(t, types.BountyStatus_BOUNTY_STATUS_AWARDED, bounty.Status)
	require.Equal(t, "100000000", paid[testCreator2].AmountOf("uspark").String(), "the answerer is paid")
	require.True(t, paid[testCreator].IsZero(), "the creator gets nothing back")
	_, err = f.keeper.ActiveBountyByThread.Get(f.ctx, thread.PostId)
	require.Error(t, err)
}
