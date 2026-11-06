package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
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

	// REP-E-11: a report whose tag is gone still resolves, and the reporter's
	// bond is settled instead of stranded.
	t.Run("tag not found still settles bonds", func(t *testing.T) {
		f := newFixture(t)
		f.commonsKeeper.IsCouncilAuthorizedFn = func(_ context.Context, _ string, _ string, _ string) bool {
			return true
		}
		ms := keeper.NewMsgServerImpl(f.keeper)

		reporter := sdk.AccAddress([]byte("orphan-reporter"))
		bond := types.DefaultTagReportBond
		require.NoError(t, f.keeper.Member.Set(f.ctx, reporter.String(), types.Member{
			Address:        reporter.String(),
			Status:         types.MemberStatus_MEMBER_STATUS_ACTIVE,
			DreamBalance:   keeper.PtrInt(bond.MulRaw(5)),
			StakedDream:    keeper.PtrInt(bond),
			LifetimeEarned: keeper.PtrInt(math.ZeroInt()),
			LifetimeBurned: keeper.PtrInt(math.ZeroInt()),
		}))
		require.NoError(t, f.keeper.TagReport.Set(f.ctx, "orphan-tag", types.TagReport{
			TagName:   "orphan-tag",
			Reporters: []string{reporter.String()},
			TotalBond: bond.String(),
		}))

		_, err := ms.ResolveTagReport(f.ctx, &types.MsgResolveTagReport{
			Creator: authorityString(f),
			TagName: "orphan-tag",
			Action:  1,
		})
		require.NoError(t, err)
		_, err = f.keeper.TagReport.Get(f.ctx, "orphan-tag")
		require.Error(t, err, "report resolved")
		m, err := f.keeper.Member.Get(f.ctx, reporter.String())
		require.NoError(t, err)
		require.True(t, m.StakedDream.IsZero(), "bond refunded")
		require.Equal(t, bond.MulRaw(5), *m.DreamBalance)
	})

	// REP-E-11: a dismissed report forfeits its bonds.
	t.Run("dismiss burns bonds", func(t *testing.T) {
		f := newFixture(t)
		f.commonsKeeper.IsCouncilAuthorizedFn = func(_ context.Context, _ string, _ string, _ string) bool {
			return true
		}
		ms := keeper.NewMsgServerImpl(f.keeper)

		reporter := sdk.AccAddress([]byte("dismissed-reporter"))
		bond := types.DefaultTagReportBond
		require.NoError(t, f.keeper.Member.Set(f.ctx, reporter.String(), types.Member{
			Address:        reporter.String(),
			Status:         types.MemberStatus_MEMBER_STATUS_ACTIVE,
			DreamBalance:   keeper.PtrInt(bond.MulRaw(5)),
			StakedDream:    keeper.PtrInt(bond),
			LifetimeEarned: keeper.PtrInt(math.ZeroInt()),
			LifetimeBurned: keeper.PtrInt(math.ZeroInt()),
		}))
		require.NoError(t, f.keeper.SetTag(f.ctx, types.Tag{Name: "fine-tag"}))
		require.NoError(t, f.keeper.TagReport.Set(f.ctx, "fine-tag", types.TagReport{
			TagName:   "fine-tag",
			Reporters: []string{reporter.String()},
			TotalBond: bond.String(),
		}))

		_, err := ms.ResolveTagReport(f.ctx, &types.MsgResolveTagReport{
			Creator: authorityString(f),
			TagName: "fine-tag",
			Action:  0,
		})
		require.NoError(t, err)
		m, err := f.keeper.Member.Get(f.ctx, reporter.String())
		require.NoError(t, err)
		require.True(t, m.StakedDream.IsZero())
		require.Equal(t, bond.MulRaw(4), *m.DreamBalance, "bond burned")
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
			ReserveAuthority:     authorityString(f),
			ReserveMembersCanUse: true,
		})
		require.NoError(t, err)

		isReserved, err := f.keeper.IsReservedTag(f.ctx, "reserved-tag")
		require.NoError(t, err)
		require.True(t, isReserved)

		rt, err := f.keeper.GetReservedTag(f.ctx, "reserved-tag")
		require.NoError(t, err)
		require.Equal(t, authorityString(f), rt.Authority)
		require.True(t, rt.MembersCanUse)
	})

	t.Run("invalid action keeps report and tag", func(t *testing.T) {
		f := newFixture(t)
		f.commonsKeeper.IsCouncilAuthorizedFn = func(_ context.Context, _ string, _ string, _ string) bool {
			return true
		}
		ms := keeper.NewMsgServerImpl(f.keeper)

		report := types.TagReport{
			TagName:   "zenith-tag",
			Reporters: []string{authorityString(f)},
			TotalBond: "1000",
		}
		require.NoError(t, f.keeper.SetTag(f.ctx, types.Tag{Name: "zenith-tag"}))
		require.NoError(t, f.keeper.TagReport.Set(f.ctx, "zenith-tag", report))

		_, err := ms.ResolveTagReport(f.ctx, &types.MsgResolveTagReport{
			Creator: authorityString(f),
			TagName: "zenith-tag",
			Action:  3,
		})
		require.ErrorIs(t, err, types.ErrInvalidTagReportAction)

		// Report (and the reporters' locked bond it tracks) survives.
		got, err := f.keeper.TagReport.Get(f.ctx, "zenith-tag")
		require.NoError(t, err)
		require.Equal(t, report.Reporters, got.Reporters)
		require.Equal(t, "1000", got.TotalBond)
		_, err = f.keeper.GetTag(f.ctx, "zenith-tag")
		require.NoError(t, err)
	})
}

