package keeper_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestMsgResolveTagReport(t *testing.T) {
	// Always-authorized policy so msg.Creator passes the commons check.
	newFixture := func(t *testing.T) *fixture {
		return initFixture(t, WithAuthorizationPolicy(AlwaysAuthorized))
	}

	authorityString := func(f *fixture) string {
		addr, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())
		return addr
	}

	t.Run("invalid creator address", func(t *testing.T) {
		f := newFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.ResolveTagReport(f.ctx, &types.MsgResolveTagReport{
			Creator: "invalid",
			TagName: "t",
			Action:  0,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("not authorized", func(t *testing.T) {
		// NeverAuthorized policy rejects every caller.
		f := initFixture(t, WithAuthorizationPolicy(NeverAuthorized))
		// Force IsCouncilAuthorized to return false too.
		f.commonsKeeper.IsCouncilAuthorizedFn = func(_ context.Context, _ string, _ string, _ string) bool {
			return false
		}
		ms := keeper.NewMsgServerImpl(f.keeper)

		require.NoError(t, f.keeper.SetTag(f.ctx, types.Tag{Name: "spam-tag"}))
		require.NoError(t, f.keeper.TagReport.Set(f.ctx, "spam-tag", types.TagReport{
			TagName: "spam-tag",
		}))

		_, err := ms.ResolveTagReport(f.ctx, &types.MsgResolveTagReport{
			Creator: authorityString(f),
			TagName: "spam-tag",
			Action:  0,
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrTagReportNotAuthorized)
	})

	t.Run("report not found", func(t *testing.T) {
		f := newFixture(t)
		f.commonsKeeper.IsCouncilAuthorizedFn = func(_ context.Context, _ string, _ string, _ string) bool {
			return true
		}
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.ResolveTagReport(f.ctx, &types.MsgResolveTagReport{
			Creator: authorityString(f),
			TagName: "nope",
			Action:  0,
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrTagReportNotFound)
	})

	t.Run("tag not found", func(t *testing.T) {
		f := newFixture(t)
		f.commonsKeeper.IsCouncilAuthorizedFn = func(_ context.Context, _ string, _ string, _ string) bool {
			return true
		}
		ms := keeper.NewMsgServerImpl(f.keeper)

		require.NoError(t, f.keeper.TagReport.Set(f.ctx, "orphan-tag", types.TagReport{
			TagName: "orphan-tag",
		}))

		_, err := ms.ResolveTagReport(f.ctx, &types.MsgResolveTagReport{
			Creator: authorityString(f),
			TagName: "orphan-tag",
			Action:  0,
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrTagNotFound)
	})

	t.Run("dismiss", func(t *testing.T) {
		f := newFixture(t)
		f.commonsKeeper.IsCouncilAuthorizedFn = func(_ context.Context, _ string, _ string, _ string) bool {
			return true
		}
		ms := keeper.NewMsgServerImpl(f.keeper)

		require.NoError(t, f.keeper.SetTag(f.ctx, types.Tag{Name: "good-tag"}))
		require.NoError(t, f.keeper.TagReport.Set(f.ctx, "good-tag", types.TagReport{
			TagName: "good-tag",
		}))

		_, err := ms.ResolveTagReport(f.ctx, &types.MsgResolveTagReport{
			Creator: authorityString(f),
			TagName: "good-tag",
			Action:  0,
		})
		require.NoError(t, err)

		_, err = f.keeper.TagReport.Get(f.ctx, "good-tag")
		require.Error(t, err)

		// Tag still exists.
		_, err = f.keeper.GetTag(f.ctx, "good-tag")
		require.NoError(t, err)
	})

	t.Run("remove tag", func(t *testing.T) {
		f := newFixture(t)
		f.commonsKeeper.IsCouncilAuthorizedFn = func(_ context.Context, _ string, _ string, _ string) bool {
			return true
		}
		ms := keeper.NewMsgServerImpl(f.keeper)

		require.NoError(t, f.keeper.SetTag(f.ctx, types.Tag{Name: "bad-tag"}))
		require.NoError(t, f.keeper.TagReport.Set(f.ctx, "bad-tag", types.TagReport{
			TagName: "bad-tag",
		}))

		_, err := ms.ResolveTagReport(f.ctx, &types.MsgResolveTagReport{
			Creator: authorityString(f),
			TagName: "bad-tag",
			Action:  1,
		})
		require.NoError(t, err)

		_, err = f.keeper.GetTag(f.ctx, "bad-tag")
		require.Error(t, err)
	})

	t.Run("reserve tag", func(t *testing.T) {
		f := newFixture(t)
		f.commonsKeeper.IsCouncilAuthorizedFn = func(_ context.Context, _ string, _ string, _ string) bool {
			return true
		}
		ms := keeper.NewMsgServerImpl(f.keeper)

		require.NoError(t, f.keeper.SetTag(f.ctx, types.Tag{Name: "reserved-tag"}))
		require.NoError(t, f.keeper.TagReport.Set(f.ctx, "reserved-tag", types.TagReport{
			TagName: "reserved-tag",
		}))

		_, err := ms.ResolveTagReport(f.ctx, &types.MsgResolveTagReport{
			Creator:              authorityString(f),
			TagName:              "reserved-tag",
			Action:               2,
			ReserveAuthority:     "council1",
			ReserveMembersCanUse: true,
		})
		require.NoError(t, err)

		isReserved, err := f.keeper.IsReservedTag(f.ctx, "reserved-tag")
		require.NoError(t, err)
		require.True(t, isReserved)

		rt, err := f.keeper.GetReservedTag(f.ctx, "reserved-tag")
		require.NoError(t, err)
		require.Equal(t, "council1", rt.Authority)
		require.True(t, rt.MembersCanUse)
	})
}
