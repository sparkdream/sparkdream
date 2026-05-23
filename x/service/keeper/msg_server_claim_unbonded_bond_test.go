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

// claimReady seeds an UNBONDING operator past unbond_complete_at.
func (f *fixture) claimReadyOperator(t *testing.T, bond math.Int) types.Operator {
	t.Helper()
	cfg := f.seedServiceType(t)
	op := f.seedActiveOperator(t, testOperator1, testController, bond)
	op.Status = types.OperatorStatus_OPERATOR_STATUS_UNBONDING
	op.UnbondCompleteAt = f.sdkCtx().BlockHeight() + 1
	require.NoError(t, f.keeper.PutOperator(f.ctx, op))
	_ = cfg
	// Advance past unbond_complete_at.
	f.withBlockHeight(op.UnbondCompleteAt + 1)
	return op
}

func TestMsgClaimUnbondedBond_HappyPath(t *testing.T) {
	f := initFixture(t)
	bondAmt := math.NewInt(2_000_000)
	f.claimReadyOperator(t, bondAmt)

	_, err := f.msgServer.ClaimUnbondedBond(f.ctx, &types.MsgClaimUnbondedBond{
		Operator:    testOperator1,
		ServiceType: testServiceType,
	})
	require.NoError(t, err)

	// Live record gone.
	_, ok := f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), testServiceType)
	require.False(t, ok)

	// Bank returned the bond.
	require.Len(t, f.bankKeeper.ModToAcctCalls, 1)
	call := f.bankKeeper.ModToAcctCalls[0]
	require.Equal(t, testOperator1Addr, call.Recipient)
	require.Equal(t, types.ModuleName, call.Module)
	require.True(t, call.Amt.AmountOf(testBondDenom).Equal(bondAmt))

	// AfterOperatorRetired hook fired.
	require.Len(t, f.hooks.Retired, 1)
	require.Equal(t, testOperator1Addr, f.hooks.Retired[0].Operator)
	require.Equal(t, testServiceType, f.hooks.Retired[0].ServiceType)
}

func TestMsgClaimUnbondedBond_BeforeUnbondingPeriod(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	op := f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))
	op.Status = types.OperatorStatus_OPERATOR_STATUS_UNBONDING
	op.UnbondCompleteAt = f.sdkCtx().BlockHeight() + 100
	require.NoError(t, f.keeper.PutOperator(f.ctx, op))

	_, err := f.msgServer.ClaimUnbondedBond(f.ctx, &types.MsgClaimUnbondedBond{
		Operator:    testOperator1,
		ServiceType: testServiceType,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrUnbondingPeriodNotElapsed.Error())
}

func TestMsgClaimUnbondedBond_NotUnbonding(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))

	_, err := f.msgServer.ClaimUnbondedBond(f.ctx, &types.MsgClaimUnbondedBond{
		Operator:    testOperator1,
		ServiceType: testServiceType,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrOperatorNotActive.Error())
}

func TestMsgClaimUnbondedBond_OpenReportBlocks(t *testing.T) {
	f := initFixture(t)
	f.claimReadyOperator(t, math.NewInt(2_000_000))

	// Insert a PENDING report against the operator.
	reportID, err := f.keeper.NextReportID.Next(f.ctx)
	require.NoError(t, err)
	report := types.Report{
		ReportId:        reportID,
		OperatorAddress: testOperator1,
		ServiceType:     testServiceType,
		Reporter:        testReporter,
		Status:          types.ReportStatus_REPORT_STATUS_PENDING,
		SlashAmount:     math.ZeroInt(),
		Deposit:         math.NewInt(10_000_000),
	}
	require.NoError(t, f.keeper.Reports.Set(f.ctx, reportID, report))
	require.NoError(t, f.keeper.ReportsByOperator.Set(f.ctx, collections.Join3(testOperator1Addr.Bytes(), testServiceType, reportID)))

	_, err = f.msgServer.ClaimUnbondedBond(f.ctx, &types.MsgClaimUnbondedBond{
		Operator:    testOperator1,
		ServiceType: testServiceType,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrOpenReports.Error())
}

func TestMsgClaimUnbondedBond_ActiveEscrowBlocks(t *testing.T) {
	f := initFixture(t)
	f.claimReadyOperator(t, math.NewInt(2_000_000))

	// Insert an active escrow (release_at in the future).
	escrowID, err := f.keeper.NextEscrowID.Next(f.ctx)
	require.NoError(t, err)
	releaseAt := f.sdkCtx().BlockHeight() + 1000
	escrow := types.Tier1EscrowEntry{
		EscrowId:        escrowID,
		ReportId:        0,
		OperatorAddress: testOperator1,
		ServiceType:     testServiceType,
		Amount:          math.NewInt(100_000),
		ReleaseAt:       releaseAt,
	}
	require.NoError(t, f.keeper.Tier1Escrow.Set(f.ctx, escrowID, escrow))
	require.NoError(t, f.keeper.Tier1EscrowByOperator.Set(f.ctx,
		collections.Join3(testOperator1Addr.Bytes(), testServiceType, escrowID)))
	require.NoError(t, f.keeper.Tier1EscrowReleaseQueue.Set(f.ctx, collections.Join(releaseAt, escrowID)))

	_, err = f.msgServer.ClaimUnbondedBond(f.ctx, &types.MsgClaimUnbondedBond{
		Operator:    testOperator1,
		ServiceType: testServiceType,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrEscrowStillActive.Error())
}

func TestMsgClaimUnbondedBond_RepGrantFailureIsNonFatal(t *testing.T) {
	f := initFixture(t)
	f.claimReadyOperator(t, math.NewInt(2_000_000))

	// Make rep grant fail — bond return must still succeed.
	f.repKeeper.AddReputationFn = func(_ context.Context, _ sdk.AccAddress, _ string, _ math.LegacyDec) error {
		return errors.New("simulated rep grant failure")
	}

	_, err := f.msgServer.ClaimUnbondedBond(f.ctx, &types.MsgClaimUnbondedBond{
		Operator:    testOperator1,
		ServiceType: testServiceType,
	})
	require.NoError(t, err)

	// Bond still returned despite rep grant failure.
	require.Len(t, f.bankKeeper.ModToAcctCalls, 1)
}
