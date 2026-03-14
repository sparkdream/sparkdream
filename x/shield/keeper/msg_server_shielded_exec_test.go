package keeper_test

import (
	"testing"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	any "github.com/cosmos/gogoproto/types/any"
	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/types"
)

func TestShieldedExecInvalidSubmitter(t *testing.T) {
	f, ms := initMsgServer(t)

	_, err := ms.ShieldedExec(f.ctx, &types.MsgShieldedExec{
		Submitter: "not_valid!!!",
		ExecMode:  types.ShieldExecMode_SHIELD_EXEC_IMMEDIATE,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid submitter address")
}

func TestShieldedExecImmediateUnregisteredOp(t *testing.T) {
	f, ms := initMsgServer(t)

	submitter, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("test"))
	require.NoError(t, err)

	_, err = ms.ShieldedExec(f.ctx, &types.MsgShieldedExec{
		Submitter: submitter,
		ExecMode:  types.ShieldExecMode_SHIELD_EXEC_IMMEDIATE,
		InnerMessage: &any.Any{
			TypeUrl: "/sparkdream.unknown.v1.MsgNotRegistered",
			Value:   []byte("data"),
		},
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrUnregisteredOperation)
}

func TestShieldedExecImmediateInactiveOp(t *testing.T) {
	f, ms := initMsgServer(t)

	submitter, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("test"))
	require.NoError(t, err)

	// Register an inactive op
	require.NoError(t, f.keeper.SetShieldedOp(f.ctx, types.ShieldedOpRegistration{
		MessageTypeUrl: "/sparkdream.test.v1.MsgInactive",
		ProofDomain:    types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
		Active:         false,
		BatchMode:      types.ShieldBatchMode_SHIELD_BATCH_MODE_IMMEDIATE_ONLY,
	}))

	_, err = ms.ShieldedExec(f.ctx, &types.MsgShieldedExec{
		Submitter: submitter,
		ExecMode:  types.ShieldExecMode_SHIELD_EXEC_IMMEDIATE,
		InnerMessage: &any.Any{
			TypeUrl: "/sparkdream.test.v1.MsgInactive",
			Value:   []byte("data"),
		},
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrOperationInactive)
}

func TestShieldedExecImmediateEncryptedOnlyOp(t *testing.T) {
	f, ms := initMsgServer(t)

	submitter, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("test"))
	require.NoError(t, err)

	// Register an encrypted-only op
	require.NoError(t, f.keeper.SetShieldedOp(f.ctx, types.ShieldedOpRegistration{
		MessageTypeUrl: "/sparkdream.test.v1.MsgEncOnly",
		ProofDomain:    types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
		Active:         true,
		BatchMode:      types.ShieldBatchMode_SHIELD_BATCH_MODE_ENCRYPTED_ONLY,
	}))

	_, err = ms.ShieldedExec(f.ctx, &types.MsgShieldedExec{
		Submitter: submitter,
		ExecMode:  types.ShieldExecMode_SHIELD_EXEC_IMMEDIATE,
		InnerMessage: &any.Any{
			TypeUrl: "/sparkdream.test.v1.MsgEncOnly",
			Value:   []byte("data"),
		},
		ProofDomain:   types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
		MinTrustLevel: 1,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrImmediateNotAllowed)
}

func TestShieldedExecImmediateProofDomainMismatch(t *testing.T) {
	f, ms := initMsgServer(t)

	submitter, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("test"))
	require.NoError(t, err)

	// Blog posts require PROOF_DOMAIN_TRUST_TREE
	_, err = ms.ShieldedExec(f.ctx, &types.MsgShieldedExec{
		Submitter: submitter,
		ExecMode:  types.ShieldExecMode_SHIELD_EXEC_IMMEDIATE,
		InnerMessage: &any.Any{
			TypeUrl: "/sparkdream.blog.v1.MsgCreatePost",
			Value:   []byte("data"),
		},
		ProofDomain:   99, // Wrong domain
		MinTrustLevel: 1,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrProofDomainMismatch)
}

func TestShieldedExecImmediateInsufficientTrustLevel(t *testing.T) {
	f, ms := initMsgServer(t)

	submitter, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("test"))
	require.NoError(t, err)

	// Blog posts require MinTrustLevel=1
	_, err = ms.ShieldedExec(f.ctx, &types.MsgShieldedExec{
		Submitter: submitter,
		ExecMode:  types.ShieldExecMode_SHIELD_EXEC_IMMEDIATE,
		InnerMessage: &any.Any{
			TypeUrl: "/sparkdream.blog.v1.MsgCreatePost",
			Value:   []byte("data"),
		},
		ProofDomain:   types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
		MinTrustLevel: 0, // Below required
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInsufficientTrustLevel)
}

func TestShieldedExecEncryptedBatchProofRejected(t *testing.T) {
	f, ms := initMsgServer(t)

	submitter, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("test"))
	require.NoError(t, err)

	// Enable encrypted batch
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 1}))

	// Proof should be empty in encrypted batch mode
	_, err = ms.ShieldedExec(f.ctx, &types.MsgShieldedExec{
		Submitter:        submitter,
		ExecMode:         types.ShieldExecMode_SHIELD_EXEC_ENCRYPTED_BATCH,
		Proof:            []byte("proof_data"),
		EncryptedPayload: []byte("encrypted"),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrCleartextFieldInBatchMode)
}
