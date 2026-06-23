package keeper_test

import (
	"context"
	"testing"

	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"

	"github.com/stretchr/testify/require"
)

func TestMoveThread(t *testing.T) {
	f := initFixture(t)

	// Create categories
	cat1 := f.createTestCategory(t, "General")
	cat2 := f.createTestCategory(t, "Off-Topic")

	// Create a thread in cat1
	thread := f.createTestPost(t, testCreator, 0, cat1.CategoryId)

	// Create a sentinel (bond record in x/rep; forum counter-only locally).
	f.createTestSentinel(t, testSentinel, "2000000000")

	tests := []struct {
		name        string
		msg         *types.MsgMoveThread
		setup       func()
		expectError bool
		errContains string
	}{
		{
			name: "successful move by sentinel",
			msg: &types.MsgMoveThread{
				Creator:       testSentinel,
				RootId:        thread.PostId,
				NewCategoryId: cat2.CategoryId,
				Reason:        "Better fit for off-topic",
			},
			expectError: false,
		},
		{
			name: "invalid creator address",
			msg: &types.MsgMoveThread{
				Creator:       "invalid-address",
				RootId:        thread.PostId,
				NewCategoryId: cat2.CategoryId,
				Reason:        "Test",
			},
			expectError: true,
			errContains: "invalid creator address",
		},
		{
			name: "forum paused",
			msg: &types.MsgMoveThread{
				Creator:       testSentinel,
				RootId:        thread.PostId,
				NewCategoryId: cat2.CategoryId,
				Reason:        "Test",
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
			msg: &types.MsgMoveThread{
				Creator:       testSentinel,
				RootId:        9999,
				NewCategoryId: cat2.CategoryId,
				Reason:        "Test",
			},
			expectError: true,
			errContains: "not found",
		},
		{
			name: "new category not found",
			msg: &types.MsgMoveThread{
				Creator:       testSentinel,
				RootId:        thread.PostId,
				NewCategoryId: 9999,
				Reason:        "Test",
			},
			expectError: true,
			errContains: "category not found",
		},
		{
			name: "same category",
			msg: &types.MsgMoveThread{
				Creator:       testSentinel,
				RootId:        thread.PostId,
				NewCategoryId: cat1.CategoryId,
				Reason:        "Test",
			},
			expectError: true,
			errContains: "already in this category",
		},
		{
			name: "moderation paused for sentinel",
			msg: &types.MsgMoveThread{
				Creator:       testSentinel,
				RootId:        thread.PostId,
				NewCategoryId: cat2.CategoryId,
				Reason:        "Test",
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
			msg: &types.MsgMoveThread{
				Creator:       testCreator2,
				RootId:        thread.PostId,
				NewCategoryId: cat2.CategoryId,
				Reason:        "Test",
			},
			expectError: true,
			errContains: "not a registered sentinel",
		},
		{
			name: "sentinel missing reason",
			msg: &types.MsgMoveThread{
				Creator:       testSentinel,
				RootId:        thread.PostId,
				NewCategoryId: cat2.CategoryId,
				Reason:        "",
			},
			expectError: true,
			errContains: "move reason required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset params and thread state
			_ = f.keeper.Params.Set(f.ctx, types.DefaultParams())
			p, _ := f.keeper.Post.Get(f.ctx, thread.PostId)
			p.CategoryId = cat1.CategoryId
			p.ParentId = 0
			_ = f.keeper.Post.Set(f.ctx, thread.PostId, p)

			// Reset sentinel (local counters + rep bond record)
			_ = f.keeper.SentinelActivity.Set(f.ctx, testSentinel, types.SentinelActivity{Address: testSentinel})
			f.repKeeper.sentinels[testSentinel] = reptypes.BondedRole{
				Address:            testSentinel,
				CurrentBond:        "2000000000",
				TotalCommittedBond: "0",
				BondStatus:         reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL,
			}

			if tt.setup != nil {
				tt.setup()
			}

			resp, err := f.msgServer.MoveThread(f.ctx, tt.msg)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)

				// Verify thread was moved
				movedThread, err := f.keeper.Post.Get(f.ctx, thread.PostId)
				require.NoError(t, err)
				require.Equal(t, tt.msg.NewCategoryId, movedThread.CategoryId)

				// Verify move record was created for sentinel
				moveRecord, err := f.keeper.ThreadMoveRecord.Get(f.ctx, thread.PostId)
				require.NoError(t, err)
				require.Equal(t, tt.msg.Creator, moveRecord.Sentinel)
				require.Equal(t, cat1.CategoryId, moveRecord.OriginalCategoryId)
				require.Equal(t, tt.msg.NewCategoryId, moveRecord.NewCategoryId)
			}
		})
	}
}

