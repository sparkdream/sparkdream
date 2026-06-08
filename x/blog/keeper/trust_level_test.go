package keeper_test

import (
	"context"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
	reptypes "sparkdream/x/rep/types"
)

func TestMeetsReplyTrustLevel(t *testing.T) {
	// meetsReplyTrustLevel is unexported, so we test it indirectly through
	// CreateReply. A post's MinReplyTrustLevel gates who can reply:
	//   -1  => open to all (no membership check)
	//    0  => requires isActiveMember to return true
	//   1-4 => requires active member with GetTrustLevel >= minLevel
	//
	// The error returned distinguishes the failure mode:
	//   ErrNotMember              => caller isn't an active member at all
	//   ErrInsufficientTrustLevel => caller is a member but trust too low

	tests := []struct {
		name               string
		minReplyTrustLevel int32
		isActiveMember     bool
		callerTrustLevel   reptypes.TrustLevel // only consulted when isActiveMember = true
		expectReplyAllowed bool
		expectedErr        error
	}{
		{
			name:               "minLevel=-1 allows anyone regardless of membership",
			minReplyTrustLevel: -1,
			isActiveMember:     false,
			expectReplyAllowed: true,
		},
		{
			name:               "minLevel=-1 with active member also succeeds",
			minReplyTrustLevel: -1,
			isActiveMember:     true,
			callerTrustLevel:   reptypes.TrustLevel_TRUST_LEVEL_CORE,
			expectReplyAllowed: true,
		},
		{
			name:               "minLevel=0 with active member succeeds",
			minReplyTrustLevel: 0,
			isActiveMember:     true,
			callerTrustLevel:   reptypes.TrustLevel_TRUST_LEVEL_CORE,
			expectReplyAllowed: true,
		},
		{
			name:               "minLevel=0 with non-active member yields ErrNotMember",
			minReplyTrustLevel: 0,
			isActiveMember:     false,
			expectReplyAllowed: false,
			expectedErr:        types.ErrNotMember,
		},
		{
			name:               "minLevel=1 with active member succeeds",
			minReplyTrustLevel: 1,
			isActiveMember:     true,
			callerTrustLevel:   reptypes.TrustLevel_TRUST_LEVEL_CORE,
			expectReplyAllowed: true,
		},
		{
			name:               "minLevel=1 with non-active member yields ErrNotMember (not insufficient trust — the actionable barrier is membership)",
			minReplyTrustLevel: 1,
			isActiveMember:     false,
			expectReplyAllowed: false,
			expectedErr:        types.ErrNotMember,
		},
		{
			name:               "minLevel=2 with active member but trust below bar yields ErrInsufficientTrustLevel",
			minReplyTrustLevel: 2,
			isActiveMember:     true,
			callerTrustLevel:   reptypes.TrustLevel_TRUST_LEVEL_PROVISIONAL,
			expectReplyAllowed: false,
			expectedErr:        types.ErrInsufficientTrustLevel,
		},
		{
			name:               "minLevel=4 with active member succeeds",
			minReplyTrustLevel: 4,
			isActiveMember:     true,
			callerTrustLevel:   reptypes.TrustLevel_TRUST_LEVEL_CORE,
			expectReplyAllowed: true,
		},
		{
			name:               "minLevel=4 with non-active member yields ErrNotMember",
			minReplyTrustLevel: 4,
			isActiveMember:     false,
			expectReplyAllowed: false,
			expectedErr:        types.ErrNotMember,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := initFixture(t)
			msgServer := keeper.NewMsgServerImpl(f.keeper)

			params, err := f.keeper.Params.Get(f.ctx)
			require.NoError(t, err)
			params.MaxPostsPerDay = 100
			params.MaxRepliesPerDay = 100
			params.CostPerByteExempt = true
			require.NoError(t, f.keeper.Params.Set(f.ctx, params))

			sdkCtx := sdk.UnwrapSDKContext(f.ctx)
			f.ctx = sdkCtx.WithBlockTime(time.Unix(1_000_000, 0))

			// The post author and the replier are DISTINCT addresses so this
			// test exercises the trust gate for a non-author. The thread-author
			// exemption (a post's own author may always reply) is covered
			// separately in TestThreadAuthorReplyExemption.
			author := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"
			replier := "sprkdrm1g5ad4qmzqpfkfzgktx6za005qt2t0v56jy529y"

			// Mocks are address-aware: the author is always an active CORE
			// member (so they can create the post and aren't the subject of
			// the gate); the replier's membership/trust comes from the case.
			f.repKeeper.IsActiveMemberFn = func(_ context.Context, addr sdk.AccAddress) bool {
				if addr.String() == author {
					return true
				}
				return tt.isActiveMember
			}
			f.repKeeper.GetTrustLevelFn = func(_ context.Context, addr sdk.AccAddress) (reptypes.TrustLevel, error) {
				if addr.String() == author {
					return reptypes.TrustLevel_TRUST_LEVEL_CORE, nil
				}
				return tt.callerTrustLevel, nil
			}

			// Create a post with the desired MinReplyTrustLevel.
			postResp, err := msgServer.CreatePost(f.ctx, &types.MsgCreatePost{
				Creator:            author,
				Title:              "Trust Level Post",
				Body:               "Body for trust level test",
				MinReplyTrustLevel: tt.minReplyTrustLevel,
			})
			require.NoError(t, err)

			_, err = msgServer.CreateReply(f.ctx, &types.MsgCreateReply{
				Creator: replier,
				PostId:  postResp.Id,
				Body:    "Attempting to reply",
			})

			if tt.expectReplyAllowed {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.ErrorIs(t, err, tt.expectedErr)
			}
		})
	}
}

