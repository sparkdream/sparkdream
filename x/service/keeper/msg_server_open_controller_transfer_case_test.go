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

func TestMsgOpenControllerTransferCase_HappyPath(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))

	// Allow testCouncil as the proposed new controller policy.
	f.commonsKeeper.IsGroupPolicyAddressFn = func(_ context.Context, addr string) bool {
		return addr == testController || addr == testCouncil
	}

	resp, err := f.msgServer.OpenControllerTransferCase(f.ctx, &types.MsgOpenControllerTransferCase{
		Opener:                testReporter,
		Operator:              testOperator1,
		ServiceType:           testServiceType,
		ProposedNewController: testCouncil,
		Reason:                "underperformance",
	})
	require.NoError(t, err)
	require.NotZero(t, resp.JuryCaseId)

	row, err := f.keeper.ControllerTransferCases.Get(f.ctx, resp.JuryCaseId)
	require.NoError(t, err)
	require.Equal(t, testOperator1, row.OperatorAddress)
	require.Equal(t, testCouncil, row.ProposedNewController)

	// Open-by-operator index seeded.
	id, err := f.keeper.OpenControllerTransferByOperator.Get(f.ctx, collections.Join(testOperator1Addr.Bytes(), testServiceType))
	require.NoError(t, err)
	require.Equal(t, resp.JuryCaseId, id)

	// Deposit escrowed.
	require.Len(t, f.bankKeeper.AcctToModCalls, 1)
}

func TestMsgOpenControllerTransferCase_OpenerIsOperatorSkipsTrustGate(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))

	// Force the trust-level check to deny; operator-as-opener must bypass.
	f.repKeeper.MeetsTrustLevelFn = func(_ context.Context, _ sdk.AccAddress, _ string) (bool, error) {
		t.Fatalf("MeetsTrustLevel should not be called when opener==operator")
		return false, nil
	}
	f.commonsKeeper.IsGroupPolicyAddressFn = func(_ context.Context, addr string) bool {
		return addr == testController || addr == testCouncil
	}

	_, err := f.msgServer.OpenControllerTransferCase(f.ctx, &types.MsgOpenControllerTransferCase{
		Opener:                testOperator1, // opener IS the operator
		Operator:              testOperator1,
		ServiceType:           testServiceType,
		ProposedNewController: testCouncil,
		Reason:                "rotate",
	})
	require.NoError(t, err)
}

func TestMsgOpenControllerTransferCase_TrustGateFails(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))
	f.commonsKeeper.IsGroupPolicyAddressFn = func(_ context.Context, addr string) bool {
		return addr == testController || addr == testCouncil
	}

	f.repKeeper.MeetsTrustLevelFn = func(_ context.Context, _ sdk.AccAddress, _ string) (bool, error) {
		return false, nil
	}

	_, err := f.msgServer.OpenControllerTransferCase(f.ctx, &types.MsgOpenControllerTransferCase{
		Opener:                testReporter,
		Operator:              testOperator1,
		ServiceType:           testServiceType,
		ProposedNewController: testCouncil,
		Reason:                "low-trust",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrReporterTrustLevelTooLow.Error())
}

func TestMsgOpenControllerTransferCase_SameControllerRejected(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))

	_, err := f.msgServer.OpenControllerTransferCase(f.ctx, &types.MsgOpenControllerTransferCase{
		Opener:                testReporter,
		Operator:              testOperator1,
		ServiceType:           testServiceType,
		ProposedNewController: testController,
		Reason:                "noop",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrInvalidVerdict.Error())
}

func TestMsgOpenControllerTransferCase_ProposedNotGroupRejected(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))
	// Proposed (testCouncil) is NOT a group policy in the default mock.

	_, err := f.msgServer.OpenControllerTransferCase(f.ctx, &types.MsgOpenControllerTransferCase{
		Opener:                testReporter,
		Operator:              testOperator1,
		ServiceType:           testServiceType,
		ProposedNewController: testCouncil,
		Reason:                "bad-target",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrControllerNotGroup.Error())
}

func TestMsgOpenControllerTransferCase_OneOpenPerOperator(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))
	f.commonsKeeper.IsGroupPolicyAddressFn = func(_ context.Context, addr string) bool {
		return addr == testController || addr == testCouncil
	}

	_, err := f.msgServer.OpenControllerTransferCase(f.ctx, &types.MsgOpenControllerTransferCase{
		Opener:                testReporter,
		Operator:              testOperator1,
		ServiceType:           testServiceType,
		ProposedNewController: testCouncil,
		Reason:                "first",
	})
	require.NoError(t, err)

	_, err = f.msgServer.OpenControllerTransferCase(f.ctx, &types.MsgOpenControllerTransferCase{
		Opener:                testReporter,
		Operator:              testOperator1,
		ServiceType:           testServiceType,
		ProposedNewController: testCouncil,
		Reason:                "second",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrControllerTransferCaseAlreadyOpen.Error())
}

func TestMsgOpenControllerTransferCase_OperatorNotFound(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.commonsKeeper.IsGroupPolicyAddressFn = func(_ context.Context, addr string) bool { return true }

	_, err := f.msgServer.OpenControllerTransferCase(f.ctx, &types.MsgOpenControllerTransferCase{
		Opener:                testReporter,
		Operator:              testOperator1,
		ServiceType:           testServiceType,
		ProposedNewController: testCouncil,
		Reason:                "missing-op",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrOperatorNotFound.Error())
}
