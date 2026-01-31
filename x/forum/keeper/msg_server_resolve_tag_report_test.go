package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestMsgResolveTagReport(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	t.Run("invalid creator address", func(t *testing.T) {
		_, err := ms.ResolveTagReport(f.ctx, &types.MsgResolveTagReport{
			Creator: "invalid",
			TagName: "reported-tag",
			Action:  0,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("not governance authority", func(t *testing.T) {
		now := f.sdkCtx().BlockTime().Unix()

		// Create tag report
		report := types.TagReport{
			TagName:       "spam-tag",
			TotalBond:     "500",
			FirstReportAt: now,
			Reporters:     []string{testSentinel},
		}
		f.keeper.TagReport.Set(f.ctx, "spam-tag", report)

		// testCreator is not gov authority
		_, err := ms.ResolveTagReport(f.ctx, &types.MsgResolveTagReport{
			Creator: testCreator,
			TagName: "spam-tag",
			Action:  0,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "governance authority")
	})

	t.Run("report not found", func(t *testing.T) {
		authority, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())

		_, err := ms.ResolveTagReport(f.ctx, &types.MsgResolveTagReport{
			Creator: authority,
			TagName: "nonexistent-tag",
			Action:  0,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "no report found")
	})

	t.Run("tag not found", func(t *testing.T) {
		now := f.sdkCtx().BlockTime().Unix()
		authority, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())

		// Create report for non-existent tag
		report := types.TagReport{
			TagName:       "missing-tag",
			TotalBond:     "500",
			FirstReportAt: now,
			Reporters:     []string{testSentinel},
		}
		f.keeper.TagReport.Set(f.ctx, "missing-tag", report)

		_, err := ms.ResolveTagReport(f.ctx, &types.MsgResolveTagReport{
			Creator: authority,
			TagName: "missing-tag",
			Action:  0,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})

	t.Run("success dismiss", func(t *testing.T) {
		now := f.sdkCtx().BlockTime().Unix()
		authority, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())

		// Create tag
		tag := f.createTestTag(t, "good-tag")
		_ = tag

		// Create report
		report := types.TagReport{
			TagName:       "good-tag",
			TotalBond:     "500",
			FirstReportAt: now,
			Reporters:     []string{testSentinel},
		}
		f.keeper.TagReport.Set(f.ctx, "good-tag", report)

		_, err := ms.ResolveTagReport(f.ctx, &types.MsgResolveTagReport{
			Creator: authority,
			TagName: "good-tag",
			Action:  0, // Dismiss
		})
		require.NoError(t, err)

		// Verify report was removed
		_, err = f.keeper.TagReport.Get(f.ctx, "good-tag")
		require.Error(t, err)

		// Verify tag still exists
		foundTag, err := f.keeper.Tag.Get(f.ctx, "good-tag")
		require.NoError(t, err)
		require.Equal(t, "good-tag", foundTag.Name)
	})

	t.Run("success remove tag", func(t *testing.T) {
		now := f.sdkCtx().BlockTime().Unix()
		authority, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())

		// Create tag
		tag := f.createTestTag(t, "bad-tag")
		_ = tag

		// Create report
		report := types.TagReport{
			TagName:       "bad-tag",
			TotalBond:     "500",
			FirstReportAt: now,
			Reporters:     []string{testSentinel},
		}
		f.keeper.TagReport.Set(f.ctx, "bad-tag", report)

		_, err := ms.ResolveTagReport(f.ctx, &types.MsgResolveTagReport{
			Creator: authority,
			TagName: "bad-tag",
			Action:  1, // Remove tag
		})
		require.NoError(t, err)

		// Verify report was removed
		_, err = f.keeper.TagReport.Get(f.ctx, "bad-tag")
		require.Error(t, err)

		// Verify tag was removed
		_, err = f.keeper.Tag.Get(f.ctx, "bad-tag")
		require.Error(t, err)
	})

	t.Run("success reserve tag", func(t *testing.T) {
		now := f.sdkCtx().BlockTime().Unix()
		authority, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())

		// Create tag
		tag := f.createTestTag(t, "reserved-tag")
		_ = tag

		// Create report
		report := types.TagReport{
			TagName:       "reserved-tag",
			TotalBond:     "500",
			FirstReportAt: now,
			Reporters:     []string{testSentinel},
		}
		f.keeper.TagReport.Set(f.ctx, "reserved-tag", report)

		_, err := ms.ResolveTagReport(f.ctx, &types.MsgResolveTagReport{
			Creator:              authority,
			TagName:              "reserved-tag",
			Action:               2, // Reserve tag
			ReserveAuthority:     "council1",
			ReserveMembersCanUse: true,
		})
		require.NoError(t, err)

		// Verify report was removed
		_, err = f.keeper.TagReport.Get(f.ctx, "reserved-tag")
		require.Error(t, err)

		// Verify reserved tag was created
		reservedTag, err := f.keeper.ReservedTag.Get(f.ctx, "reserved-tag")
		require.NoError(t, err)
		require.Equal(t, "council1", reservedTag.Authority)
		require.True(t, reservedTag.MembersCanUse)
	})
}
