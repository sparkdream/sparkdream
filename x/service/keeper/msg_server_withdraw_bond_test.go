package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/service/keeper"
	"sparkdream/x/service/types"
)

// withdrawFixture seeds the test service type (min_bond 1_000_000,
// unbonding 20 blocks) and an ACTIVE operator with `bond`.
func (f *fixture) withdrawFixture(t *testing.T, bond math.Int) types.Operator {
	t.Helper()
	f.seedServiceType(t)
	return f.seedActiveOperator(t, testOperator1, testController, bond)
}

func (f *fixture) getOp(t *testing.T) types.Operator {
	t.Helper()
	op, ok := f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), testServiceType)
	require.True(t, ok)
	return op
}

func TestMsgWithdrawBond_QueuesAndStaysActive(t *testing.T) {
	f := initFixture(t)
	f.withdrawFixture(t, math.NewInt(3_000_000))
	height := f.sdkCtx().BlockHeight()

	_, err := f.msgServer.WithdrawBond(f.ctx, &types.MsgWithdrawBond{
		Operator: testOperator1, ServiceType: testServiceType, Amount: math.NewInt(1_500_000),
	})
	require.NoError(t, err)

	op := f.getOp(t)
	require.Equal(t, types.OperatorStatus_OPERATOR_STATUS_ACTIVE, op.Status)
	require.True(t, op.BondAmount.Equal(math.NewInt(3_000_000)), "bond untouched until claim")
	require.True(t, op.PendingWithdrawAmount.Equal(math.NewInt(1_500_000)))
	require.Equal(t, height+20, op.WithdrawCompleteAt)
	require.Len(t, f.bankKeeper.ModToAcctCalls, 0, "nothing leaves the pool at queue time")
}

func TestMsgWithdrawBond_RejectsBelowMinBond(t *testing.T) {
	f := initFixture(t)
	f.withdrawFixture(t, math.NewInt(3_000_000))

	// 3_000_000 - 1_000_000 min = 2_000_000 available.
	_, err := f.msgServer.WithdrawBond(f.ctx, &types.MsgWithdrawBond{
		Operator: testOperator1, ServiceType: testServiceType, Amount: math.NewInt(2_000_001),
	})
	require.ErrorIs(t, err, types.ErrWithdrawExceedsAvailable)

	// Exactly the spare amount is fine.
	_, err = f.msgServer.WithdrawBond(f.ctx, &types.MsgWithdrawBond{
		Operator: testOperator1, ServiceType: testServiceType, Amount: math.NewInt(2_000_000),
	})
	require.NoError(t, err)
}

func TestMsgWithdrawBond_IncrementalAccumulatesAndResetsClock(t *testing.T) {
	f := initFixture(t)
	f.withdrawFixture(t, math.NewInt(3_000_000))

	_, err := f.msgServer.WithdrawBond(f.ctx, &types.MsgWithdrawBond{
		Operator: testOperator1, ServiceType: testServiceType, Amount: math.NewInt(500_000),
	})
	require.NoError(t, err)
	first := f.getOp(t).WithdrawCompleteAt

	f.withBlockHeight(f.sdkCtx().BlockHeight() + 5)
	_, err = f.msgServer.WithdrawBond(f.ctx, &types.MsgWithdrawBond{
		Operator: testOperator1, ServiceType: testServiceType, Amount: math.NewInt(1_500_000),
	})
	require.NoError(t, err)
	op := f.getOp(t)
	require.True(t, op.PendingWithdrawAmount.Equal(math.NewInt(2_000_000)))
	require.Equal(t, first+5, op.WithdrawCompleteAt, "clock resets to now + period")

	// Pending is counted against availability: nothing left.
	_, err = f.msgServer.WithdrawBond(f.ctx, &types.MsgWithdrawBond{
		Operator: testOperator1, ServiceType: testServiceType, Amount: math.NewInt(1),
	})
	require.ErrorIs(t, err, types.ErrWithdrawExceedsAvailable)
}

