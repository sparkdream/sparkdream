package keeper_test

import (
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

	// Create a sentinel
	sentinel := types.SentinelActivity{
		Address:            testSentinel,
		CurrentBond:        "2000",
		TotalCommittedBond: "0",
		BondStatus:         types.SentinelBondStatus_SENTINEL_BOND_STATUS_NORMAL,
	}
	_ = f.keeper.SentinelActivity.Set(f.ctx, testSentinel, sentinel)

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

			// Reset sentinel
			sentinel := types.SentinelActivity{
				Address:            testSentinel,
				CurrentBond:        "2000",
				TotalCommittedBond: "0",
				BondStatus:         types.SentinelBondStatus_SENTINEL_BOND_STATUS_NORMAL,
			}
			_ = f.keeper.SentinelActivity.Set(f.ctx, testSentinel, sentinel)

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

	// Create a sentinel
	sentinel := types.SentinelActivity{
		Address:            testSentinel,
		CurrentBond:        "2000",
		TotalCommittedBond: "0",
		BondStatus:         types.SentinelBondStatus_SENTINEL_BOND_STATUS_NORMAL,
	}
	_ = f.keeper.SentinelActivity.Set(f.ctx, testSentinel, sentinel)

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
