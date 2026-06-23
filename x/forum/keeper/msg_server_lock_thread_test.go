package keeper_test

import (
	"context"
	"testing"

	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"

	"github.com/stretchr/testify/require"
)

func TestLockThread(t *testing.T) {
	f := initFixture(t)

	// Create a category and thread (root post)
	cat := f.createTestCategory(t, "General")
	thread := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	// Create a sentinel with sufficient bond (bond record in x/rep).
	f.createTestSentinel(t, testSentinel, "3000000000")

	tests := []struct {
		name        string
		msg         *types.MsgLockThread
		setup       func()
		expectError bool
		errContains string
	}{
		{
			name: "successful lock by sentinel",
			msg: &types.MsgLockThread{
				Creator: testSentinel,
				RootId:  thread.PostId,
				Reason:  "Off-topic discussion",
			},
			expectError: false,
		},
		{
			name: "invalid creator address",
			msg: &types.MsgLockThread{
				Creator: "invalid-address",
				RootId:  thread.PostId,
				Reason:  "Test",
			},
			expectError: true,
			errContains: "invalid creator address",
		},
		{
			name: "forum paused",
			msg: &types.MsgLockThread{
				Creator: testSentinel,
				RootId:  thread.PostId,
				Reason:  "Test",
			},
			setup: func() {
				params := types.DefaultParams()
				params.ForumPaused = true
				_ = f.keeper.Params.Set(f.ctx, params)
			},
			expectError: true,
			errContains: "forum is paused",
		},
		{
			name: "thread not found",
			msg: &types.MsgLockThread{
				Creator: testSentinel,
				RootId:  9999,
				Reason:  "Test",
			},
			expectError: true,
			errContains: "post not found",
		},
		{
			name: "only allowed on root posts",
			msg: &types.MsgLockThread{
				Creator: testSentinel,
				RootId:  thread.PostId,
				Reason:  "Test",
			},
			setup: func() {
				// Create a reply and try to lock it
				reply := f.createTestPost(t, testCreator2, thread.PostId, cat.CategoryId)
				// We need to override the msg in this case, so we'll handle it differently
				_ = reply
			},
			expectError: true,
			errContains: "only allowed on root posts",
		},
		{
			name: "thread is already locked",
			msg: &types.MsgLockThread{
				Creator: testSentinel,
				RootId:  thread.PostId,
				Reason:  "Test",
			},
			setup: func() {
				p, _ := f.keeper.Post.Get(f.ctx, thread.PostId)
				p.Locked = true
				_ = f.keeper.Post.Set(f.ctx, thread.PostId, p)
			},
			expectError: true,
			errContains: "thread is already locked",
		},
		{
			name: "moderation paused for sentinel",
			msg: &types.MsgLockThread{
				Creator: testSentinel,
				RootId:  thread.PostId,
				Reason:  "Test",
			},
			setup: func() {
				params := types.DefaultParams()
				params.ModerationPaused = true
				_ = f.keeper.Params.Set(f.ctx, params)
			},
			expectError: true,
			errContains: "moderation is paused",
		},
		{
			name: "not a sentinel",
			msg: &types.MsgLockThread{
				Creator: testCreator2,
				RootId:  thread.PostId,
				Reason:  "Test",
			},
			expectError: true,
			errContains: "not a registered sentinel",
		},
		{
			name: "sentinel missing reason",
			msg: &types.MsgLockThread{
				Creator: testSentinel,
				RootId:  thread.PostId,
				Reason:  "",
			},
			expectError: true,
			errContains: "lock reason required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset params and thread state
			_ = f.keeper.Params.Set(f.ctx, types.DefaultParams())
			p, _ := f.keeper.Post.Get(f.ctx, thread.PostId)
			p.Locked = false
			p.ParentId = 0 // Ensure it's a root post
			_ = f.keeper.Post.Set(f.ctx, thread.PostId, p)

			// Reset sentinel (forum-local counters + rep bond record)
			_ = f.keeper.SentinelActivity.Set(f.ctx, testSentinel, types.SentinelActivity{Address: testSentinel})
			f.repKeeper.sentinels[testSentinel] = reptypes.BondedRole{
				Address:            testSentinel,
				CurrentBond:        "3000000000",
				TotalCommittedBond: "0",
				BondStatus:         reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL,
			}

			// Skip the "only allowed on root posts" test as it requires special handling
			if tt.name == "only allowed on root posts" {
				reply := f.createTestPost(t, testCreator2, thread.PostId, cat.CategoryId)
				_, err := f.msgServer.LockThread(f.ctx, &types.MsgLockThread{
					Creator: testSentinel,
					RootId:  reply.PostId,
					Reason:  "Test",
				})
				require.Error(t, err)
				require.Contains(t, err.Error(), "only allowed on root posts")
				return
			}

			if tt.setup != nil {
				tt.setup()
			}

			resp, err := f.msgServer.LockThread(f.ctx, tt.msg)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)

				// Verify thread was locked
				lockedThread, err := f.keeper.Post.Get(f.ctx, thread.PostId)
				require.NoError(t, err)
				require.True(t, lockedThread.Locked)
				require.Equal(t, tt.msg.Creator, lockedThread.LockedBy)
				require.Equal(t, tt.msg.Reason, lockedThread.LockReason)

				// Verify lock record was created for sentinel
				lockRecord, err := f.keeper.ThreadLockRecord.Get(f.ctx, thread.PostId)
				require.NoError(t, err)
				require.Equal(t, tt.msg.Creator, lockRecord.Sentinel)
			}
		})
	}
}

