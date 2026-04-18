package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestMsgServerDefendMemberReport(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.DefendMemberReport(f.ctx, &types.MsgDefendMemberReport{
			Creator: "invalid",
			Defense: "some defense",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("report not found", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		defender := sdk.AccAddress([]byte("defender_missing"))
		defenderStr, err := f.addressCodec.BytesToString(defender)
		require.NoError(t, err)

		_, err = ms.DefendMemberReport(f.ctx, &types.MsgDefendMemberReport{
			Creator: defenderStr,
			Defense: "I'm innocent",
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrReportNotFound)
	})

	t.Run("report not defendable (resolved)", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		defender := sdk.AccAddress([]byte("defender_resolv_"))
		defenderStr, err := f.addressCodec.BytesToString(defender)
		require.NoError(t, err)

		require.NoError(t, f.keeper.MemberReport.Set(f.ctx, defenderStr, types.MemberReport{
			Member: defenderStr,
			Status: types.MemberReportStatus_MEMBER_REPORT_STATUS_RESOLVED,
		}))

		_, err = ms.DefendMemberReport(f.ctx, &types.MsgDefendMemberReport{
			Creator: defenderStr,
			Defense: "too late",
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrReportNotPending)
	})

	t.Run("defense already submitted", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		defender := sdk.AccAddress([]byte("defender_dup____"))
		defenderStr, err := f.addressCodec.BytesToString(defender)
		require.NoError(t, err)

		require.NoError(t, f.keeper.MemberReport.Set(f.ctx, defenderStr, types.MemberReport{
			Member:  defenderStr,
			Status:  types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING,
			Defense: "already said",
		}))

		_, err = ms.DefendMemberReport(f.ctx, &types.MsgDefendMemberReport{
			Creator: defenderStr,
			Defense: "new attempt",
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrDefenseAlreadySubmitted)
	})

	t.Run("successful defense on pending report", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		defender := sdk.AccAddress([]byte("defender_happy__"))
		defenderStr, err := f.addressCodec.BytesToString(defender)
		require.NoError(t, err)

		require.NoError(t, f.keeper.MemberReport.Set(f.ctx, defenderStr, types.MemberReport{
			Member: defenderStr,
			Status: types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING,
		}))

		_, err = ms.DefendMemberReport(f.ctx, &types.MsgDefendMemberReport{
			Creator: defenderStr,
			Defense: "here is my side of the story",
		})
		require.NoError(t, err)

		report, err := f.keeper.MemberReport.Get(f.ctx, defenderStr)
		require.NoError(t, err)
		require.Equal(t, "here is my side of the story", report.Defense)
		require.NotZero(t, report.DefenseSubmittedAt)
	})

	t.Run("successful defense on escalated report", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		defender := sdk.AccAddress([]byte("defender_escal__"))
		defenderStr, err := f.addressCodec.BytesToString(defender)
		require.NoError(t, err)

		require.NoError(t, f.keeper.MemberReport.Set(f.ctx, defenderStr, types.MemberReport{
			Member: defenderStr,
			Status: types.MemberReportStatus_MEMBER_REPORT_STATUS_ESCALATED,
		}))

		_, err = ms.DefendMemberReport(f.ctx, &types.MsgDefendMemberReport{
			Creator: defenderStr,
			Defense: "defense during escalation",
		})
		require.NoError(t, err)

		report, err := f.keeper.MemberReport.Get(f.ctx, defenderStr)
		require.NoError(t, err)
		require.Equal(t, "defense during escalation", report.Defense)
	})
}
