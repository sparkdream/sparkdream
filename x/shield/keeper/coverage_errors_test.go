package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	any "github.com/cosmos/gogoproto/types/any"
	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/keeper"
	"sparkdream/x/shield/types"
)

// TestAuthorityHandlersRejectNonGovSigner: a well-formed address that is not
// the gov authority is refused with ErrInvalidSigner by every authority
// handler, and nothing changes.
func TestAuthorityHandlersRejectNonGovSigner(t *testing.T) {
	f, ms := initMsgServer(t)
	phoenix, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("phoenix"))
	require.NoError(t, err)

	before, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	changed := before
	changed.MaxGasPerExec = before.MaxGasPerExec + 1
	_, err = ms.UpdateParams(f.ctx, &types.MsgUpdateParams{Authority: phoenix, Params: changed})
	require.ErrorIs(t, err, types.ErrInvalidSigner)
	after, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, before, after)

	reg := types.ShieldedOpRegistration{
		MessageTypeUrl:     "/sparkdream.aurora.v1.MsgZenith",
		ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
		NullifierDomain:    77,
		NullifierScopeType: types.NullifierScopeType_NULLIFIER_SCOPE_GLOBAL,
		Active:             true,
		BatchMode:          types.ShieldBatchMode_SHIELD_BATCH_MODE_EITHER,
	}
	_, err = ms.RegisterShieldedOp(f.ctx, &types.MsgRegisterShieldedOp{Authority: phoenix, Registration: reg})
	require.ErrorIs(t, err, types.ErrInvalidSigner)
	_, found := f.keeper.GetShieldedOp(f.ctx, reg.MessageTypeUrl)
	require.False(t, found)

	require.NoError(t, f.keeper.SetShieldedOp(f.ctx, reg))
	_, err = ms.DeregisterShieldedOp(f.ctx, &types.MsgDeregisterShieldedOp{Authority: phoenix, MessageTypeUrl: reg.MessageTypeUrl})
	require.ErrorIs(t, err, types.ErrInvalidSigner)
	_, found = f.keeper.GetShieldedOp(f.ctx, reg.MessageTypeUrl)
	require.True(t, found)

	_, err = ms.TriggerDKG(f.ctx, &types.MsgTriggerDkg{Authority: phoenix})
	require.ErrorIs(t, err, types.ErrInvalidSigner)
	_, found = f.keeper.GetDKGStateVal(f.ctx)
	require.False(t, found)
}

// TestTriggerDKGInsufficientValidators: fewer bonded validators than
// min_tle_validators, or no staking keeper wired, refuses the trigger.
func TestTriggerDKGInsufficientValidators(t *testing.T) {
	t.Run("below min_tle_validators", func(t *testing.T) {
		f, ms := initMsgServer(t)
		authority, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("gov"))
		require.NoError(t, err)
		params, err := f.keeper.Params.Get(f.ctx)
		require.NoError(t, err)
		params.MinTleValidators = 3
		require.NoError(t, f.keeper.Params.Set(f.ctx, params))
		f.keeper.SetStakingKeeper(mockStakingKeeper{validators: []stakingtypes.Validator{
			{OperatorAddress: "sprkdrmvaloper1aaaaa"},
			{OperatorAddress: "sprkdrmvaloper1bbbbb"},
		}})

		_, err = ms.TriggerDKG(f.ctx, &types.MsgTriggerDkg{Authority: authority})
		require.ErrorIs(t, err, types.ErrInsufficientValidators)
		_, found := f.keeper.GetDKGStateVal(f.ctx)
		require.False(t, found, "no round opened")
	})
	t.Run("staking keeper not wired", func(t *testing.T) {
		f := initFixtureEmpty(t)
		require.NoError(t, f.keeper.InitGenesis(f.ctx, *types.DefaultGenesis()))
		ms := keeper.NewMsgServerImpl(f.keeper)
		authority, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("gov"))
		require.NoError(t, err)

		_, err = ms.TriggerDKG(f.ctx, &types.MsgTriggerDkg{Authority: authority})
		require.ErrorIs(t, err, types.ErrInsufficientValidators)
	})
}