func TestMoveThreadByGovAuthority(t *testing.T) {
	f := initFixture(t)

	// Create categories
	cat1 := f.createTestCategory(t, "General")
	cat2 := f.createTestCategory(t, "Archive")

	// Create a thread
	thread := f.createTestPost(t, testCreator, 0, cat1.CategoryId)

	// Get authority address
	authority, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())

	// Move by gov authority
	resp, err := f.msgServer.MoveThread(f.ctx, &types.MsgMoveThread{
		Creator:       authority,
		RootId:        thread.PostId,
		NewCategoryId: cat2.CategoryId,
		Reason:        "", // Optional for gov authority
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify thread was moved
	movedThread, err := f.keeper.Post.Get(f.ctx, thread.PostId)
	require.NoError(t, err)
	require.Equal(t, cat2.CategoryId, movedThread.CategoryId)

	// Verify no move record was created (gov moves don't create move records)
	_, err = f.keeper.ThreadMoveRecord.Get(f.ctx, thread.PostId)
	require.Error(t, err) // Should not find move record
}

func TestMoveThreadWithReservedTag(t *testing.T) {
	f := initFixture(t)

	// Create categories
	cat1 := f.createTestCategory(t, "General")
	cat2 := f.createTestCategory(t, "Off-Topic")

	// Create a thread with a reserved tag
	thread := f.createTestPost(t, testCreator, 0, cat1.CategoryId)
	p, _ := f.keeper.Post.Get(f.ctx, thread.PostId)
	p.Tags = []string{"governance"}
	_ = f.keeper.Post.Set(f.ctx, thread.PostId, p)

	// Create reserved tag in mock rep registry
	reservedTag := reptypes.ReservedTag{
		Name:          "governance",
		Authority:     testAuthority,
		MembersCanUse: true,
	}
	_ = f.repKeeper.SetReservedTag(f.ctx, reservedTag)

	// Create a sentinel (bond record in x/rep; forum counter-only locally).
	f.createTestSentinel(t, testSentinel, "2000000000")

	// Sentinel should not be able to move thread with reserved tag
	_, err := f.msgServer.MoveThread(f.ctx, &types.MsgMoveThread{
		Creator:       testSentinel,
		RootId:        thread.PostId,
		NewCategoryId: cat2.CategoryId,
		Reason:        "Test",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "reserved tag")
}

// TestMoveThread_UnbondingSentinelRejected: a sentinel unbonding its entire
// bond leaves zero staying bond — below the floor — so it cannot move threads.
func TestMoveThread_UnbondingSentinelRejected(t *testing.T) {
	f := initFixture(t)

	cat1 := f.createTestCategory(t, "Source")
	cat2 := f.createTestCategory(t, "Destination")
	thread := f.createTestPost(t, testCreator, 0, cat1.CategoryId)

	f.createTestSentinel(t, testSentinel, "3000000000")
	br := f.repKeeper.sentinels[testSentinel]
	br.PendingUnbondAmount = "3000000000"
	br.BondStatus = reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_UNBONDING
	f.repKeeper.sentinels[testSentinel] = br

	_, err := f.msgServer.MoveThread(f.ctx, &types.MsgMoveThread{
		Creator:       testSentinel,
		RootId:        thread.PostId,
		NewCategoryId: cat2.CategoryId,
		Reason:        "Test",
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrSentinelUnbonding)
}

// TestMoveThread_PartialUnbondingSentinelAllowed: a partial unbond leaving the
// staying bond above the floor keeps move authority. 3000 bonded, 100 queued →
// 2900 staying ≥ 500 floor.
func TestMoveThread_PartialUnbondingSentinelAllowed(t *testing.T) {
	f := initFixture(t)

	cat1 := f.createTestCategory(t, "Source")
	cat2 := f.createTestCategory(t, "Destination")
	thread := f.createTestPost(t, testCreator, 0, cat1.CategoryId)

	f.createTestSentinel(t, testSentinel, "3000000000")
	br := f.repKeeper.sentinels[testSentinel]
	br.PendingUnbondAmount = "100000000"
	br.BondStatus = reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_UNBONDING
	f.repKeeper.sentinels[testSentinel] = br

	_, err := f.msgServer.MoveThread(f.ctx, &types.MsgMoveThread{
		Creator:       testSentinel,
		RootId:        thread.PostId,
		NewCategoryId: cat2.CategoryId,
		Reason:        "Test",
	})
	require.NoError(t, err)

	moved, err := f.keeper.Post.Get(f.ctx, thread.PostId)
	require.NoError(t, err)
	require.Equal(t, cat2.CategoryId, moved.CategoryId)
}

// TestMoveThread_AuthorityDisambiguation covers the sentinel-vs-council choice
// for MsgMoveThread when one account holds both roles. Move eligibility is
// bonded NORMAL/RECOVERY + no reserved tag (no extra bond/tier floor), so a
// plain bonded sentinel that is also council defaults to the sentinel path.
// See docs/HANDOFF_HIDE_AUTHORITY_DISAMBIGUATION.md.
func TestMoveThread_AuthorityDisambiguation(t *testing.T) {
	setup := func(t *testing.T) (*fixture, uint64, uint64) {
		t.Helper()
		f := initFixture(t)
		cat1 := f.createTestCategory(t, "General")
		cat2 := f.createTestCategory(t, "Off-Topic")
		thread := f.createTestPost(t, testCreator, 0, cat1.CategoryId)
		f.createTestSentinel(t, testSentinel, "2000000000")
		authority, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())
		f.commonsKeeper.IsCouncilAuthorizedFn = func(_ context.Context, addr string, _ string, _ string) bool {
			return addr == authority || addr == testSentinel
		}
		return f, thread.PostId, cat2.CategoryId
	}

	// AUTO by a sentinel-and-council account takes the sentinel path: a
	// ThreadMoveRecord with Sentinel == creator.
	t.Run("auto prefers sentinel path", func(t *testing.T) {
		f, rootID, destCat := setup(t)
		_, err := f.msgServer.MoveThread(f.ctx, &types.MsgMoveThread{
			Creator:       testSentinel,
			RootId:        rootID,
			NewCategoryId: destCat,
			Reason:        "off-topic",
			Authority:     types.ModerationAuthority_MODERATION_AUTHORITY_AUTO,
		})
		require.NoError(t, err)
		rec, err := f.keeper.ThreadMoveRecord.Get(f.ctx, rootID)
		require.NoError(t, err, "AUTO must take the sentinel path and write a move record")
		require.Equal(t, testSentinel, rec.Sentinel)
	})

	// Explicit COUNCIL by the same account is the deliberate gov move: no record.
	t.Run("explicit council is opt-in gov move", func(t *testing.T) {
		f, rootID, destCat := setup(t)
		_, err := f.msgServer.MoveThread(f.ctx, &types.MsgMoveThread{
			Creator:       testSentinel,
			RootId:        rootID,
			NewCategoryId: destCat,
			Reason:        "",
			Authority:     types.ModerationAuthority_MODERATION_AUTHORITY_COUNCIL,
		})
		require.NoError(t, err)
		_, err = f.keeper.ThreadMoveRecord.Get(f.ctx, rootID)
		require.Error(t, err, "explicit COUNCIL move must not write a sentinel move record")
	})

	// AUTO by a council account moving a RESERVED-tag thread is not
	// sentinel-eligible (only council may move reserved-tag threads), so it
	// falls through to the council path instead of erroring.
	t.Run("auto falls through to council for reserved-tag thread", func(t *testing.T) {
		f, rootID, destCat := setup(t)
		p, _ := f.keeper.Post.Get(f.ctx, rootID)
		p.Tags = []string{"governance"}
		_ = f.keeper.Post.Set(f.ctx, rootID, p)
		_ = f.repKeeper.SetReservedTag(f.ctx, reptypes.ReservedTag{Name: "governance", Authority: testAuthority, MembersCanUse: true})
		_, err := f.msgServer.MoveThread(f.ctx, &types.MsgMoveThread{
			Creator:       testSentinel,
			RootId:        rootID,
			NewCategoryId: destCat,
			Reason:        "",
			Authority:     types.ModerationAuthority_MODERATION_AUTHORITY_AUTO,
		})
		require.NoError(t, err)
		_, err = f.keeper.ThreadMoveRecord.Get(f.ctx, rootID)
		require.Error(t, err, "council moving a reserved-tag thread writes no sentinel record")
	})

	// Explicit SENTINEL moving a reserved-tag thread hard-errors with the
	// reserved-tag error — no silent downgrade to council.
	t.Run("explicit sentinel on reserved-tag thread hard-errors", func(t *testing.T) {
		f, rootID, destCat := setup(t)
		p, _ := f.keeper.Post.Get(f.ctx, rootID)
		p.Tags = []string{"governance"}
		_ = f.keeper.Post.Set(f.ctx, rootID, p)
		_ = f.repKeeper.SetReservedTag(f.ctx, reptypes.ReservedTag{Name: "governance", Authority: testAuthority, MembersCanUse: true})
		_, err := f.msgServer.MoveThread(f.ctx, &types.MsgMoveThread{
			Creator:       testSentinel,
			RootId:        rootID,
			NewCategoryId: destCat,
			Reason:        "x",
			Authority:     types.ModerationAuthority_MODERATION_AUTHORITY_SENTINEL,
		})
		require.ErrorIs(t, err, types.ErrCannotMoveReservedTag)
	})
}
