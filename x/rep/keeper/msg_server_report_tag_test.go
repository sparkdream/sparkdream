package keeper_test

import (
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
			DreamBalance: keeper.PtrInt(math.NewInt(1000)),
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
			DreamBalance: keeper.PtrInt(math.NewInt(1000)),
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
			DreamBalance: keeper.PtrInt(math.NewInt(1000)),
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
				DreamBalance: keeper.PtrInt(math.NewInt(1000)),
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
