package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/service/types"
)

func TestMsgTopUpBond_HappyPath(t *testing.T) {
	f := initFixture(t)
	cfg := f.seedServiceType(t)
	startBond := cfg.MinBondAmount
	f.seedActiveOperator(t, testOperator1, testController, startBond)

	additional := math.NewInt(500_000)
	_, err := f.msgServer.TopUpBond(f.ctx, &types.MsgTopUpBond{
		Operator:             testOperator1,
		ServiceType:          testServiceType,
		AdditionalBondAmount: additional,
	})
	require.NoError(t, err)

	op, ok := f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), testServiceType)
	require.True(t, ok)
	require.True(t, op.BondAmount.Equal(startBond.Add(additional)))
	// Bank: bond escrow was extended.
	require.Len(t, f.bankKeeper.AcctToModCalls, 1)
}

func TestMsgTopUpBond_UnderfundedReturnsActive(t *testing.T) {
	f := initFixture(t)
	cfg := f.seedServiceType(t)

	// Seed UNDERFUNDED at half the min_bond.
	height := f.sdkCtx().BlockHeight()
	startBond := cfg.MinBondAmount.QuoRaw(2)
	op := types.Operator{
		Address:                 testOperator1,
		ServiceType:             testServiceType,
		Controller:              testController,
		BondAmount:              startBond,
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
	topup := cfg.MinBondAmount // pushes bond > min_bond
	_, err := f.msgServer.TopUpBond(f.ctx, &types.MsgTopUpBond{
		Operator:             testOperator1,
		ServiceType:          testServiceType,
		AdditionalBondAmount: topup,
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
				Operator:             testOperator1,
				ServiceType:          testServiceType,
				AdditionalBondAmount: math.NewInt(1),
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
				Operator:             testOperator1,
				ServiceType:          testServiceType,
				AdditionalBondAmount: math.NewInt(1),
			},
			expErr: types.ErrOperatorUnbonding,
		},
		{
			name: "invalid signer",
			setup: func(f *fixture) {
				f.seedServiceType(t)
			},
			msg: &types.MsgTopUpBond{
				Operator:             "not-bech32",
				ServiceType:          testServiceType,
				AdditionalBondAmount: math.NewInt(1),
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

// ---------------------------------------------------------------------------
// TopUpBond keeper-level public API: AfterOperatorReFunded hook fires
// on UNDERFUNDED → ACTIVE transition. Exercised against the Keeper.TopUpBond
// path (federation→service migration Phase 0).
// ---------------------------------------------------------------------------

func TestTopUpBond_FiresAfterOperatorReFundedOnTransition(t *testing.T) {
	f := initFixture(t)
	hooks := f.hooks

	cfg := f.seedServiceType(t)

	// Seed UNDERFUNDED at half the min_bond.
	height := f.sdkCtx().BlockHeight()
	startBond := cfg.MinBondAmount.QuoRaw(2)
	op := types.Operator{
		Address:                 testOperator1,
		ServiceType:             testServiceType,
		Controller:              testController,
		BondAmount:              startBond,
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

	// Top up enough to clear min_bond — should trigger ACTIVE transition
	// AND fire the AfterOperatorReFunded hook.
	require.NoError(t, f.keeper.TopUpBond(f.ctx, testOperator1Addr.Bytes(), testServiceType,
		cfg.MinBondAmount))

	require.Len(t, hooks.ReFunded, 1)
	require.True(t, hooks.ReFunded[0].Operator.Equals(testOperator1Addr))
	require.Equal(t, testServiceType, hooks.ReFunded[0].ServiceType)
	require.Empty(t, hooks.Underfunded, "TopUpBond must not fire AfterOperatorUnderfunded")
}

func TestTopUpBond_NoHookWhenAlreadyActive(t *testing.T) {
	f := initFixture(t)
	hooks := f.hooks

	cfg := f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, cfg.MinBondAmount.MulRaw(2))

	require.NoError(t, f.keeper.TopUpBond(f.ctx, testOperator1Addr.Bytes(), testServiceType,
		math.NewInt(100)))

	require.Empty(t, hooks.ReFunded, "no transition, no hook fire")
}

// SVC-12: gov raises min_bond above an operator's bond. The operator stops
// reading as eligible at once; its next message moves it to UNDERFUNDED
// with a grace deadline and the hook; after the grace period the sweep
// forces it to UNBONDING.
func TestRaisedMinBondUnderfundsOnNextMessage(t *testing.T) {
	f := initFixture(t)
	cfg := f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, cfg.MinBondAmount)
	require.True(t, f.keeper.IsActiveOperator(f.ctx, testOperator1Addr, testServiceType))

	cfg.MinBondAmount = cfg.MinBondAmount.MulRaw(10)
	require.NoError(t, f.keeper.ServiceTypes.Set(f.ctx, testServiceType, cfg))
	require.False(t, f.keeper.IsActiveOperator(f.ctx, testOperator1Addr, testServiceType),
		"an untouched operator below the new minimum is not eligible")

	// A top-up that stays below the new minimum is the next message.
	_, err := f.msgServer.TopUpBond(f.ctx, &types.MsgTopUpBond{
		Operator:             testOperator1,
		ServiceType:          testServiceType,
		AdditionalBondAmount: math.NewInt(500_000),
	})
	require.NoError(t, err)
	op, ok := f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), testServiceType)
	require.True(t, ok)
	require.Equal(t, types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED, op.Status)
	require.Equal(t, f.sdkCtx().BlockHeight(), op.UnderfundedSince)
	has, err := f.keeper.UnderfundedQueue.Has(f.ctx, collections.Join3(op.UnderfundedSince, testOperator1Addr.Bytes(), testServiceType))
	require.NoError(t, err)
	require.True(t, has)
	require.Len(t, f.hooks.Underfunded, 1)

	// Past the grace period the operator is forced out.
	f.withBlockHeight(op.UnderfundedSince + cfg.UnderfundedGraceBlocks + 1)
	require.NoError(t, f.keeper.EndBlocker(f.ctx))
	op, ok = f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), testServiceType)
	require.True(t, ok)
	require.Equal(t, types.OperatorStatus_OPERATOR_STATUS_UNBONDING, op.Status)
}

// SVC-12: a top-up that clears the raised minimum leaves the operator
// ACTIVE, with no underfunded round-trip.
func TestRaisedMinBondTopUpToNewMinimumStaysActive(t *testing.T) {
	f := initFixture(t)
	cfg := f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, cfg.MinBondAmount)
	cfg.MinBondAmount = cfg.MinBondAmount.MulRaw(2)
	require.NoError(t, f.keeper.ServiceTypes.Set(f.ctx, testServiceType, cfg))

	_, err := f.msgServer.TopUpBond(f.ctx, &types.MsgTopUpBond{
		Operator:             testOperator1,
		ServiceType:          testServiceType,
		AdditionalBondAmount: cfg.MinBondAmount,
	})
	require.NoError(t, err)
	op, ok := f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), testServiceType)
	require.True(t, ok)
	require.Equal(t, types.OperatorStatus_OPERATOR_STATUS_ACTIVE, op.Status)
	require.Empty(t, f.hooks.Underfunded)
	require.True(t, f.keeper.IsActiveOperator(f.ctx, testOperator1Addr, testServiceType))
}