func TestMsgWithdrawBond_StatusGates(t *testing.T) {
	f := initFixture(t)
	op := f.withdrawFixture(t, math.NewInt(3_000_000))

	op.Status = types.OperatorStatus_OPERATOR_STATUS_UNBONDING
	require.NoError(t, f.keeper.PutOperator(f.ctx, op))
	_, err := f.msgServer.WithdrawBond(f.ctx, &types.MsgWithdrawBond{
		Operator: testOperator1, ServiceType: testServiceType, Amount: math.NewInt(1),
	})
	require.ErrorIs(t, err, types.ErrOperatorUnbonding)

	op.Status = types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED
	op.UnderfundedSince = f.sdkCtx().BlockHeight()
	require.NoError(t, f.keeper.PutOperator(f.ctx, op))
	_, err = f.msgServer.WithdrawBond(f.ctx, &types.MsgWithdrawBond{
		Operator: testOperator1, ServiceType: testServiceType, Amount: math.NewInt(1),
	})
	require.ErrorIs(t, err, types.ErrOperatorNotActive)

	_, err = f.msgServer.WithdrawBond(f.ctx, &types.MsgWithdrawBond{
		Operator: testOperator2, ServiceType: testServiceType, Amount: math.NewInt(1),
	})
	require.ErrorIs(t, err, types.ErrOperatorNotFound)
}

func TestMsgClaimUnbondedBond_PartialWithdrawal(t *testing.T) {
	f := initFixture(t)
	f.withdrawFixture(t, math.NewInt(3_000_000))

	_, err := f.msgServer.WithdrawBond(f.ctx, &types.MsgWithdrawBond{
		Operator: testOperator1, ServiceType: testServiceType, Amount: math.NewInt(1_500_000),
	})
	require.NoError(t, err)
	completeAt := f.getOp(t).WithdrawCompleteAt

	// Too early.
	_, err = f.msgServer.ClaimUnbondedBond(f.ctx, &types.MsgClaimUnbondedBond{
		Operator: testOperator1, ServiceType: testServiceType,
	})
	require.ErrorIs(t, err, types.ErrUnbondingPeriodNotElapsed)

	f.withBlockHeight(completeAt)
	_, err = f.msgServer.ClaimUnbondedBond(f.ctx, &types.MsgClaimUnbondedBond{
		Operator: testOperator1, ServiceType: testServiceType,
	})
	require.NoError(t, err)

	op := f.getOp(t)
	require.Equal(t, types.OperatorStatus_OPERATOR_STATUS_ACTIVE, op.Status, "record stays live")
	require.True(t, op.BondAmount.Equal(math.NewInt(1_500_000)))
	require.True(t, op.PendingWithdrawAmount.IsZero())
	require.Equal(t, int64(0), op.WithdrawCompleteAt)

	require.Len(t, f.bankKeeper.ModToAcctCalls, 1)
	call := f.bankKeeper.ModToAcctCalls[0]
	require.Equal(t, testOperator1Addr, call.Recipient)
	require.True(t, call.Amt.AmountOf(testBondDenom).Equal(math.NewInt(1_500_000)))
	require.Len(t, f.hooks.Retired, 0, "partial claim is not an exit")

	// Nothing left to claim.
	_, err = f.msgServer.ClaimUnbondedBond(f.ctx, &types.MsgClaimUnbondedBond{
		Operator: testOperator1, ServiceType: testServiceType,
	})
	require.ErrorIs(t, err, types.ErrOperatorNotActive)
}

func TestMsgClaimUnbondedBond_PartialWithdrawalCappedBySlash(t *testing.T) {
	f := initFixture(t)
	f.withdrawFixture(t, math.NewInt(3_000_000))

	_, err := f.msgServer.WithdrawBond(f.ctx, &types.MsgWithdrawBond{
		Operator: testOperator1, ServiceType: testServiceType, Amount: math.NewInt(2_000_000),
	})
	require.NoError(t, err)

	// A slash during the wait drops the bond to 2_500_000: only 1_500_000
	// sits above min_bond now.
	op := f.getOp(t)
	op.BondAmount = math.NewInt(2_500_000)
	require.NoError(t, f.keeper.PutOperator(f.ctx, op))

	f.withBlockHeight(op.WithdrawCompleteAt)
	_, err = f.msgServer.ClaimUnbondedBond(f.ctx, &types.MsgClaimUnbondedBond{
		Operator: testOperator1, ServiceType: testServiceType,
	})
	require.NoError(t, err)

	op = f.getOp(t)
	require.True(t, op.BondAmount.Equal(math.NewInt(1_000_000)), "never below min_bond")
	require.True(t, op.PendingWithdrawAmount.Equal(math.NewInt(500_000)), "remainder stays queued")
	require.Equal(t, types.OperatorStatus_OPERATOR_STATUS_ACTIVE, op.Status)
	require.Positive(t, op.WithdrawCompleteAt)

	// With nothing spare, a further claim is refused until a top-up.
	_, err = f.msgServer.ClaimUnbondedBond(f.ctx, &types.MsgClaimUnbondedBond{
		Operator: testOperator1, ServiceType: testServiceType,
	})
	require.ErrorIs(t, err, types.ErrWithdrawExceedsAvailable)
}

