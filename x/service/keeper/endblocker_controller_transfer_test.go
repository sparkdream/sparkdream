package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/service/types"
)

// openPendingTransferCase opens a controller-transfer case whose x/rep
// jury never reaches a verdict (GetJuryVerdictName reports PENDING), the
// state a case with no jurors is stuck in.
func openPendingTransferCase(t *testing.T, f *fixture) uint64 {
	t.Helper()
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))
	f.commonsKeeper.IsGroupPolicyAddressFn = func(_ context.Context, addr string) bool {
		return addr == testController || addr == testCouncil
	}
	f.repKeeper.GetJuryVerdictNameFn = func(context.Context, uint64) (string, error) {
		return "PENDING", nil
	}
	resp, err := f.msgServer.OpenControllerTransferCase(f.ctx, &types.MsgOpenControllerTransferCase{
		Opener:                testReporter,
		Operator:              testOperator1,
		ServiceType:           testServiceType,
		ProposedNewController: testCouncil,
		Reason:                "controller unresponsive",
	})
	require.NoError(t, err)
	return resp.JuryCaseId
}

// TestEndBlocker_ControllerTransferCaseTimesOut: a case still open at
// opened_at + max_escalated_blocks is timed out — opener refunded, case
// row and index deleted, controller unchanged — and a new case can then
// be opened for the same (operator, service_type).
func TestEndBlocker_ControllerTransferCaseTimesOut(t *testing.T) {
	f := initFixture(t)
	caseID := openPendingTransferCase(t, f)
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	row, err := f.keeper.ControllerTransferCases.Get(f.ctx, caseID)
	require.NoError(t, err)

	// The PENDING jury blocks finalize.
	_, err = f.msgServer.FinalizeControllerTransfer(f.ctx, &types.MsgFinalizeControllerTransfer{
		JuryAuthority: testCouncil,
		JuryCaseId:    caseID,
		Verdict:       types.TransferVerdict_TRANSFER_VERDICT_REJECT,
	})
	require.ErrorIs(t, err, types.ErrJuryVerdictMismatch)

	// One block short of the deadline: untouched.
	f.withBlockHeight(row.OpenedAt + params.MaxEscalatedBlocks - 1)
	require.NoError(t, f.keeper.EndBlocker(f.ctx))
	_, err = f.keeper.ControllerTransferCases.Get(f.ctx, caseID)
	require.NoError(t, err)
	require.Empty(t, f.bankKeeper.ModToAcctCalls)

	// At the deadline: timed out.
	f.withBlockHeight(row.OpenedAt + params.MaxEscalatedBlocks)
	require.NoError(t, f.keeper.EndBlocker(f.ctx))

	_, err = f.keeper.ControllerTransferCases.Get(f.ctx, caseID)
	require.ErrorIs(t, err, collections.ErrNotFound)
	has, err := f.keeper.OpenControllerTransferByOperator.Has(f.ctx, collections.Join(testOperator1Addr.Bytes(), testServiceType))
	require.NoError(t, err)
	require.False(t, has)

	require.Len(t, f.bankKeeper.ModToAcctCalls, 1)
	refund := f.bankKeeper.ModToAcctCalls[0]
	require.Equal(t, testReporterAddr, refund.Recipient)
	require.True(t, refund.Amt.Equal(sdk.NewCoins(sdk.NewCoin(f.keeper.BondDenom(f.ctx), row.Deposit))))

	op, ok := f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), testServiceType)
	require.True(t, ok)
	require.Equal(t, testController, op.Controller)

	var sawTimeout bool
	for _, ev := range f.sdkCtx().EventManager().Events() {
		if ev.Type != types.EventTypeControllerTransferCaseFinalized {
			continue
		}
		for _, a := range ev.Attributes {
			if a.Key == types.AttrVerdict && a.Value == types.CtfVerdictAutoTimeout {
				sawTimeout = true
			}
		}
	}
	require.True(t, sawTimeout)

	// The slot is free again.
	_, err = f.msgServer.OpenControllerTransferCase(f.ctx, &types.MsgOpenControllerTransferCase{
		Opener:                testReporter,
		Operator:              testOperator1,
		ServiceType:           testServiceType,
		ProposedNewController: testCouncil,
		Reason:                "second attempt",
	})
	require.NoError(t, err)
}

