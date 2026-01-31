package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestQueryAppealCooldown(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.AppealCooldown(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("zero post_id", func(t *testing.T) {
		_, err := qs.AppealCooldown(f.ctx, &types.QueryAppealCooldownRequest{PostId: 0})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("no hide record", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		resp, err := qs.AppealCooldown(f.ctx, &types.QueryAppealCooldownRequest{PostId: post.PostId})
		require.NoError(t, err)
		require.False(t, resp.InCooldown)
		require.Equal(t, int64(0), resp.CooldownEnds)
	})

	t.Run("in cooldown", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)
		now := f.sdkCtx().BlockTime().Unix()

		// Create hide record with recent hide time
		hideRecord := types.HideRecord{
			PostId:   post.PostId,
			Sentinel: testSentinel,
			HiddenAt: now,
		}
		f.keeper.HideRecord.Set(f.ctx, post.PostId, hideRecord)

		resp, err := qs.AppealCooldown(f.ctx, &types.QueryAppealCooldownRequest{PostId: post.PostId})
		require.NoError(t, err)
		require.True(t, resp.InCooldown)
		require.Equal(t, now+types.DefaultHideAppealCooldown, resp.CooldownEnds)
	})

	t.Run("cooldown expired", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)
		now := f.sdkCtx().BlockTime().Unix()

		// Create hide record with old hide time (cooldown passed)
		hideRecord := types.HideRecord{
			PostId:   post.PostId,
			Sentinel: testSentinel,
			HiddenAt: now - types.DefaultHideAppealCooldown - 1,
		}
		f.keeper.HideRecord.Set(f.ctx, post.PostId, hideRecord)

		resp, err := qs.AppealCooldown(f.ctx, &types.QueryAppealCooldownRequest{PostId: post.PostId})
		require.NoError(t, err)
		require.False(t, resp.InCooldown)
	})
}