func TestMsgClaimUnbondedBond_PartialWithdrawalOpenReportBlocks(t *testing.T) {
	f := initFixture(t)
	f.withdrawFixture(t, math.NewInt(3_000_000))
	_, err := f.msgServer.WithdrawBond(f.ctx, &types.MsgWithdrawBond{
		Operator: testOperator1, ServiceType: testServiceType, Amount: math.NewInt(1_000_000),
	})
	require.NoError(t, err)

	reportID, err := f.keeper.NextReportID.Next(f.ctx)
	require.NoError(t, err)
	require.NoError(t, f.keeper.Reports.Set(f.ctx, reportID, types.Report{
		ReportId: reportID, OperatorAddress: testOperator1, ServiceType: testServiceType,
		Reporter: testReporter, Status: types.ReportStatus_REPORT_STATUS_PENDING,
		SlashAmount: math.ZeroInt(), Deposit: math.NewInt(10_000_000),
	}))
	require.NoError(t, f.keeper.ReportsByOperator.Set(f.ctx, collections.Join3(testOperator1Addr.Bytes(), testServiceType, reportID)))

	f.withBlockHeight(f.getOp(t).WithdrawCompleteAt)
	_, err = f.msgServer.ClaimUnbondedBond(f.ctx, &types.MsgClaimUnbondedBond{
		Operator: testOperator1, ServiceType: testServiceType,
	})
	require.ErrorIs(t, err, types.ErrOpenReports)
}

func TestMsgUnbondOperator_ClearsPendingWithdrawal(t *testing.T) {
	f := initFixture(t)
	f.withdrawFixture(t, math.NewInt(3_000_000))
	_, err := f.msgServer.WithdrawBond(f.ctx, &types.MsgWithdrawBond{
		Operator: testOperator1, ServiceType: testServiceType, Amount: math.NewInt(1_000_000),
	})
	require.NoError(t, err)

	_, err = f.msgServer.UnbondOperator(f.ctx, &types.MsgUnbondOperator{
		Operator: testOperator1, ServiceType: testServiceType,
	})
	require.NoError(t, err)
	op := f.getOp(t)
	require.Equal(t, types.OperatorStatus_OPERATOR_STATUS_UNBONDING, op.Status)
	require.True(t, op.PendingWithdrawAmount.IsZero())
	require.Equal(t, int64(0), op.WithdrawCompleteAt)
	require.True(t, op.BondAmount.Equal(math.NewInt(3_000_000)), "whole bond returns via the exit claim")
}

func TestGenesisValidate_PendingWithdrawal(t *testing.T) {
	gs := types.DefaultGenesis()
	cfg := gs.ServiceTypes[0]
	base := types.Operator{
		Address: testOperator1, ServiceType: cfg.ServiceType, Controller: testController,
		BondAmount: math.NewInt(3_000_000), Status: types.OperatorStatus_OPERATOR_STATUS_ACTIVE,
		Tier1SlashedInWindow: math.ZeroInt(), Tier1WindowStartBond: math.NewInt(3_000_000),
		TotalLifetimeBondBlocks: math.ZeroInt(), RegisteredAt: 1,
	}

	ok := base
	ok.PendingWithdrawAmount = math.NewInt(1_000_000)
	ok.WithdrawCompleteAt = 50
	gs.Operators = []types.Operator{ok}
	require.NoError(t, gs.Validate())

	tooMuch := ok
	tooMuch.PendingWithdrawAmount = math.NewInt(3_000_001)
	gs.Operators = []types.Operator{tooMuch}
	require.ErrorContains(t, gs.Validate(), "exceeds bond")

	noClock := ok
	noClock.WithdrawCompleteAt = 0
	gs.Operators = []types.Operator{noClock}
	require.ErrorContains(t, gs.Validate(), "withdraw_complete_at")

	wrongStatus := ok
	wrongStatus.Status = types.OperatorStatus_OPERATOR_STATUS_UNBONDING
	wrongStatus.UnbondCompleteAt = 50
	gs.Operators = []types.Operator{wrongStatus}
	require.ErrorContains(t, gs.Validate(), "pending withdrawal but status")
}

