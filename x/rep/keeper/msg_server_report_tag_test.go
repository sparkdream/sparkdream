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

func TestMsgServerReportTag(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.ReportTag(f.ctx, &types.MsgReportTag{
			Creator: "invalid",
			TagName: "foo",
			Reason:  "spam",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("tag not found", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		creator := sdk.AccAddress([]byte("creator-reporter-1"))
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		// Register as active member.
		require.NoError(t, f.keeper.Member.Set(f.ctx, creatorStr, types.Member{
			Address:      creatorStr,
			Status:       types.MemberStatus_MEMBER_STATUS_ACTIVE,
			DreamBalance: keeper.PtrInt(math.NewInt(10_000_000)),
			StakedDream:  keeper.PtrInt(math.NewInt(0)),
		}))

		_, err := ms.ReportTag(f.ctx, &types.MsgReportTag{
			Creator: creatorStr,
			TagName: "nonexistent-tag",
			Reason:  "spam",
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrTagNotFound)
	})

	t.Run("successful first report", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		creator := sdk.AccAddress([]byte("creator-reporter-2"))
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		require.NoError(t, f.keeper.Member.Set(f.ctx, creatorStr, types.Member{
			Address:      creatorStr,
			Status:       types.MemberStatus_MEMBER_STATUS_ACTIVE,
			DreamBalance: keeper.PtrInt(math.NewInt(10_000_000)),
			StakedDream:  keeper.PtrInt(math.NewInt(0)),
		}))
		require.NoError(t, f.keeper.SetTag(f.ctx, types.Tag{Name: "report-test-tag"}))

		_, err := ms.ReportTag(f.ctx, &types.MsgReportTag{
			Creator: creatorStr,
			TagName: "report-test-tag",
			Reason:  "inappropriate content",
		})
		require.NoError(t, err)

		report, err := f.keeper.TagReport.Get(f.ctx, "report-test-tag")
		require.NoError(t, err)
		require.Equal(t, "report-test-tag", report.TagName)
		require.Contains(t, report.Reporters, creatorStr)
	})

	t.Run("duplicate report from same creator", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		creator := sdk.AccAddress([]byte("creator-reporter-3"))
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		require.NoError(t, f.keeper.Member.Set(f.ctx, creatorStr, types.Member{
			Address:      creatorStr,
			Status:       types.MemberStatus_MEMBER_STATUS_ACTIVE,
			DreamBalance: keeper.PtrInt(math.NewInt(10_000_000)),
			StakedDream:  keeper.PtrInt(math.NewInt(0)),
		}))
		require.NoError(t, f.keeper.SetTag(f.ctx, types.Tag{Name: "double-report-tag"}))
		require.NoError(t, f.keeper.TagReport.Set(f.ctx, "double-report-tag", types.TagReport{
			TagName:       "double-report-tag",
			TotalBond:     "100",
			FirstReportAt: f.ctx.BlockTime().Unix(),
			Reporters:     []string{creatorStr},
		}))

		_, err := ms.ReportTag(f.ctx, &types.MsgReportTag{
			Creator: creatorStr,
			TagName: "double-report-tag",
			Reason:  "spam",
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrTagReportAlreadyExists)
	})

	t.Run("additional reporter appended", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		first := sdk.AccAddress([]byte("creator-reporter-4a"))
		firstStr, _ := f.addressCodec.BytesToString(first)
		second := sdk.AccAddress([]byte("creator-reporter-4b"))
		secondStr, _ := f.addressCodec.BytesToString(second)

		for _, s := range []string{firstStr, secondStr} {
			require.NoError(t, f.keeper.Member.Set(f.ctx, s, types.Member{
				Address:      s,
				Status:       types.MemberStatus_MEMBER_STATUS_ACTIVE,
				DreamBalance: keeper.PtrInt(math.NewInt(10_000_000)),
				StakedDream:  keeper.PtrInt(math.NewInt(0)),
			}))
		}
		require.NoError(t, f.keeper.SetTag(f.ctx, types.Tag{Name: "multi-report-tag"}))
		require.NoError(t, f.keeper.TagReport.Set(f.ctx, "multi-report-tag", types.TagReport{
			TagName:       "multi-report-tag",
			TotalBond:     "10",
			FirstReportAt: f.ctx.BlockTime().Unix(),
			Reporters:     []string{firstStr},
		}))

		_, err := ms.ReportTag(f.ctx, &types.MsgReportTag{
			Creator: secondStr,
			TagName: "multi-report-tag",
			Reason:  "also problematic",
		})
		require.NoError(t, err)

		updated, err := f.keeper.TagReport.Get(f.ctx, "multi-report-tag")
		require.NoError(t, err)
		require.Contains(t, updated.Reporters, firstStr)
		require.Contains(t, updated.Reporters, secondStr)
	})
}

// REP-R-203: a reserved tag without a Tag row (seeded at genesis, or reserved
// after its row expired) can be reported, so the restore action that releases
// the reservation is reachable for it.
func TestMsgServerReportTag_ReservedTagWithoutRow(t *testing.T) {
	f := initFixture(t)
	f.commonsKeeper.IsCouncilAuthorizedFn = func(_ context.Context, _ string, _ string, _ string) bool { return true }
	ms := keeper.NewMsgServerImpl(f.keeper)
	authority, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())

	reporter := sdk.AccAddress([]byte("reserved-reporter-1"))
	reporterStr, _ := f.addressCodec.BytesToString(reporter)
	require.NoError(t, f.keeper.Member.Set(f.ctx, reporterStr, types.Member{
		Address:      reporterStr,
		Status:       types.MemberStatus_MEMBER_STATUS_ACTIVE,
		DreamBalance: keeper.PtrInt(math.NewInt(10_000_000)),
		StakedDream:  keeper.PtrInt(math.NewInt(0)),
	}))
	require.NoError(t, f.keeper.SetReservedTag(f.ctx, types.ReservedTag{Name: "phoenix", Authority: authority}))
	exists, err := f.keeper.TagExists(f.ctx, "phoenix")
	require.NoError(t, err)
	require.False(t, exists, "precondition: no Tag row")

	_, err = ms.ReportTag(f.ctx, &types.MsgReportTag{Creator: reporterStr, TagName: "phoenix", Reason: "wrongly reserved"})
	require.NoError(t, err)
	report, err := f.keeper.TagReport.Get(f.ctx, "phoenix")
	require.NoError(t, err)
	require.Equal(t, []string{reporterStr}, report.Reporters)

	// Restore releases the reservation and refunds the bond.
	_, err = ms.ResolveTagReport(f.ctx, &types.MsgResolveTagReport{Creator: authority, TagName: "phoenix", Action: 3})
	require.NoError(t, err)
	reserved, err := f.keeper.IsReservedTag(f.ctx, "phoenix")
	require.NoError(t, err)
	require.False(t, reserved)
	m, err := f.keeper.GetMember(f.ctx, reporter)
	require.NoError(t, err)
	require.True(t, keeper.DerefInt(m.DreamBalance).Equal(math.NewInt(10_000_000)), "bond refunded, got %s", keeper.DerefInt(m.DreamBalance))

	// A tag that is neither a row nor a reservation still cannot be reported.
	_, err = ms.ReportTag(f.ctx, &types.MsgReportTag{Creator: reporterStr, TagName: "phoenix", Reason: "again"})
	require.ErrorIs(t, err, types.ErrTagNotFound)
}