// Action 3 (restore) releases a reserved tag back to public use and refunds
// the reporters; it is refused for a tag that is not reserved.
func TestMsgResolveTagReport_Restore(t *testing.T) {
	f := initFixture(t, WithAuthorizationPolicy(AlwaysAuthorized))
	f.commonsKeeper.IsCouncilAuthorizedFn = func(_ context.Context, _ string, _ string, _ string) bool { return true }
	ms := keeper.NewMsgServerImpl(f.keeper)
	authority, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())
	reporter := sdk.AccAddress([]byte("tag_restore_reporter")).String()

	require.NoError(t, f.keeper.SetTag(f.ctx, types.Tag{Name: "aurora"}))
	require.NoError(t, f.keeper.TagReport.Set(f.ctx, "aurora", types.TagReport{
		TagName: "aurora", TotalBond: math.ZeroInt().String(), Reporters: []string{reporter}, UnderReview: true,
	}))
	// Not reserved: restore is refused and the report survives.
	_, err := ms.ResolveTagReport(f.ctx, &types.MsgResolveTagReport{Creator: authority, TagName: "aurora", Action: 3})
	require.ErrorIs(t, err, types.ErrInvalidTagReportAction)
	_, err = f.keeper.TagReport.Get(f.ctx, "aurora")
	require.NoError(t, err)

	require.NoError(t, f.keeper.SetReservedTag(f.ctx, types.ReservedTag{Name: "aurora", Authority: authority}))
	_, err = ms.ResolveTagReport(f.ctx, &types.MsgResolveTagReport{Creator: authority, TagName: "aurora", Action: 3})
	require.NoError(t, err)
	reserved, err := f.keeper.IsReservedTag(f.ctx, "aurora")
	require.NoError(t, err)
	require.False(t, reserved, "the reservation is released")
	_, err = f.keeper.TagReport.Get(f.ctx, "aurora")
	require.Error(t, err, "the report is resolved")

	// Action 4 is still unknown.
	_, err = ms.ResolveTagReport(f.ctx, &types.MsgResolveTagReport{Creator: authority, TagName: "aurora", Action: 4})
	require.ErrorIs(t, err, types.ErrInvalidTagReportAction)
}

// REP-R-203: reserve_authority is stored as-is and read by clients deciding
// who may use the tag, so a value that is not an address is refused before
// the report is touched; empty stays allowed.
func TestMsgResolveTagReport_ReserveAuthorityValidated(t *testing.T) {
	f := initFixture(t, WithAuthorizationPolicy(AlwaysAuthorized))
	f.commonsKeeper.IsCouncilAuthorizedFn = func(_ context.Context, _ string, _ string, _ string) bool { return true }
	ms := keeper.NewMsgServerImpl(f.keeper)
	authority, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())

	for _, tc := range []struct {
		name, reserveAuthority string
		ok                     bool
	}{
		{"plain word", "council1", false},
		{"broken checksum", "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xx", false},
		{"empty", "", true},
		{"bech32 address", authority, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tag := "zenith-" + tc.name
			require.NoError(t, f.keeper.SetTag(f.ctx, types.Tag{Name: tag}))
			require.NoError(t, f.keeper.TagReport.Set(f.ctx, tag, types.TagReport{TagName: tag, Reporters: []string{authority}}))
			_, err := ms.ResolveTagReport(f.ctx, &types.MsgResolveTagReport{
				Creator: authority, TagName: tag, Action: 2, ReserveAuthority: tc.reserveAuthority,
			})
			reserved, rErr := f.keeper.IsReservedTag(f.ctx, tag)
			require.NoError(t, rErr)
			_, reportErr := f.keeper.TagReport.Get(f.ctx, tag)
			if tc.ok {
				require.NoError(t, err)
				require.True(t, reserved)
				require.Error(t, reportErr, "the report is resolved")
				return
			}
			require.ErrorIs(t, err, types.ErrInvalidTagReportAction)
			require.False(t, reserved)
			require.NoError(t, reportErr, "the report (and its bonds) survive a refused resolution")
		})
	}
}
