package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/service/types"
)

func TestMsgRegisterOperator_HappyPath(t *testing.T) {
	f := initFixture(t)
	cfg := f.seedServiceType(t)

	bond := cfg.MinBondAmount

	_, err := f.msgServer.RegisterOperator(f.ctx, &types.MsgRegisterOperator{
		Creator:     testOperator1,
		ServiceType: testServiceType,
		Controller:  testController,
		BondAmount:        bond,
		Metadata:    []byte("operator1-metadata"),
	})
	require.NoError(t, err)

	op, ok := f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), testServiceType)
	require.True(t, ok)
	require.Equal(t, types.OperatorStatus_OPERATOR_STATUS_ACTIVE, op.Status)
	require.Equal(t, bond, op.BondAmount)
	require.Equal(t, testController, op.Controller)
	require.Equal(t, []byte("operator1-metadata"), op.Metadata)
	require.Equal(t, f.sdkCtx().BlockHeight(), op.RegisteredAt)

	// Bond was escrowed via bank.
	require.Len(t, f.bankKeeper.AcctToModCalls, 1)
	require.Equal(t, testOperator1Addr, f.bankKeeper.AcctToModCalls[0].Sender)
	require.Equal(t, types.ModuleName, f.bankKeeper.AcctToModCalls[0].Module)
	require.True(t, f.bankKeeper.AcctToModCalls[0].Amt.AmountOf(testBondDenom).Equal(bond))
}

func TestMsgRegisterOperator_Rejections(t *testing.T) {
	cfgBond := math.NewInt(1_000_000)
	belowMin := cfgBond.SubRaw(1)

	cases := []struct {
		name        string
		mutate      func(f *fixture, msg *types.MsgRegisterOperator)
		expErr      *errorKindMatch
		dontSeedSvc bool
	}{
		{
			name: "invalid creator address",
			mutate: func(_ *fixture, msg *types.MsgRegisterOperator) {
				msg.Creator = "not-a-bech32"
			},
			expErr: matchKind(types.ErrInvalidSigner),
		},
		{
			name: "invalid controller address",
			mutate: func(_ *fixture, msg *types.MsgRegisterOperator) {
				msg.Controller = "not-a-bech32"
			},
			expErr: matchKind(types.ErrControllerNotGroup),
		},
		{
			name: "self-controller",
			mutate: func(_ *fixture, msg *types.MsgRegisterOperator) {
				msg.Controller = msg.Creator
			},
			expErr: matchKind(types.ErrSelfController),
		},
		{
			name: "controller not a group policy",
			mutate: func(_ *fixture, msg *types.MsgRegisterOperator) {
				msg.Controller = testRandom // mock returns false for non-testController
			},
			expErr: matchKind(types.ErrControllerNotGroup),
		},
		{
			name: "unknown service type",
			mutate: func(_ *fixture, msg *types.MsgRegisterOperator) {
				msg.ServiceType = "missing"
			},
			expErr:      matchKind(types.ErrServiceTypeNotFound),
			dontSeedSvc: false, // seeded, but msg targets unknown one
		},
		{
			name: "service type disabled",
			mutate: func(f *fixture, _ *types.MsgRegisterOperator) {
				cfg, _ := f.keeper.ServiceTypes.Get(f.ctx, testServiceType)
				cfg.Enabled = false
				_ = f.keeper.ServiceTypes.Set(f.ctx, testServiceType, cfg)
			},
			expErr: matchKind(types.ErrServiceTypeDisabled),
		},
		{
			name: "bond below min",
			mutate: func(_ *fixture, msg *types.MsgRegisterOperator) {
				msg.BondAmount = belowMin
			},
			expErr: matchKind(types.ErrInsufficientBond),
		},
		{
			name: "metadata too large",
			mutate: func(_ *fixture, msg *types.MsgRegisterOperator) {
				msg.Metadata = make([]byte, types.DefaultMaxMetadataBytes+1)
			},
			expErr: matchKind(types.ErrInvalidMetadataSize),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := initFixture(t)
			f.seedServiceType(t)
			msg := &types.MsgRegisterOperator{
				Creator:     testOperator1,
				ServiceType: testServiceType,
				Controller:  testController,
				BondAmount:        cfgBond,
				Metadata:    []byte("ok"),
			}
			tc.mutate(f, msg)
			_, err := f.msgServer.RegisterOperator(f.ctx, msg)
			tc.expErr.requireMatch(t, err)
		})
	}
}

func TestMsgRegisterOperator_DuplicateRejected(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))

	_, err := f.msgServer.RegisterOperator(f.ctx, &types.MsgRegisterOperator{
		Creator:     testOperator1,
		ServiceType: testServiceType,
		Controller:  testController,
		BondAmount:        math.NewInt(1_000_000),
		Metadata:    []byte("dup"),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrOperatorAlreadyExists.Error())
}

func TestMsgRegisterOperator_PreviouslySlashedRejected(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)

	// Seed an archived SLASHED record for (operator1, testServiceType).
	op := types.Operator{
		Address:                 testOperator1,
		ServiceType:             testServiceType,
		Controller:              testController,
		BondAmount:                    math.ZeroInt(),
		Status:                  types.OperatorStatus_OPERATOR_STATUS_SLASHED,
		RetiredAt:               f.sdkCtx().BlockHeight(),
		Tier1SlashedInWindow:    math.ZeroInt(),
		TotalLifetimeBondBlocks: math.ZeroInt(),
		Tier1WindowStartBond:    math.ZeroInt(),
	}
	require.NoError(t, f.keeper.ArchiveOperator(f.ctx, op))

	_, err := f.msgServer.RegisterOperator(f.ctx, &types.MsgRegisterOperator{
		Creator:     testOperator1,
		ServiceType: testServiceType,
		Controller:  testController,
		BondAmount:        math.NewInt(1_000_000),
		Metadata:    []byte("retry"),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrOperatorPreviouslySlashed.Error())
}

// ---------------------------------------------------------------------------
// Shared error-match helper (used by table-driven tests in this package).
// ---------------------------------------------------------------------------

type errorKindMatch struct {
	kind error // sentinel from types/errors.go
}

func matchKind(e error) *errorKindMatch { return &errorKindMatch{kind: e} }

func (m *errorKindMatch) requireMatch(t *testing.T, got error) {
	t.Helper()
	require.Error(t, got, "expected error %v, got nil", m.kind)
	require.Contains(t, got.Error(), m.kind.Error(), "error %q does not contain sentinel %q", got, m.kind)
}