// TestThreadAuthorReplyExemption verifies the thread-author exemption: the
// author of a post may always reply within their own thread — including as a
// non-member and including replying to nested replies — even when the post's
// MinReplyTrustLevel would otherwise lock them out. The exemption bypasses
// ONLY the trust gate; replies_enabled, moderation state, and the ephemeral
// TTL for non-members still apply (the latter two are covered below).
func TestThreadAuthorReplyExemption(t *testing.T) {
	author := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"
	stranger := "sprkdrm1g5ad4qmzqpfkfzgktx6za005qt2t0v56jy529y"

	tests := []struct {
		name               string
		minReplyTrustLevel int32
	}{
		{name: "member-only post (minLevel=0)", minReplyTrustLevel: 0},
		{name: "established+ post (minLevel=2)", minReplyTrustLevel: 2},
		{name: "core-only post (minLevel=4)", minReplyTrustLevel: 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := initFixture(t)
			msgServer := keeper.NewMsgServerImpl(f.keeper)

			params, err := f.keeper.Params.Get(f.ctx)
			require.NoError(t, err)
			params.MaxPostsPerDay = 100
			params.MaxRepliesPerDay = 100
			params.CostPerByteExempt = true
			require.NoError(t, f.keeper.Params.Set(f.ctx, params))

			sdkCtx := sdk.UnwrapSDKContext(f.ctx)
			f.ctx = sdkCtx.WithBlockTime(time.Unix(1_000_000, 0))

			// The author is a NON-MEMBER. They can still create the post
			// (non-members get ephemeral posts), and the exemption must let
			// them participate in their own thread regardless of trust gate.
			f.repKeeper.IsActiveMemberFn = func(_ context.Context, _ sdk.AccAddress) bool { return false }
			f.repKeeper.GetTrustLevelFn = func(_ context.Context, _ sdk.AccAddress) (reptypes.TrustLevel, error) {
				return reptypes.TrustLevel_TRUST_LEVEL_NEW, nil
			}

			postResp, err := msgServer.CreatePost(f.ctx, &types.MsgCreatePost{
				Creator:            author,
				Title:              "Author Exemption Post",
				Body:               "Body",
				MinReplyTrustLevel: tt.minReplyTrustLevel,
			})
			require.NoError(t, err)

			// Sanity: a non-author non-member is still blocked by the gate.
			_, err = msgServer.CreateReply(f.ctx, &types.MsgCreateReply{
				Creator: stranger,
				PostId:  postResp.Id,
				Body:    "Stranger should be blocked",
			})
			require.Error(t, err, "non-author non-member must not bypass the trust gate")

			// Author replies to their own root post — must succeed via the
			// exemption. Consumes reply ID 0.
			_, err = msgServer.CreateReply(f.ctx, &types.MsgCreateReply{
				Creator: author,
				PostId:  postResp.Id,
				Body:    "Author replying to own post",
			})
			require.NoError(t, err, "thread author must be able to reply on own post")

			// Author replies again to get a parentable reply (ID >= 1).
			topResp, err := msgServer.CreateReply(f.ctx, &types.MsgCreateReply{
				Creator: author,
				PostId:  postResp.Id,
				Body:    "Author top-level reply",
			})
			require.NoError(t, err)
			require.NotZero(t, topResp.Id)

			// Author replies to a reply (deep in their own thread) — the
			// "reply to replies on their created posts" case. Must succeed.
			nestedResp, err := msgServer.CreateReply(f.ctx, &types.MsgCreateReply{
				Creator:       author,
				PostId:        postResp.Id,
				ParentReplyId: topResp.Id,
				Body:          "Author replying to a reply",
			})
			require.NoError(t, err, "thread author must be able to reply to replies in own thread")

			// The exemption bypasses only the trust gate: a non-member
			// author's self-reply is still ephemeral (spam guardrail intact).
			nested, found := f.keeper.GetReply(f.ctx, nestedResp.Id)
			require.True(t, found)
			require.Greater(t, nested.ExpiresAt, int64(0),
				"non-member author's self-reply must still be ephemeral")
		})
	}
}

