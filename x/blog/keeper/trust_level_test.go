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

			creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

			// Creator must be active to create the post itself.
			f.repKeeper.IsActiveMemberFn = func(_ context.Context, _ sdk.AccAddress) bool { return true }

			// Create a post with the desired MinReplyTrustLevel.
			postResp, err := msgServer.CreatePost(f.ctx, &types.MsgCreatePost{
				Creator:            creator,
				Title:              "Trust Level Post",
				Body:               "Body for trust level test",
				MinReplyTrustLevel: tt.minReplyTrustLevel,
			})
			require.NoError(t, err)

			// Now set the active member mock for the reply attempt.
			f.repKeeper.IsActiveMemberFn = func(_ context.Context, _ sdk.AccAddress) bool {
				return tt.isActiveMember
			}
			f.repKeeper.GetTrustLevelFn = func(_ context.Context, _ sdk.AccAddress) (reptypes.TrustLevel, error) {
				return tt.callerTrustLevel, nil
			}

			_, err = msgServer.CreateReply(f.ctx, &types.MsgCreateReply{
				Creator: creator,
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