// TestEndBlocker_ControllerTransferCaseSweepLimit: at most
// endblocker_sweep_limit cases time out per block; the rest drain next
// block. Rows are seeded directly, with the opened_at index genesis import
// rebuilds.
func TestEndBlocker_ControllerTransferCaseSweepLimit(t *testing.T) {
	f := initFixture(t)
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EndblockerSweepLimit = 2
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	height := f.sdkCtx().BlockHeight()
	for i := uint64(1); i <= 3; i++ {
		serviceType := testServiceType + string(rune('a'+i))
		require.NoError(t, f.keeper.ControllerTransferCases.Set(f.ctx, i, types.ControllerTransferCase{
			JuryCaseId:            i,
			OperatorAddress:       testOperator1,
			ServiceType:           serviceType,
			Opener:                testReporter,
			ProposedNewController: testCouncil,
			Deposit:               math.NewInt(1_000),
			OpenedAt:              height,
		}))
		require.NoError(t, f.keeper.OpenControllerTransferByOperator.Set(f.ctx,
			collections.Join(testOperator1Addr.Bytes(), serviceType), i))
		require.NoError(t, f.keeper.ControllerTransferCasesByOpenedAt.Set(f.ctx, collections.Join(height, i)))
	}

	f.withBlockHeight(height + params.MaxEscalatedBlocks)
	require.NoError(t, f.keeper.EndBlocker(f.ctx))
	require.Len(t, f.bankKeeper.ModToAcctCalls, 2)
	_, err = f.keeper.ControllerTransferCases.Get(f.ctx, 3)
	require.NoError(t, err)

	f.withBlockHeight(height + params.MaxEscalatedBlocks + 1)
	require.NoError(t, f.keeper.EndBlocker(f.ctx))
	require.Len(t, f.bankKeeper.ModToAcctCalls, 3)
	_, err = f.keeper.ControllerTransferCases.Get(f.ctx, 3)
	require.ErrorIs(t, err, collections.ErrNotFound)
}

// SVC-13: the sweep reads cases in opened_at order and stops at the first
// undue one. Undue cases with lower jury ids do not delay a due case, are
// left untouched, and genesis import rebuilds the index.
func TestEndBlocker_ControllerTransferSweepReadsOnlyDueCases(t *testing.T) {
	f := initFixture(t)
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	start := f.sdkCtx().BlockHeight()

	seed := func(id uint64, openedAt int64) types.ControllerTransferCase {
		c := types.ControllerTransferCase{
			JuryCaseId:            id,
			OperatorAddress:       testOperator1,
			ServiceType:           testServiceType + string(rune('a'+id)),
			Opener:                testReporter,
			ProposedNewController: testCouncil,
			Deposit:               math.NewInt(1_000),
			OpenedAt:              openedAt,
		}
		require.NoError(t, f.keeper.ControllerTransferCases.Set(f.ctx, id, c))
		require.NoError(t, f.keeper.OpenControllerTransferByOperator.Set(f.ctx,
			collections.Join(testOperator1Addr.Bytes(), c.ServiceType), id))
		return c
	}
	// Five undue cases with low ids, one due case with a high id.
	var cases []types.ControllerTransferCase
	for id := uint64(1); id <= 5; id++ {
		cases = append(cases, seed(id, start+100))
	}
	cases = append(cases, seed(9, start))

	// Rebuild the index the way genesis import does.
	gs, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	g := initFixture(t)
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *gs))
	for _, c := range cases {
		has, err := g.keeper.ControllerTransferCasesByOpenedAt.Has(g.ctx, collections.Join(c.OpenedAt, c.JuryCaseId))
		require.NoError(t, err)
		require.True(t, has, "case %d indexed on import", c.JuryCaseId)
	}

	g.withBlockHeight(start + params.MaxEscalatedBlocks)
	require.NoError(t, g.keeper.EndBlocker(g.ctx))
	_, err = g.keeper.ControllerTransferCases.Get(g.ctx, 9)
	require.ErrorIs(t, err, collections.ErrNotFound, "the due case timed out")
	for id := uint64(1); id <= 5; id++ {
		_, err = g.keeper.ControllerTransferCases.Get(g.ctx, id)
		require.NoError(t, err, "undue case %d untouched", id)
	}
	has, err := g.keeper.ControllerTransferCasesByOpenedAt.Has(g.ctx, collections.Join(start, uint64(9)))
	require.NoError(t, err)
	require.False(t, has, "index entry removed with the case")
}