// TestExecuteInnerMessageRejections covers the gates executeInnerMessage
// applies before dispatch.
func TestExecuteInnerMessageRejections(t *testing.T) {
	shieldSigned := func(t *testing.T) *any.Any {
		return innerAny(t, &types.MsgTriggerDkg{Authority: authtypes.NewModuleAddress(types.ModuleName).String()})
	}
	cases := []struct {
		name   string
		setup  func(*fixture)
		inner  func(*testing.T) *any.Any
		wantIs error
	}{
		{
			name:   "no router wired",
			setup:  func(*fixture) {},
			inner:  shieldSigned,
			wantIs: types.ErrInvalidInnerMessage,
		},
		{
			name:  "undecodable inner message",
			setup: func(f *fixture) { f.keeper.SetRouter(eventRouter{}) },
			inner: func(*testing.T) *any.Any {
				return &any.Any{TypeUrl: "/sparkdream.aurora.v1.MsgUnknown", Value: []byte{1}}
			},
			wantIs: types.ErrInvalidInnerMessage,
		},
		{
			name: "inner signer is not the shield module",
			setup: func(f *fixture) {
				f.keeper.SetRouter(eventRouter{})
				f.keeper.RegisterShieldAwareModule("/sparkdream.shield.v1.", &mockShieldAware{compatible: true})
			},
			inner: func(t *testing.T) *any.Any {
				return innerAny(t, &types.MsgTriggerDkg{Authority: authtypes.NewModuleAddress("gov").String()})
			},
			wantIs: types.ErrInvalidInnerMessageSigner,
		},
		{
			name:   "no ShieldAware registered",
			setup:  func(f *fixture) { f.keeper.SetRouter(eventRouter{}) },
			inner:  shieldSigned,
			wantIs: types.ErrIncompatibleOperation,
		},
		{
			name: "ShieldAware rejects the message",
			setup: func(f *fixture) {
				f.keeper.SetRouter(eventRouter{})
				f.keeper.RegisterShieldAwareModule("/sparkdream.shield.v1.", &mockShieldAware{compatible: false})
			},
			inner:  shieldSigned,
			wantIs: types.ErrIncompatibleOperation,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := initFixture(t)
			tc.setup(f)
			ctx := sdk.UnwrapSDKContext(f.ctx).WithEventManager(sdk.NewEventManager())
			_, err := f.keeper.ExecuteInnerMessage(ctx, types.DefaultParams(), tc.inner(t))
			require.ErrorIs(t, err, tc.wantIs)
			for _, ev := range ctx.EventManager().Events() {
				require.NotEqual(t, "collection_created", ev.Type, "handler must not run")
			}
		})
	}
}

// TestPrecheckOwnershipModeRejections: an ownership-mode exec whose target
// module has no ShieldAware is incompatible; one whose ShieldAware cannot
// resolve ownership is not ownable.
func TestPrecheckOwnershipModeRejections(t *testing.T) {
	cases := []struct {
		name   string
		sa     types.ShieldAware
		wantIs error
	}{
		{"no ShieldAware registered", nil, types.ErrIncompatibleOperation},
		{"ShieldAware without ownership resolver", &mockShieldAware{compatible: true}, types.ErrNotOwnable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := initFixture(t)
			if tc.sa != nil {
				f.keeper.RegisterShieldAwareModule("/sparkdream.shield.v1.", tc.sa)
			}
			inner := innerAny(t, &types.MsgTriggerDkg{Authority: authtypes.NewModuleAddress(types.ModuleName).String()})
			require.NoError(t, f.keeper.SetShieldedOp(f.ctx, types.ShieldedOpRegistration{
				MessageTypeUrl:  inner.TypeUrl,
				ProofDomain:     types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
				NullifierDomain: 12,
				NullifierMode:   types.NullifierMode_NULLIFIER_MODE_OWNERSHIP,
				Active:          true,
				BatchMode:       types.ShieldBatchMode_SHIELD_BATCH_MODE_IMMEDIATE_ONLY,
			}))
			msg := &types.MsgShieldedExec{
				ExecMode:           types.ShieldExecMode_SHIELD_EXEC_IMMEDIATE,
				ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
				InnerMessage:       inner,
				Nullifier:          make([]byte, 32),
				RateLimitNullifier: make([]byte, 32),
			}
			require.ErrorIs(t, f.keeper.PrecheckExecErr(f.ctx.WithIsCheckTx(true), msg), tc.wantIs)
		})
	}
}

