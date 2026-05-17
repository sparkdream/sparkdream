package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/service/types"
)

// seedOpenTransferCase drives a fresh OpenControllerTransferCase msg
// through the msg-server, returning the case id.
func (f *fixture) seedOpenTransferCase(t *testing.T) uint64 {
	t.Helper()
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))
	f.commonsKeeper.IsGroupPolicyAddressFn = func(_ context.Context, addr string) bool {
		return addr == testController || addr == testCouncil
	}
	resp, err := f.msgServer.OpenControllerTransferCase(f.ctx, &types.MsgOpenControllerTransferCase{
		Opener:                testReporter,
		Operator:              testOperator1,
		ServiceType:           testServiceType,
		ProposedNewController: testCouncil,
		Reason:                "seed",
	})
	require.NoError(t, err)
	return resp.JuryCaseId
}

func TestMsgFinalizeControllerTransfer_AcceptSwapsController(t *testing.T) {
	f := initFixture(t)
	caseID := f.seedOpenTransferCase(t)

	_, err := f.msgServer.FinalizeControllerTransfer(f.ctx, &types.MsgFinalizeControllerTransfer{
		JuryAuthority: testCouncil,
		JuryCaseId:    caseID,
		Verdict:       types.TransferVerdict_TRANSFER_VERDICT_ACCEPT,
		NewController: testCouncil,
	})
	require.NoError(t, err)

	op, ok := f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), testServiceType)
	require.True(t, ok)
	require.Equal(t, testCouncil, op.Controller)

	// Case row removed.
	_, err = f.keeper.ControllerTransferCases.Get(f.ctx, caseID)
	require.Error(t, err)
	// Open-by-operator index cleared.
	_, err = f.keeper.OpenControllerTransferByOperator.Get(f.ctx, collections.Join(testOperator1Addr.Bytes(), testServiceType))
	require.Error(t, err)

	// Opener deposit refunded.
	require.Len(t, f.bankKeeper.ModToAcctCalls, 1)
	require.Equal(t, testReporterAddr, f.bankKeeper.ModToAcctCalls[0].Recipient)
}

func TestMsgFinalizeControllerTransfer_RejectForfeitsDeposit(t *testing.T) {
	f := initFixture(t)
	caseID := f.seedOpenTransferCase(t)

	// Jury voted REJECT.
	f.repKeeper.GetJuryVerdictNameFn = func(_ context.Context, _ uint64) (string, error) {
		return types.JuryVerdictRejectChallenge, nil
	}

	_, err := f.msgServer.FinalizeControllerTransfer(f.ctx, &types.MsgFinalizeControllerTransfer{
		JuryAuthority: testCouncil,
		JuryCaseId:    caseID,
		Verdict:       types.TransferVerdict_TRANSFER_VERDICT_REJECT,
	})
	require.NoError(t, err)

	op, _ := f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), testServiceType)
	require.Equal(t, testController, op.Controller, "controller unchanged on REJECT")

	// Deposit forfeited to community pool.
	require.NotEmpty(t, f.distributionKeeper.Calls)
}

func TestMsgFinalizeControllerTransfer_AcceptRejectsOnNewControllerMismatch(t *testing.T) {
	f := initFixture(t)
	caseID := f.seedOpenTransferCase(t)

	_, err := f.msgServer.FinalizeControllerTransfer(f.ctx, &types.MsgFinalizeControllerTransfer{
		JuryAuthority: testCouncil,
		JuryCaseId:    caseID,
		Verdict:       types.TransferVerdict_TRANSFER_VERDICT_ACCEPT,
		NewController: testRandom, // not what the case opened with
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrInvalidVerdict.Error())
}

func TestMsgFinalizeControllerTransfer_AcceptFallsThroughOnEmptyGroup(t *testing.T) {
	f := initFixture(t)
	caseID := f.seedOpenTransferCase(t)

	// At apply time the proposed group has zero members → ACCEPT falls
	// through to REJECT.
	f.commonsKeeper.GroupPolicyMemberCountFn = func(_ context.Context, _ string) (uint64, error) {
		return 0, nil
	}

	_, err := f.msgServer.FinalizeControllerTransfer(f.ctx, &types.MsgFinalizeControllerTransfer{
		JuryAuthority: testCouncil,
		JuryCaseId:    caseID,
		Verdict:       types.TransferVerdict_TRANSFER_VERDICT_ACCEPT,
		NewController: testCouncil,
	})
	require.NoError(t, err)

	op, _ := f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), testServiceType)
	require.Equal(t, testController, op.Controller, "controller stays on fall-through REJECT")
	require.NotEmpty(t, f.distributionKeeper.Calls)
}

func TestMsgFinalizeControllerTransfer_UnauthorizedResolver(t *testing.T) {
	f := initFixture(t)
	caseID := f.seedOpenTransferCase(t)

	_, err := f.msgServer.FinalizeControllerTransfer(f.ctx, &types.MsgFinalizeControllerTransfer{
		JuryAuthority: testRandom,
		JuryCaseId:    caseID,
		Verdict:       types.TransferVerdict_TRANSFER_VERDICT_ACCEPT,
		NewController: testCouncil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrUnauthorizedCouncilResolver.Error())
}

func TestMsgFinalizeControllerTransfer_CaseNotFound(t *testing.T) {
	f := initFixture(t)

	_, err := f.msgServer.FinalizeControllerTransfer(f.ctx, &types.MsgFinalizeControllerTransfer{
		JuryAuthority: testCouncil,
		JuryCaseId:    99999,
		Verdict:       types.TransferVerdict_TRANSFER_VERDICT_ACCEPT,
		NewController: testCouncil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrControllerTransferCaseNotFound.Error())
}

func TestMsgFinalizeControllerTransfer_VerdictMismatch(t *testing.T) {
	f := initFixture(t)
	caseID := f.seedOpenTransferCase(t)

	// Jury voted REJECT; resolver claims ACCEPT.
	f.repKeeper.GetJuryVerdictNameFn = func(_ context.Context, _ uint64) (string, error) {
		return types.JuryVerdictRejectChallenge, nil
	}

	_, err := f.msgServer.FinalizeControllerTransfer(f.ctx, &types.MsgFinalizeControllerTransfer{
		JuryAuthority: testCouncil,
		JuryCaseId:    caseID,
		Verdict:       types.TransferVerdict_TRANSFER_VERDICT_ACCEPT,
		NewController: testCouncil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrJuryVerdictMismatch.Error())
}