// A slash that underfunds the operator mid-wait keeps the queue but blocks
// the claim until a top-up restores ACTIVE; the claim then releases what
// sits above min_bond.
func TestMsgClaimUnbondedBond_PartialWithdrawalUnderfundedThenTopUp(t *testing.T) {
	f := initFixture(t)
	f.withdrawFixture(t, math.NewInt(3_000_000))
	_, err := f.msgServer.WithdrawBond(f.ctx, &types.MsgWithdrawBond{
		Operator: testOperator1, ServiceType: testServiceType, Amount: math.NewInt(1_000_000),
	})
	require.NoError(t, err)

	// Simulate a slash below min_bond -> UNDERFUNDED (what applySlash does).
	op := f.getOp(t)
	op.BondAmount = math.NewInt(900_000)
	op.Status = types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED
	op.UnderfundedSince = f.sdkCtx().BlockHeight()
	require.NoError(t, f.keeper.PutOperator(f.ctx, op))
	f.withBlockHeight(op.WithdrawCompleteAt)

	_, err = f.msgServer.ClaimUnbondedBond(f.ctx, &types.MsgClaimUnbondedBond{
		Operator: testOperator1, ServiceType: testServiceType,
	})
	require.ErrorIs(t, err, types.ErrOperatorNotActive)
	require.True(t, f.getOp(t).PendingWithdrawAmount.Equal(math.NewInt(1_000_000)), "queue survives underfunding")

	// Top up back to ACTIVE with 2_000_000 total, then claim: 1_000_000 spare.
	_, err = f.msgServer.TopUpBond(f.ctx, &types.MsgTopUpBond{
		Operator: testOperator1, ServiceType: testServiceType, AdditionalBondAmount: math.NewInt(1_100_000),
	})
	require.NoError(t, err)
	require.Equal(t, types.OperatorStatus_OPERATOR_STATUS_ACTIVE, f.getOp(t).Status)

	_, err = f.msgServer.ClaimUnbondedBond(f.ctx, &types.MsgClaimUnbondedBond{
		Operator: testOperator1, ServiceType: testServiceType,
	})
	require.NoError(t, err)
	op = f.getOp(t)
	require.True(t, op.BondAmount.Equal(math.NewInt(1_000_000)))
	require.True(t, op.PendingWithdrawAmount.IsZero())
	require.Equal(t, types.OperatorStatus_OPERATOR_STATUS_ACTIVE, op.Status)
}

func TestEndBlocker_ForcedUnbondClearsPendingWithdrawal(t *testing.T) {
	f := initFixture(t)
	cfg := f.seedServiceType(t)
	height := f.sdkCtx().BlockHeight()
	op := f.seedActiveOperator(t, testOperator1, testController, math.NewInt(100_000))
	op.Status = types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED
	op.UnderfundedSince = height
	op.PendingWithdrawAmount = math.NewInt(50_000)
	op.WithdrawCompleteAt = height + 5
	require.NoError(t, f.keeper.PutOperator(f.ctx, op))

	f.withBlockHeight(height + cfg.UnderfundedGraceBlocks + 1)
	require.NoError(t, f.keeper.EndBlocker(f.ctx))

	post := f.getOp(t)
	require.Equal(t, types.OperatorStatus_OPERATOR_STATUS_UNBONDING, post.Status)
	require.True(t, post.PendingWithdrawAmount.IsZero())
	require.Equal(t, int64(0), post.WithdrawCompleteAt)
}