// TestThreadAuthorExemptionDoesNotBypassRepliesDisabled verifies the exemption
// is scoped to the trust gate only: if the author disabled replies, even they
// cannot reply.
func TestThreadAuthorExemptionDoesNotBypassRepliesDisabled(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MaxPostsPerDay = 100
	params.MaxRepliesPerDay = 100
	params.CostPerByteExempt = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	f.ctx = sdkCtx.WithBlockTime(time.Unix(1_000_000, 0))

	author := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"
	f.repKeeper.IsActiveMemberFn = func(_ context.Context, _ sdk.AccAddress) bool { return false }

	postResp, err := msgServer.CreatePost(f.ctx, &types.MsgCreatePost{
		Creator: author,
		Title:   "No-Replies Post",
		Body:    "Body",
	})
	require.NoError(t, err)

	post, found := f.keeper.GetPost(f.ctx, postResp.Id)
	require.True(t, found)
	post.RepliesEnabled = false
	f.keeper.SetPost(f.ctx, post)

	_, err = msgServer.CreateReply(f.ctx, &types.MsgCreateReply{
		Creator: author,
		PostId:  postResp.Id,
		Body:    "Author cannot reply when replies disabled",
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrRepliesDisabled)
}

func TestTrustLevelDoesNotAffectPostCreation(t *testing.T) {
	// Post creation does not check trust level; it only checks rate limits
	// and params. A non-active member can still create posts (they just get
	// ephemeral TTL). Verify this works regardless of IsActiveMember.
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MaxPostsPerDay = 100
	params.CostPerByteExempt = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	f.ctx = sdkCtx.WithBlockTime(time.Unix(1_000_000, 0))

	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	// Non-active member can still create a post.
	f.repKeeper.IsActiveMemberFn = func(_ context.Context, _ sdk.AccAddress) bool { return false }

	resp, err := msgServer.CreatePost(f.ctx, &types.MsgCreatePost{
		Creator: creator,
		Title:   "Non-Member Post",
		Body:    "Should succeed with ephemeral TTL",
	})
	require.NoError(t, err)

	// Verify it was created as ephemeral (ExpiresAt > 0).
	post, found := f.keeper.GetPost(f.ctx, resp.Id)
	require.True(t, found)
	require.Greater(t, post.ExpiresAt, int64(0))
}

func TestActiveMemberGetsPermanentPost(t *testing.T) {
	// Active members get permanent posts (ExpiresAt == 0).
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MaxPostsPerDay = 100
	params.CostPerByteExempt = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	f.ctx = sdkCtx.WithBlockTime(time.Unix(1_000_000, 0))

	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	f.repKeeper.IsActiveMemberFn = func(_ context.Context, _ sdk.AccAddress) bool { return true }

	resp, err := msgServer.CreatePost(f.ctx, &types.MsgCreatePost{
		Creator: creator,
		Title:   "Active Member Post",
		Body:    "Should be permanent",
	})
	require.NoError(t, err)

	post, found := f.keeper.GetPost(f.ctx, resp.Id)
	require.True(t, found)
	require.Equal(t, int64(0), post.ExpiresAt)
}
