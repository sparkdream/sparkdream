package keeper_test

import (
	"testing"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/forum/types"

	"github.com/stretchr/testify/require"
)

func TestHidePost(t *testing.T) {
	f := initFixture(t)

	// Create a category and post
	cat := f.createTestCategory(t, "General")
	post := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	// Create a sentinel with sufficient bond
	f.createTestSentinel(t, testSentinel, "2000")

	tests := []struct {
		name        string
		msg         *types.MsgHidePost
		setup       func()
		expectError bool
		errContains string
	}{
		{
			name: "successful hide by sentinel",
			msg: &types.MsgHidePost{
				Creator:    testSentinel,
				PostId:     post.PostId,
				ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
				ReasonText: "This is spam",
			},
			expectError: false,
		},
		{
			name: "invalid creator address",
			msg: &types.MsgHidePost{
				Creator:    "invalid-address",
				PostId:     post.PostId,
				ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
				ReasonText: "Test",
			},
			expectError: true,
			errContains: "invalid creator address",
		},
		{
			name: "moderation paused",
			msg: &types.MsgHidePost{
				Creator:    testSentinel,
				PostId:     post.PostId,
				ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
				ReasonText: "Test",
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
			name: "post not found",
			msg: &types.MsgHidePost{
				Creator:    testSentinel,
				PostId:     9999,
				ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
				ReasonText: "Test",
			},
			expectError: true,
			errContains: "post not found",
		},
		{
			name: "invalid reason code",
			msg: &types.MsgHidePost{
				Creator:    testSentinel,
				PostId:     post.PostId,
				ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_UNSPECIFIED),
				ReasonText: "Test",
			},
			expectError: true,
			errContains: "invalid reason code",
		},
		{
			name: "not a sentinel",
			msg: &types.MsgHidePost{
				Creator:    testCreator2,
				PostId:     post.PostId,
				ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
				ReasonText: "Test",
			},
			expectError: true,
			errContains: "not a registered sentinel",
		},
		{
			name: "post already hidden",
			msg: &types.MsgHidePost{
				Creator:    testSentinel,
				PostId:     post.PostId,
				ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
				ReasonText: "Test",
			},
			setup: func() {
				p, _ := f.keeper.Post.Get(f.ctx, post.PostId)
				p.Status = types.PostStatus_POST_STATUS_HIDDEN
				_ = f.keeper.Post.Set(f.ctx, post.PostId, p)
			},
			expectError: true,
			errContains: "already hidden",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset params and post status
			_ = f.keeper.Params.Set(f.ctx, types.DefaultParams())
			p, _ := f.keeper.Post.Get(f.ctx, post.PostId)
			p.Status = types.PostStatus_POST_STATUS_ACTIVE
			_ = f.keeper.Post.Set(f.ctx, post.PostId, p)

			// Reset sentinel activity
			f.createTestSentinel(t, testSentinel, "2000")

			if tt.setup != nil {
				tt.setup()
			}

			resp, err := f.msgServer.HidePost(f.ctx, tt.msg)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)

				// Verify post was hidden
				hiddenPost, err := f.keeper.Post.Get(f.ctx, post.PostId)
				require.NoError(t, err)
				require.Equal(t, types.PostStatus_POST_STATUS_HIDDEN, hiddenPost.Status)
				require.Equal(t, tt.msg.Creator, hiddenPost.HiddenBy)

				// Verify hide record was created
				hideRecord, err := f.keeper.HideRecord.Get(f.ctx, post.PostId)
				require.NoError(t, err)
				require.Equal(t, tt.msg.Creator, hideRecord.Sentinel)
			}
		})
	}
}

func TestHidePostByGovAuthority(t *testing.T) {
	f := initFixture(t)

	// Create a category and post
	cat := f.createTestCategory(t, "General")
	post := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	// Get authority address
	authority, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())

	// Hide by gov authority
	resp, err := f.msgServer.HidePost(f.ctx, &types.MsgHidePost{
		Creator:    authority,
		PostId:     post.PostId,
		ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_POLICY_VIOLATION),
		ReasonText: "Policy violation",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify post was hidden
	hiddenPost, err := f.keeper.Post.Get(f.ctx, post.PostId)
	require.NoError(t, err)
	require.Equal(t, types.PostStatus_POST_STATUS_HIDDEN, hiddenPost.Status)

	// Verify no hide record was created (gov hides don't create hide records)
	_, err = f.keeper.HideRecord.Get(f.ctx, post.PostId)
	require.Error(t, err) // Should not find hide record
}

func TestHidePostSentinelBondCommitment(t *testing.T) {
	f := initFixture(t)

	// Create a category and post
	cat := f.createTestCategory(t, "General")
	post := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	// Create a sentinel with specific bond
	initialBond := "2000"
	f.createTestSentinel(t, testSentinel, initialBond)

	// Hide the post
	_, err := f.msgServer.HidePost(f.ctx, &types.MsgHidePost{
		Creator:    testSentinel,
		PostId:     post.PostId,
		ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
		ReasonText: "Spam",
	})
	require.NoError(t, err)

	// Verify sentinel activity was updated
	sentinel, err := f.keeper.SentinelActivity.Get(f.ctx, testSentinel)
	require.NoError(t, err)
	require.Equal(t, uint64(1), sentinel.TotalHides)
	require.Equal(t, uint64(1), sentinel.EpochHides)

	// Committed bond should be increased
	require.NotEqual(t, "0", sentinel.TotalCommittedBond)
}