func TestTerminateOperator_ClearsPendingWithdrawal(t *testing.T) {
	f := initFixture(t)
	f.withdrawFixture(t, math.NewInt(3_000_000))
	_, err := f.msgServer.WithdrawBond(f.ctx, &types.MsgWithdrawBond{
		Operator: testOperator1, ServiceType: testServiceType, Amount: math.NewInt(1_000_000),
	})
	require.NoError(t, err)

	require.NoError(t, f.keeper.TerminateOperator(f.ctx, testOperator1Addr, testServiceType, 0))
	_, live := f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), testServiceType)
	require.False(t, live)
	var archived types.Operator
	found := false
	require.NoError(t, f.keeper.ArchivedOperators.Walk(f.ctx, nil,
		func(_ collections.Triple[[]byte, string, int64], op types.Operator) (bool, error) {
			if op.Address == testOperator1 && op.ServiceType == testServiceType {
				archived, found = op, true
				return true, nil
			}
			return false, nil
		}))
	require.True(t, found)
	require.Equal(t, types.OperatorStatus_OPERATOR_STATUS_SLASHED, archived.Status)
	require.True(t, archived.PendingWithdrawAmount.IsZero())
	require.Equal(t, int64(0), archived.WithdrawCompleteAt)
}

func TestPendingWithdrawalSanityInvariant(t *testing.T) {
	f := initFixture(t)
	f.withdrawFixture(t, math.NewInt(3_000_000))
	inv := keeper.PendingWithdrawalSanityInvariant(f.keeper)

	_, broken := inv(f.sdkCtx())
	require.False(t, broken, "fresh operator is clean")

	_, err := f.msgServer.WithdrawBond(f.ctx, &types.MsgWithdrawBond{
		Operator: testOperator1, ServiceType: testServiceType, Amount: math.NewInt(1_000_000),
	})
	require.NoError(t, err)
	_, broken = inv(f.sdkCtx())
	require.False(t, broken, "queued withdrawal is clean")

	// UNDERFUNDED with a queue is allowed.
	op := f.getOp(t)
	op.Status = types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED
	op.UnderfundedSince = f.sdkCtx().BlockHeight()
	require.NoError(t, f.keeper.PutOperator(f.ctx, op))
	_, broken = inv(f.sdkCtx())
	require.False(t, broken)

	// UNBONDING with a queue is not.
	op = f.getOp(t)
	op.Status = types.OperatorStatus_OPERATOR_STATUS_UNBONDING
	op.UnderfundedSince = 0
	op.UnbondCompleteAt = f.sdkCtx().BlockHeight() + 20
	require.NoError(t, f.keeper.PutOperator(f.ctx, op))
	msg, broken := inv(f.sdkCtx())
	require.True(t, broken, msg)

	// Queue larger than the bond is not.
	op.Status = types.OperatorStatus_OPERATOR_STATUS_ACTIVE
	op.UnbondCompleteAt = 0
	op.PendingWithdrawAmount = math.NewInt(4_000_000)
	require.NoError(t, f.keeper.PutOperator(f.ctx, op))
	_, broken = inv(f.sdkCtx())
	require.True(t, broken)

	// Clock without a queue is not.
	op.PendingWithdrawAmount = math.ZeroInt()
	op.WithdrawCompleteAt = 99
	require.NoError(t, f.keeper.PutOperator(f.ctx, op))
	_, broken = inv(f.sdkCtx())
	require.True(t, broken)
}

// A hand-written genesis without pending_withdraw_amount imports as zero
// rather than a nil Int.
func TestInitGenesis_NormalizesNilPendingWithdraw(t *testing.T) {
	f := initFixture(t)
	gs := types.DefaultGenesis()
	cfg := gs.ServiceTypes[0]
	gs.Operators = []types.Operator{{
		Address: testOperator1, ServiceType: cfg.ServiceType, Controller: testController,
		BondAmount: math.NewInt(3_000_000), Status: types.OperatorStatus_OPERATOR_STATUS_ACTIVE,
		Tier1SlashedInWindow: math.ZeroInt(), Tier1WindowStartBond: math.NewInt(3_000_000),
		TotalLifetimeBondBlocks: math.ZeroInt(), RegisteredAt: 1,
	}}
	require.NoError(t, gs.Validate())
	require.NoError(t, f.keeper.InitGenesis(f.ctx, *gs))

	op, ok := f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), cfg.ServiceType)
	require.True(t, ok)
	require.False(t, op.PendingWithdrawAmount.IsNil())
	require.True(t, op.PendingWithdrawAmount.IsZero())
}
