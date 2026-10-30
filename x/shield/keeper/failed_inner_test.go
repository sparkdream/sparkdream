package keeper_test

import (
	"encoding/hex"
	"errors"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/keeper"
	"sparkdream/x/shield/types"
)

// TestFailedInnerStillSpendsExec (SHD-03): in DeliverTx the ante handler pays
// the fee and spends the exec (nullifier + identity rate limit) in the ante
// state, which commits even when the inner message fails and the msg state is
// discarded. Before the fix the nullifier and the rate-limit increment were
// written in the msg state, so a failing exec cost the reserve a fee every
// block and could be replayed (by anyone, as the submitter is public) forever.
func TestFailedInnerStillSpendsExec(t *testing.T) {
	f := initFixture(t)
	f.keeper.SetRouter(writeThenRouter{k: f.keeper, err: errors.New("target does not exist")})
	f.keeper.RegisterShieldAwareModule("/sparkdream.shield.v1.", &ownedModule{})

	const domain = 31
	inner := innerAny(t, &types.MsgTriggerDkg{Authority: authtypes.NewModuleAddress(types.ModuleName).String()})
	require.NoError(t, f.keeper.SetShieldedOp(f.ctx, types.ShieldedOpRegistration{
		MessageTypeUrl:     inner.TypeUrl,
		ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
		NullifierDomain:    domain,
		NullifierScopeType: types.NullifierScopeType_NULLIFIER_SCOPE_GLOBAL,
		Active:             true,
		BatchMode:          types.ShieldBatchMode_SHIELD_BATCH_MODE_EITHER,
	}))

	nullifier := make([]byte, 32)
	nullifier[31] = 0x5a
	rateLimit := make([]byte, 32)
	rateLimit[31] = 0x6b
	msg := &types.MsgShieldedExec{
		Submitter:          submitterAddr(t, f),
		ExecMode:           types.ShieldExecMode_SHIELD_EXEC_IMMEDIATE,
		ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
		InnerMessage:       inner,
		Nullifier:          nullifier,
		RateLimitNullifier: rateLimit,
		MerkleRoot:         make([]byte, 32),
		Proof:              make([]byte, 128),
	}

	// The DeliverTx flow: ante (writes kept), then the msg in its own cache.
	deliver := f.ctx.WithExecMode(sdk.ExecModeFinalize)
	deliverTx := func() (anteErr, msgErr error) {
		consumed, err := f.keeper.PrecheckExec(deliver, msg)
		if err != nil {
			return err, nil
		}
		require.True(t, consumed, "DeliverTx spends the exec in the ante")
		msgCtx, write := types.WithExecConsumed(types.WithProofVerified(deliver, msg), msg).CacheContext()
		if _, err := keeper.NewMsgServerImpl(f.keeper).ShieldedExec(msgCtx, msg); err != nil {
			return nil, err // baseapp discards the msg state
		}
		write()
		return nil, nil
	}

	anteErr, msgErr := deliverTx()
	require.NoError(t, anteErr)
	require.ErrorContains(t, msgErr, "target does not exist")

	// The failed exec is spent all the same.
	require.True(t, f.keeper.IsNullifierUsed(f.ctx, domain, 0, hex.EncodeToString(nullifier)))
	require.Equal(t, uint64(1), f.keeper.GetIdentityRateLimitCount(f.ctx, hex.EncodeToString(rateLimit)))
	_, ran := f.keeper.GetShieldEpochDecryptionKeyVal(f.ctx, markerEpoch)
	require.False(t, ran, "the failed inner message left no state")

	// Replaying it (next block, rewrapped) fails in the ante: no second fee.
	anteErr, _ = deliverTx()
	require.ErrorIs(t, anteErr, types.ErrNullifierUsed)
	require.Equal(t, uint64(1), f.keeper.GetIdentityRateLimitCount(f.ctx, hex.EncodeToString(rateLimit)))

	// Even once the target would succeed, the spent exec can't be re-run.
	f.keeper.SetRouter(writeThenRouter{k: f.keeper})
	anteErr, _ = deliverTx()
	require.ErrorIs(t, anteErr, types.ErrNullifierUsed)
}

// TestConsumedExecRunsOnce: a successful exec spent by the ante runs its
// inner message exactly once in the msg server, which neither re-checks nor
// re-records the nullifier, and simulate spends nothing.
func TestConsumedExecRunsOnce(t *testing.T) {
	f := initFixture(t)
	f.keeper.SetRouter(writeThenRouter{k: f.keeper})
	f.keeper.RegisterShieldAwareModule("/sparkdream.shield.v1.", &ownedModule{})
	inner := innerAny(t, &types.MsgTriggerDkg{Authority: authtypes.NewModuleAddress(types.ModuleName).String()})
	require.NoError(t, f.keeper.SetShieldedOp(f.ctx, types.ShieldedOpRegistration{
		MessageTypeUrl:     inner.TypeUrl,
		ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
		NullifierDomain:    32,
		NullifierScopeType: types.NullifierScopeType_NULLIFIER_SCOPE_GLOBAL,
		Active:             true,
		BatchMode:          types.ShieldBatchMode_SHIELD_BATCH_MODE_EITHER,
	}))
	msg := &types.MsgShieldedExec{
		Submitter:          submitterAddr(t, f),
		ExecMode:           types.ShieldExecMode_SHIELD_EXEC_IMMEDIATE,
		ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
		InnerMessage:       inner,
		Nullifier:          []byte{31: 1},
		RateLimitNullifier: []byte{31: 2},
		MerkleRoot:         make([]byte, 32),
		Proof:              make([]byte, 128),
	}

	// Simulate validates but spends nothing.
	consumed, err := f.keeper.PrecheckExec(f.ctx.WithExecMode(sdk.ExecModeSimulate), msg)
	require.NoError(t, err)
	require.False(t, consumed)
	require.Zero(t, f.keeper.GetIdentityRateLimitCount(f.ctx, hex.EncodeToString(msg.RateLimitNullifier)))

	deliver := f.ctx.WithExecMode(sdk.ExecModeFinalize)
	consumed, err = f.keeper.PrecheckExec(deliver, msg)
	require.NoError(t, err)
	require.True(t, consumed)
	_, err = keeper.NewMsgServerImpl(f.keeper).ShieldedExec(types.WithExecConsumed(types.WithProofVerified(deliver, msg), msg), msg)
	require.NoError(t, err)
	_, ran := f.keeper.GetShieldEpochDecryptionKeyVal(f.ctx, markerEpoch)
	require.True(t, ran)
	require.Equal(t, uint64(1), f.keeper.GetIdentityRateLimitCount(f.ctx, hex.EncodeToString(msg.RateLimitNullifier)), "counted once")
}

func submitterAddr(t *testing.T, f *fixture) string {
	t.Helper()
	addr, err := f.addressCodec.BytesToString(types.PublicSubmitterAddress())
	require.NoError(t, err)
	return addr
}
