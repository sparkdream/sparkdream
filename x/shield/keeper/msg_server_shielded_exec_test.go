package keeper_test

import (
	"encoding/hex"
	"testing"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
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

	// Arbiter hashes require MinTrustLevel=2
	_, err = ms.ShieldedExec(f.ctx, &types.MsgShieldedExec{
		Submitter: submitter,
		ExecMode:  types.ShieldExecMode_SHIELD_EXEC_IMMEDIATE,
		InnerMessage: &any.Any{
			TypeUrl: "/sparkdream.federation.v1.MsgSubmitArbiterHash",
			Value:   []byte("data"),
		},
		ProofDomain:   types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
		MinTrustLevel: 1, // Below required
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInsufficientTrustLevel)
}

// The registration's MinTrustLevel is a floor, not an exact match: a proof at
// the floor (0 for content ops) or above it, made for a target that asks for
// more, passes the trust check. The garbage inner message fails later.
func TestShieldedExecImmediateTrustLevelAtOrAboveFloor(t *testing.T) {
	f, ms := initMsgServer(t)

	submitter, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("test"))
	require.NoError(t, err)

	for _, level := range []uint32{0, 2, 4} {
		_, err = ms.ShieldedExec(f.ctx, &types.MsgShieldedExec{
			Submitter: submitter,
			ExecMode:  types.ShieldExecMode_SHIELD_EXEC_IMMEDIATE,
			InnerMessage: &any.Any{
				TypeUrl: "/sparkdream.blog.v1.MsgCreateReply",
				Value:   []byte("data"),
			},
			ProofDomain:   types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
			MinTrustLevel: level,
		})
		require.Error(t, err)
		require.NotErrorIs(t, err, types.ErrInsufficientTrustLevel, "level %d", level)
	}
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

func TestPrecheckImmediateWritesNothing(t *testing.T) {
	f := initFixture(t)

	require.NoError(t, f.keeper.SetShieldedOp(f.ctx, types.ShieldedOpRegistration{
		MessageTypeUrl:     "/sparkdream.test.v1.MsgPrecheck",
		ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
		Active:             true,
		BatchMode:          types.ShieldBatchMode_SHIELD_BATCH_MODE_EITHER,
		NullifierDomain:    99,
		NullifierScopeType: types.NullifierScopeType_NULLIFIER_SCOPE_GLOBAL,
	}))

	msg := &types.MsgShieldedExec{
		ExecMode:           types.ShieldExecMode_SHIELD_EXEC_IMMEDIATE,
		ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
		InnerMessage:       &any.Any{TypeUrl: "/sparkdream.test.v1.MsgPrecheck"},
		Nullifier:          make([]byte, 32),
		RateLimitNullifier: make([]byte, 32),
	}

	// Test builds skip proof verification when no VK is stored.
	require.NoError(t, f.keeper.PrecheckImmediate(f.ctx, msg))
	require.NoError(t, f.keeper.PrecheckImmediate(f.ctx, msg))
	require.False(t, f.keeper.IsNullifierUsed(f.ctx, 99, 0, hex.EncodeToString(msg.Nullifier)))
	require.Zero(t, f.keeper.GetIdentityRateLimitCount(f.ctx, hex.EncodeToString(msg.RateLimitNullifier)))

	// Simulate runs on the check state but must not mark the nullifier: its
	// msg server would then find it used.
	simCtx := f.ctx.WithIsCheckTx(true).WithExecMode(sdk.ExecModeSimulate)
	require.NoError(t, f.keeper.PrecheckImmediate(simCtx, msg))
	require.NoError(t, f.keeper.PrecheckImmediate(simCtx, msg))
	require.False(t, f.keeper.IsNullifierUsed(f.ctx, 99, 0, hex.EncodeToString(msg.Nullifier)))

	// A used nullifier is caught before any fee is paid.
	require.NoError(t, f.keeper.RecordNullifier(f.ctx, 99, 0, hex.EncodeToString(msg.Nullifier), 1))
	require.ErrorIs(t, f.keeper.PrecheckImmediate(f.ctx, msg), types.ErrNullifierUsed)
}

// In CheckTx the precheck marks the nullifier in the check state, so copies of
// a pending exec rewrapped in new outer txs (anyone can sign as the public
// submitter) are refused from the mempool instead of each taking block space.
func TestPrecheckImmediateReservesNullifierInCheckTx(t *testing.T) {
	f := initFixture(t)

	require.NoError(t, f.keeper.SetShieldedOp(f.ctx, types.ShieldedOpRegistration{
		MessageTypeUrl:     "/sparkdream.test.v1.MsgPrecheck",
		ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
		Active:             true,
		BatchMode:          types.ShieldBatchMode_SHIELD_BATCH_MODE_EITHER,
		NullifierDomain:    99,
		NullifierScopeType: types.NullifierScopeType_NULLIFIER_SCOPE_GLOBAL,
	}))
	msg := &types.MsgShieldedExec{
		ExecMode:           types.ShieldExecMode_SHIELD_EXEC_IMMEDIATE,
		ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
		InnerMessage:       &any.Any{TypeUrl: "/sparkdream.test.v1.MsgPrecheck"},
		Nullifier:          make([]byte, 32),
		RateLimitNullifier: make([]byte, 32),
	}

	checkCtx := f.ctx.WithIsCheckTx(true)
	require.NoError(t, f.keeper.PrecheckImmediate(checkCtx, msg))
	require.ErrorIs(t, f.keeper.PrecheckImmediate(checkCtx, msg), types.ErrNullifierUsed)
}

// A two-field scope path packs the qualifier into the top byte, so the same id
// under different qualifiers (a collection 5 and an item 5) gets different
// nullifier scopes. The fixture's codec only knows shield messages, so a
// MsgShieldedExec stands in as the inner message: exec_mode is the qualifier,
// min_trust_level the id.
func TestResolveNullifierScopeTwoFields(t *testing.T) {
	f := initFixture(t)
	reg := types.ShieldedOpRegistration{
		NullifierScopeType: types.NullifierScopeType_NULLIFIER_SCOPE_MESSAGE_FIELD,
		ScopeFieldPath:     "exec_mode,min_trust_level",
	}
	scopeOf := func(mode types.ShieldExecMode, id uint32) (uint64, error) {
		inner, err := codectypes.NewAnyWithValue(&types.MsgShieldedExec{ExecMode: mode, MinTrustLevel: id})
		require.NoError(t, err)
		return f.keeper.ResolveNullifierScope(f.ctx, reg, &types.MsgShieldedExec{
			InnerMessage: &any.Any{TypeUrl: inner.TypeUrl, Value: inner.Value},
		})
	}

	immediate, err := scopeOf(types.ShieldExecMode_SHIELD_EXEC_IMMEDIATE, 5)
	require.NoError(t, err)
	batch, err := scopeOf(types.ShieldExecMode_SHIELD_EXEC_ENCRYPTED_BATCH, 5)
	require.NoError(t, err)
	require.NotEqual(t, immediate, batch)

	want, err := types.PackNullifierScope(uint64(types.ShieldExecMode_SHIELD_EXEC_ENCRYPTED_BATCH), 5)
	require.NoError(t, err)
	require.Equal(t, want, batch)

	// A field that doesn't exist fails the scope rather than defaulting it.
	reg.ScopeFieldPath = "exec_mode,no_such_field"
	_, err = scopeOf(types.ShieldExecMode_SHIELD_EXEC_IMMEDIATE, 5)
	require.ErrorIs(t, err, types.ErrInvalidInnerMessage)
}

// A fallback scope path uses the first field when it is non-zero (qualified
// with 1) and the second otherwise (unqualified), so a reaction on reply 5 and
// one on post 5 get different scopes while a post reaction keeps the bare
// post id. target_epoch stands in for reply_id, min_trust_level for post_id.
func TestResolveNullifierScopeFallback(t *testing.T) {
	f := initFixture(t)
	reg := types.ShieldedOpRegistration{
		NullifierScopeType: types.NullifierScopeType_NULLIFIER_SCOPE_MESSAGE_FIELD,
		ScopeFieldPath:     "target_epoch" + types.ScopeFieldFallback + "min_trust_level",
	}
	scopeOf := func(first uint64, second uint32) (uint64, error) {
		inner, err := codectypes.NewAnyWithValue(&types.MsgShieldedExec{TargetEpoch: first, MinTrustLevel: second})
		require.NoError(t, err)
		return f.keeper.ResolveNullifierScope(f.ctx, reg, &types.MsgShieldedExec{
			InnerMessage: &any.Any{TypeUrl: inner.TypeUrl, Value: inner.Value},
		})
	}

	onSecond, err := scopeOf(0, 5)
	require.NoError(t, err)
	require.Equal(t, uint64(5), onSecond)

	onFirst, err := scopeOf(5, 5)
	require.NoError(t, err)
	want, err := types.PackNullifierScope(1, 5)
	require.NoError(t, err)
	require.Equal(t, want, onFirst)
	require.NotEqual(t, onSecond, onFirst)

	// A field that doesn't exist fails the scope rather than defaulting it.
	reg.ScopeFieldPath = "no_such_field" + types.ScopeFieldFallback + "min_trust_level"
	_, err = scopeOf(0, 5)
	require.ErrorIs(t, err, types.ErrInvalidInnerMessage)
}

// An EPOCH scope with an epoch_window is the window index, so every epoch in
// a window shares one scope (one action per member per window).
func TestResolveNullifierScopeEpochWindow(t *testing.T) {
	f := initFixture(t)
	reg := types.ShieldedOpRegistration{
		NullifierScopeType: types.NullifierScopeType_NULLIFIER_SCOPE_EPOCH,
		EpochWindow:        12,
	}
	scopeAt := func(epoch uint64) uint64 {
		require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: epoch}))
		scope, err := f.keeper.ResolveNullifierScope(f.ctx, reg, &types.MsgShieldedExec{})
		require.NoError(t, err)
		return scope
	}

	require.Equal(t, uint64(2), scopeAt(24))
	require.Equal(t, uint64(2), scopeAt(35))
	require.Equal(t, uint64(3), scopeAt(36))

	reg.EpochWindow = 0
	require.Equal(t, uint64(36), scopeAt(36))
}
