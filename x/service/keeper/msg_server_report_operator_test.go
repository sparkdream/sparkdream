package keeper_test

import (
	"context"
	"errors"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/service/types"
)

func TestMsgReportOperator_HappyPath(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))

	resp, err := f.msgServer.ReportOperator(f.ctx, &types.MsgReportOperator{
		Reporter:    testReporter,
		Operator:    testOperator1,
		ServiceType: testServiceType,
		Reason:      "broken-deployment",
	})
	require.NoError(t, err)
	require.NotZero(t, resp.ReportId)

	report, err := f.keeper.Reports.Get(f.ctx, resp.ReportId)
	require.NoError(t, err)
	require.Equal(t, types.ReportStatus_REPORT_STATUS_PENDING, report.Status)
	require.Equal(t, testReporter, report.Reporter)
	require.Equal(t, testOperator1, report.OperatorAddress)
	require.True(t, report.Deposit.Amount.IsPositive())

	// ReportsByOperator index seeded.
	has, _ := f.keeper.ReportsByOperator.Has(f.ctx, collections.Join3(testOperator1Addr.Bytes(), testServiceType, resp.ReportId))
	require.True(t, has)
	// PendingReportsQueue index seeded at filed_at.
	hasQ, _ := f.keeper.PendingReportsQueue.Has(f.ctx, collections.Join(report.FiledAt, resp.ReportId))
	require.True(t, hasQ)

	// Deposit escrowed from reporter.
	require.Len(t, f.bankKeeper.AcctToModCalls, 1)
	require.Equal(t, testReporterAddr, f.bankKeeper.AcctToModCalls[0].Sender)
}

func TestMsgReportOperator_TrustLevelGate(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))

	f.repKeeper.MeetsTrustLevelFn = func(_ context.Context, _ sdk.AccAddress, _ string) (bool, error) {
		return false, nil
	}

	_, err := f.msgServer.ReportOperator(f.ctx, &types.MsgReportOperator{
		Reporter:    testReporter,
		Operator:    testOperator1,
		ServiceType: testServiceType,
		Reason:      "low-trust",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrReporterTrustLevelTooLow.Error())
}

func TestMsgReportOperator_TrustLevelError(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))

	f.repKeeper.MeetsTrustLevelFn = func(_ context.Context, _ sdk.AccAddress, _ string) (bool, error) {
		return false, errors.New("rep down")
	}

	_, err := f.msgServer.ReportOperator(f.ctx, &types.MsgReportOperator{
		Reporter:    testReporter,
		Operator:    testOperator1,
		ServiceType: testServiceType,
		Reason:      "rep-error",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "rep down")
}

func TestMsgReportOperator_ReporterIsControllerMember(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))

	f.commonsKeeper.IsGroupPolicyMemberFn = func(_ context.Context, _ string, _ string) (bool, error) {
		return true, nil
	}

	_, err := f.msgServer.ReportOperator(f.ctx, &types.MsgReportOperator{
		Reporter:    testReporter,
		Operator:    testOperator1,
		ServiceType: testServiceType,
		Reason:      "controller-member",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrReporterIsControllerMember.Error())
}

func TestMsgReportOperator_OperatorNotFound(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)

	_, err := f.msgServer.ReportOperator(f.ctx, &types.MsgReportOperator{
		Reporter:    testReporter,
		Operator:    testOperator1,
		ServiceType: testServiceType,
		Reason:      "missing-op",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrOperatorNotFound.Error())
}

func TestMsgReportOperator_ReasonTooLong(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))

	_, err := f.msgServer.ReportOperator(f.ctx, &types.MsgReportOperator{
		Reporter:    testReporter,
		Operator:    testOperator1,
		ServiceType: testServiceType,
		Reason:      string(make([]byte, types.DefaultMaxReasonBytes+1)),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrInvalidReasonSize.Error())
}

func TestMsgReportOperator_RateLimitCap(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))

	// Default MaxReportsPerReporter... = 3. File 3 successful, then 4th fails.
	for i := 0; i < 3; i++ {
		_, err := f.msgServer.ReportOperator(f.ctx, &types.MsgReportOperator{
			Reporter:    testReporter,
			Operator:    testOperator1,
			ServiceType: testServiceType,
			Reason:      "rate-limit-fill",
		})
		require.NoError(t, err, "expected report %d to succeed", i+1)
	}

	_, err := f.msgServer.ReportOperator(f.ctx, &types.MsgReportOperator{
		Reporter:    testReporter,
		Operator:    testOperator1,
		ServiceType: testServiceType,
		Reason:      "over-cap",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrReporterRateLimitExceeded.Error())
}
