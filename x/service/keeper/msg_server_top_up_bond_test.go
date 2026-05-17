package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/service/types"
)

func TestMsgTopUpBond_HappyPath(t *testing.T) {
	f := initFixture(t)
	cfg := f.seedServiceType(t)
	startBond := cfg.MinBond.Amount
	f.seedActiveOperator(t, testOperator1, testController, startBond)

	additional := math.NewInt(500_000)
	_, err := f.msgServer.TopUpBond(f.ctx, &types.MsgTopUpBond{
		Operator:       testOperator1,
		ServiceType:    testServiceType,
		AdditionalBond: sdk.NewCoin(types.BondDenom, additional),
	})
	require.NoError(t, err)

	op, ok := f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), testServiceType)
	require.True(t, ok)
	require.True(t, op.Bond.Amount.Equal(startBond.Add(additional)))
	// Bank: bond escrow was extended.
	require.Len(t, f.bankKeeper.AcctToModCalls, 1)
}

func TestMsgTopUpBond_UnderfundedReturnsActive(t *testing.T) {
	f := initFixture(t)
	cfg := f.seedServiceType(t)

	// Seed UNDERFUNDED at half the min_bond.
	height := f.sdkCtx().BlockHeight()
	startBond := cfg.MinBond.Amount.QuoRaw(2)
	op := types.Operator{
		Address:                 testOperator1,
		ServiceType:             testServiceType,
		Controller:              testController,
		Bond:                    sdk.NewCoin(types.BondDenom, startBond),
		Status:                  types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED,
		UnderfundedSince:        height,
		Tier1SlashedInWindow:    math.ZeroInt(),
		Tier1WindowStart:        height,
		Tier1WindowStartBond:    startBond,
		RegisteredAt:            height,
		TotalLifetimeBondBlocks: math.ZeroInt(),
		LastBondBlockUpdateAt:   height,
	}
	require.NoError(t, f.keeper.PutOperator(f.ctx, op))
	// Confirm the UnderfundedQueue entry is in place.
	has, hasErr := f.keeper.UnderfundedQueue.Has(f.ctx, collections.Join3(height, testOperator1Addr.Bytes(), testServiceType))
	require.NoError(t, hasErr)
	require.True(t, has)

	// Top up enough to clear min_bond.
	topup := cfg.MinBond.Amount // pushes bond > min_bond
	_, err := f.msgServer.TopUpBond(f.ctx, &types.MsgTopUpBond{
		Operator:       testOperator1,
		ServiceType:    testServiceType,
		AdditionalBond: sdk.NewCoin(types.BondDenom, topup),
	})
	require.NoError(t, err)

	post, ok := f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), testServiceType)
	require.True(t, ok)
	require.Equal(t, types.OperatorStatus_OPERATOR_STATUS_ACTIVE, post.Status)
	require.EqualValues(t, 0, post.UnderfundedSince)
	// UnderfundedQueue entry has been pulled.
	has, _ = f.keeper.UnderfundedQueue.Has(f.ctx, collections.Join3(height, testOperator1Addr.Bytes(), testServiceType))
	require.False(t, has)
}

func TestMsgTopUpBond_Rejections(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(f *fixture)
		msg    *types.MsgTopUpBond
		expErr error
	}{
		{
			name:  "operator not found",
			setup: func(f *fixture) { f.seedServiceType(t) },
			msg: &types.MsgTopUpBond{
				Operator:       testOperator1,
				ServiceType:    testServiceType,
				AdditionalBond: sdk.NewCoin(types.BondDenom, math.NewInt(1)),
			},
			expErr: types.ErrOperatorNotFound,
		},
		{
			name: "operator is unbonding",
			setup: func(f *fixture) {
				f.seedServiceType(t)
				op := f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))
				op.Status = types.OperatorStatus_OPERATOR_STATUS_UNBONDING
				op.UnbondCompleteAt = f.sdkCtx().BlockHeight() + 10
				require.NoError(t, f.keeper.PutOperator(f.ctx, op))
			},
			msg: &types.MsgTopUpBond{
				Operator:       testOperator1,
				ServiceType:    testServiceType,
				AdditionalBond: sdk.NewCoin(types.BondDenom, math.NewInt(1)),
			},
			expErr: types.ErrOperatorUnbonding,
		},
		{
			name: "bond denom mismatch",
			setup: func(f *fixture) {
				f.seedServiceType(t)
				f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))
			},
			msg: &types.MsgTopUpBond{
				Operator:       testOperator1,
				ServiceType:    testServiceType,
				AdditionalBond: sdk.NewCoin("udream", math.NewInt(1)),
			},
			expErr: types.ErrBondDenomMismatch,
		},
		{
			name: "invalid signer",
			setup: func(f *fixture) {
				f.seedServiceType(t)
			},
			msg: &types.MsgTopUpBond{
				Operator:       "not-bech32",
				ServiceType:    testServiceType,
				AdditionalBond: sdk.NewCoin(types.BondDenom, math.NewInt(1)),
			},
			expErr: types.ErrInvalidSigner,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := initFixture(t)
			tc.setup(f)
			_, err := f.msgServer.TopUpBond(f.ctx, tc.msg)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.expErr.Error())
		})
	}
}
