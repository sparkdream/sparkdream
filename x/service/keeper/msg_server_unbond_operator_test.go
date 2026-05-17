package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/service/types"
)

func TestMsgUnbondOperator_FromActive(t *testing.T) {
	f := initFixture(t)
	cfg := f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, cfg.MinBond.Amount)

	_, err := f.msgServer.UnbondOperator(f.ctx, &types.MsgUnbondOperator{
		Operator:    testOperator1,
		ServiceType: testServiceType,
	})
	require.NoError(t, err)

	op, ok := f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), testServiceType)
	require.True(t, ok)
	require.Equal(t, types.OperatorStatus_OPERATOR_STATUS_UNBONDING, op.Status)
	require.Equal(t, f.sdkCtx().BlockHeight()+cfg.UnbondingPeriodBlocks, op.UnbondCompleteAt)
}

func TestMsgUnbondOperator_FromUnderfundedClearsQueue(t *testing.T) {
	f := initFixture(t)
	cfg := f.seedServiceType(t)
	height := f.sdkCtx().BlockHeight()
	op := types.Operator{
		Address:                 testOperator1,
		ServiceType:             testServiceType,
		Controller:              testController,
		Bond:                    cfg.MinBond,
		Status:                  types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED,
		UnderfundedSince:        height,
		Tier1SlashedInWindow:    math.ZeroInt(),
		Tier1WindowStart:        height,
		Tier1WindowStartBond:    cfg.MinBond.Amount,
		RegisteredAt:            height,
		TotalLifetimeBondBlocks: math.ZeroInt(),
		LastBondBlockUpdateAt:   height,
	}
	require.NoError(t, f.keeper.PutOperator(f.ctx, op))

	_, err := f.msgServer.UnbondOperator(f.ctx, &types.MsgUnbondOperator{
		Operator:    testOperator1,
		ServiceType: testServiceType,
	})
	require.NoError(t, err)

	post, ok := f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), testServiceType)
	require.True(t, ok)
	require.Equal(t, types.OperatorStatus_OPERATOR_STATUS_UNBONDING, post.Status)
	require.EqualValues(t, 0, post.UnderfundedSince)
}

func TestMsgUnbondOperator_DoubleUnbondRejected(t *testing.T) {
	f := initFixture(t)
	cfg := f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, cfg.MinBond.Amount)

	_, err := f.msgServer.UnbondOperator(f.ctx, &types.MsgUnbondOperator{
		Operator:    testOperator1,
		ServiceType: testServiceType,
	})
	require.NoError(t, err)

	_, err = f.msgServer.UnbondOperator(f.ctx, &types.MsgUnbondOperator{
		Operator:    testOperator1,
		ServiceType: testServiceType,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrOperatorUnbonding.Error())
}

func TestMsgUnbondOperator_NotFound(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)

	_, err := f.msgServer.UnbondOperator(f.ctx, &types.MsgUnbondOperator{
		Operator:    testOperator1,
		ServiceType: testServiceType,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrOperatorNotFound.Error())
}

func TestMsgUnbondOperator_InvalidSigner(t *testing.T) {
	f := initFixture(t)

	_, err := f.msgServer.UnbondOperator(f.ctx, &types.MsgUnbondOperator{
		Operator:    "not-bech32",
		ServiceType: testServiceType,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrInvalidSigner.Error())
}