// TestPrecheckUnknownProofDomain: with a verification key stored and the rep
// keeper wired, a proof domain the merkle-root check does not know is
// refused with ErrInvalidProofDomain before the proof is parsed.
func TestPrecheckUnknownProofDomain(t *testing.T) {
	f := initFixtureWithRepKeeper(t, &mockRepKeeper{currentRoot: make([]byte, 32), previousRoot: make([]byte, 32)})
	require.NoError(t, f.keeper.SetVerificationKey(f.ctx, types.VerificationKey{CircuitId: types.ShieldCircuitID, VkBytes: []byte("vk")}))
	require.NoError(t, f.keeper.SetShieldedOp(f.ctx, types.ShieldedOpRegistration{
		MessageTypeUrl:     "/sparkdream.aurora.v1.MsgZenith",
		ProofDomain:        types.ProofDomain_PROOF_DOMAIN_UNSPECIFIED,
		NullifierDomain:    13,
		NullifierScopeType: types.NullifierScopeType_NULLIFIER_SCOPE_GLOBAL,
		Active:             true,
		BatchMode:          types.ShieldBatchMode_SHIELD_BATCH_MODE_EITHER,
	}))
	msg := &types.MsgShieldedExec{
		ExecMode:           types.ShieldExecMode_SHIELD_EXEC_IMMEDIATE,
		ProofDomain:        types.ProofDomain_PROOF_DOMAIN_UNSPECIFIED,
		InnerMessage:       &any.Any{TypeUrl: "/sparkdream.aurora.v1.MsgZenith"},
		Nullifier:          make([]byte, 32),
		RateLimitNullifier: make([]byte, 32),
		MerkleRoot:         make([]byte, 32),
		Proof:              []byte("proof"),
	}
	require.ErrorIs(t, f.keeper.PrecheckExecErr(f.ctx, msg), types.ErrInvalidProofDomain)
}

// TestPrecheckImmediateRefusesIncompatibleOp: an active registration whose
// target module rejects the message is refused before the ante spends the
// nullifier or the reserve pays a fee (a module that refuses every exec,
// such as anonymous council votes, would otherwise cost a fee per attempt).
func TestPrecheckImmediateRefusesIncompatibleOp(t *testing.T) {
	f := initFixture(t)
	inner := innerAny(t, &types.MsgTriggerDkg{Authority: authtypes.NewModuleAddress(types.ModuleName).String()})
	require.NoError(t, f.keeper.SetShieldedOp(f.ctx, types.ShieldedOpRegistration{
		MessageTypeUrl:     inner.TypeUrl,
		ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
		Active:             true,
		BatchMode:          types.ShieldBatchMode_SHIELD_BATCH_MODE_EITHER,
		NullifierDomain:    99,
		NullifierScopeType: types.NullifierScopeType_NULLIFIER_SCOPE_GLOBAL,
	}))
	msg := &types.MsgShieldedExec{
		ExecMode:           types.ShieldExecMode_SHIELD_EXEC_IMMEDIATE,
		ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
		InnerMessage:       inner,
		Nullifier:          make([]byte, 32),
		RateLimitNullifier: make([]byte, 32),
	}

	f.keeper.RegisterShieldAwareModule("/sparkdream.shield.v1.", &mockShieldAware{compatible: false})
	require.ErrorIs(t, f.keeper.PrecheckImmediateErr(f.ctx, msg), types.ErrIncompatibleOperation)

	f.keeper.RegisterShieldAwareModule("/sparkdream.shield.v1.", &mockShieldAware{compatible: true})
	require.NoError(t, f.keeper.PrecheckImmediateErr(f.ctx, msg))
}
