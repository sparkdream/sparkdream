package keeper_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestMsgUpdateOperationalParams(t *testing.T) {
	t.Run("gov authority succeeds", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		authorityStr, err := f.addressCodec.BytesToString(f.keeper.GetAuthority())
		require.NoError(t, err)

		msg := &types.MsgUpdateOperationalParams{
			Authority:         authorityStr,
			OperationalParams: types.DefaultForumOperationalParams(),
		}

		_, err = ms.UpdateOperationalParams(f.ctx, msg)
		require.NoError(t, err)

		// Verify the params were stored
		storedParams, err := f.keeper.Params.Get(f.ctx)
		require.NoError(t, err)
		require.Equal(t, types.DefaultForumOperationalParams().EphemeralTtl, storedParams.EphemeralTtl)
	})

	t.Run("council authorized succeeds", func(t *testing.T) {
		mock := &mockCommonsKeeper{
			IsCouncilAuthorizedFn: func(ctx context.Context, addr string, council string, committee string) bool {
				return council == "commons" && committee == "operations"
			},
		}
		f := initFixtureWithCommons(t, mock)
		ms := keeper.NewMsgServerImpl(f.keeper)

		// Use a random non-authority address; it should still succeed because
		// the mock commons keeper returns true.
		msg := &types.MsgUpdateOperationalParams{
			Authority:         testCreator,
			OperationalParams: types.DefaultForumOperationalParams(),
		}

		_, err := ms.UpdateOperationalParams(f.ctx, msg)
		require.NoError(t, err)
	})

	t.Run("unauthorized fails", func(t *testing.T) {
		f := initFixture(t) // nil commonsKeeper => falls back to IsGovAuthority
		ms := keeper.NewMsgServerImpl(f.keeper)

		msg := &types.MsgUpdateOperationalParams{
			Authority:         testCreator, // not the gov authority
			OperationalParams: types.DefaultForumOperationalParams(),
		}

		_, err := ms.UpdateOperationalParams(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not authorized")
	})

	t.Run("invalid params fails", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		authorityStr, err := f.addressCodec.BytesToString(f.keeper.GetAuthority())
		require.NoError(t, err)

		badParams := types.DefaultForumOperationalParams()
		badParams.EphemeralTtl = -1

		msg := &types.MsgUpdateOperationalParams{
			Authority:         authorityStr,
			OperationalParams: badParams,
		}

		_, err = ms.UpdateOperationalParams(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "ephemeral_ttl must be positive")
	})

	t.Run("governance-only fields preserved", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		authorityStr, err := f.addressCodec.BytesToString(f.keeper.GetAuthority())
		require.NoError(t, err)

		// Set initial params with ForumPaused = true (governance-only field)
		initialParams := types.DefaultParams()
		initialParams.ForumPaused = true
		require.NoError(t, f.keeper.Params.Set(f.ctx, initialParams))

		// Send operational params update (does not include ForumPaused)
		msg := &types.MsgUpdateOperationalParams{
			Authority:         authorityStr,
			OperationalParams: types.DefaultForumOperationalParams(),
		}

		_, err = ms.UpdateOperationalParams(f.ctx, msg)
		require.NoError(t, err)

		// Verify governance-only field was preserved
		storedParams, err := f.keeper.Params.Get(f.ctx)
		require.NoError(t, err)
		require.True(t, storedParams.ForumPaused, "ForumPaused should still be true after operational params update")
	})
}