func TestLockThreadByGovAuthority(t *testing.T) {
	f := initFixture(t)

	// Create a category and thread
	cat := f.createTestCategory(t, "General")
	thread := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	// Get authority address
	authority, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())

	// Lock by gov authority (no reason required)
	resp, err := f.msgServer.LockThread(f.ctx, &types.MsgLockThread{
		Creator: authority,
		RootId:  thread.PostId,
		Reason:  "", // Optional for gov authority
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify thread was locked
	lockedThread, err := f.keeper.Post.Get(f.ctx, thread.PostId)
	require.NoError(t, err)
	require.True(t, lockedThread.Locked)

	// Verify no lock record was created (gov locks don't create lock records)
	_, err = f.keeper.ThreadLockRecord.Get(f.ctx, thread.PostId)
	require.Error(t, err) // Should not find lock record
}

// TestLockThread_UnbondingSentinelRejected: a sentinel unbonding its entire
// bond leaves zero staying bond — below the floor — so it cannot lock.
func TestLockThread_UnbondingSentinelRejected(t *testing.T) {
	f := initFixture(t)

	cat := f.createTestCategory(t, "General")
	thread := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	f.createTestSentinel(t, testSentinel, "3000000000")
	br := f.repKeeper.sentinels[testSentinel]
	br.PendingUnbondAmount = "3000000000"
	br.BondStatus = reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_UNBONDING
	f.repKeeper.sentinels[testSentinel] = br

	_, err := f.msgServer.LockThread(f.ctx, &types.MsgLockThread{
		Creator: testSentinel,
		RootId:  thread.PostId,
		Reason:  "Test",
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrSentinelUnbonding)
}

// TestLockThread_PartialUnbondingSentinelAllowed: a partial unbond that leaves
// the staying bond above both the sentinel floor and the higher lock floor lets
// the sentinel keep locking. 3000 bonded, 100 queued → 2900 staying.
func TestLockThread_PartialUnbondingSentinelAllowed(t *testing.T) {
	f := initFixture(t)

	cat := f.createTestCategory(t, "General")
	thread := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	f.createTestSentinel(t, testSentinel, "3000000000")
	br := f.repKeeper.sentinels[testSentinel]
	br.PendingUnbondAmount = "100000000" // 2900 stays, above lock floor
	br.BondStatus = reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_UNBONDING
	f.repKeeper.sentinels[testSentinel] = br

	_, err := f.msgServer.LockThread(f.ctx, &types.MsgLockThread{
		Creator: testSentinel,
		RootId:  thread.PostId,
		Reason:  "Test",
	})
	require.NoError(t, err)

	locked, err := f.keeper.Post.Get(f.ctx, thread.PostId)
	require.NoError(t, err)
	require.True(t, locked.Locked)
}

// TestLockThread_ParamDrivenBondFloor proves the lock bond floor is read from
// params (lock_bond_multiplier × min_sentinel_bond), not a hardcoded constant:
// raising the multiplier makes an otherwise-eligible sentinel ineligible, and
// lowering it restores eligibility.
func TestLockThread_ParamDrivenBondFloor(t *testing.T) {
	f := initFixture(t)
	cat := f.createTestCategory(t, "General")
	thread := f.createTestPost(t, testCreator, 0, cat.CategoryId)
	f.createTestSentinel(t, testSentinel, "3000000000") // 3000 DREAM

	// Raise the multiplier so the derived floor (10 × 500 = 5000 DREAM) exceeds
	// the sentinel's 3000 DREAM bond.
	params := types.DefaultParams()
	params.LockBondMultiplier = 10
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	_, err := f.msgServer.LockThread(f.ctx, &types.MsgLockThread{
		Creator:   testSentinel,
		RootId:    thread.PostId,
		Reason:    "x",
		Authority: types.ModerationAuthority_MODERATION_AUTHORITY_SENTINEL,
	})
	require.ErrorIs(t, err, types.ErrInsufficientLockBond)

	// Restore the default multiplier (4 × 500 = 2000 DREAM <= 3000) → succeeds.
	params.LockBondMultiplier = 4
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	_, err = f.msgServer.LockThread(f.ctx, &types.MsgLockThread{
		Creator: testSentinel,
		RootId:  thread.PostId,
		Reason:  "x",
	})
	require.NoError(t, err)
}

// TestLockThread_AuthorityDisambiguation covers the sentinel-vs-council choice
// for MsgLockThread when one account holds both roles. AUTO must prefer the
// accountable sentinel path (writes a ThreadLockRecord) over the council path
// (no record, unlockable only by the council). See
// docs/HANDOFF_HIDE_AUTHORITY_DISAMBIGUATION.md.
func TestLockThread_AuthorityDisambiguation(t *testing.T) {
	// Lock-eligible sentinel that is ALSO council.
	setup := func(t *testing.T, bond string) (*fixture, uint64) {
		t.Helper()
		f := initFixture(t)
		cat := f.createTestCategory(t, "General")
		thread := f.createTestPost(t, testCreator, 0, cat.CategoryId)
		f.createTestSentinel(t, testSentinel, bond)
		authority, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())
		f.commonsKeeper.IsCouncilAuthorizedFn = func(_ context.Context, addr string, _ string, _ string) bool {
			return addr == authority || addr == testSentinel
		}
		return f, thread.PostId
	}

	// AUTO by a lock-eligible (3000 DREAM ≥ 2000) sentinel-and-council account
	// takes the sentinel path: a ThreadLockRecord with Sentinel == creator.
	t.Run("auto prefers sentinel path when lock-eligible", func(t *testing.T) {
		f, rootID := setup(t, "3000000000")
		_, err := f.msgServer.LockThread(f.ctx, &types.MsgLockThread{
			Creator:   testSentinel,
			RootId:    rootID,
			Reason:    "off-topic",
			Authority: types.ModerationAuthority_MODERATION_AUTHORITY_AUTO,
		})
		require.NoError(t, err)
		rec, err := f.keeper.ThreadLockRecord.Get(f.ctx, rootID)
		require.NoError(t, err, "AUTO must take the sentinel path and write a lock record")
		require.Equal(t, testSentinel, rec.Sentinel)
	})

	// Explicit COUNCIL by the same account is the deliberate gov lock: no record.
	t.Run("explicit council is opt-in gov lock", func(t *testing.T) {
		f, rootID := setup(t, "3000000000")
		_, err := f.msgServer.LockThread(f.ctx, &types.MsgLockThread{
			Creator:   testSentinel,
			RootId:    rootID,
			Reason:    "",
			Authority: types.ModerationAuthority_MODERATION_AUTHORITY_COUNCIL,
		})
		require.NoError(t, err)
		_, err = f.keeper.ThreadLockRecord.Get(f.ctx, rootID)
		require.Error(t, err, "explicit COUNCIL lock must not write a sentinel lock record")
	})

	// AUTO by a council account whose bond is BELOW the 2x lock floor (1000 <
	// 2000 DREAM) is NOT lock-eligible, so it falls through to the council path.
	t.Run("auto falls through to council when bond below lock floor", func(t *testing.T) {
		f, rootID := setup(t, "1000000000") // 1000 DREAM < 2000 lock floor
		_, err := f.msgServer.LockThread(f.ctx, &types.MsgLockThread{
			Creator:   testSentinel,
			RootId:    rootID,
			Reason:    "",
			Authority: types.ModerationAuthority_MODERATION_AUTHORITY_AUTO,
		})
		require.NoError(t, err)
		_, err = f.keeper.ThreadLockRecord.Get(f.ctx, rootID)
		require.Error(t, err, "sub-floor bond with council falls through to a gov lock (no record)")
	})

	// Explicit SENTINEL by a sub-floor-bond account hard-errors with the
	// specific lock-bond error — no silent downgrade to council.
	t.Run("explicit sentinel below lock floor hard-errors", func(t *testing.T) {
		f, rootID := setup(t, "1000000000")
		_, err := f.msgServer.LockThread(f.ctx, &types.MsgLockThread{
			Creator:   testSentinel,
			RootId:    rootID,
			Reason:    "x",
			Authority: types.ModerationAuthority_MODERATION_AUTHORITY_SENTINEL,
		})
		require.ErrorIs(t, err, types.ErrInsufficientLockBond)
	})

	// Explicit COUNCIL by a non-council account → ErrNotAuthorized.
	t.Run("explicit council by non-council fails", func(t *testing.T) {
		f := initFixture(t)
		cat := f.createTestCategory(t, "General")
		thread := f.createTestPost(t, testCreator, 0, cat.CategoryId)
		f.createTestSentinel(t, testSentinel, "3000000000") // sentinel, not council
		_, err := f.msgServer.LockThread(f.ctx, &types.MsgLockThread{
			Creator:   testSentinel,
			RootId:    thread.PostId,
			Reason:    "x",
			Authority: types.ModerationAuthority_MODERATION_AUTHORITY_COUNCIL,
		})
		require.ErrorIs(t, err, types.ErrNotAuthorized)
	})
}
