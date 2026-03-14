package keeper_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/types"
)

func TestRegistrationOverwrite(t *testing.T) {
	f := initFixture(t)

	// Register an op
	reg := types.ShieldedOpRegistration{
		MessageTypeUrl:     "/sparkdream.test.v1.MsgFoo",
		ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
		MinTrustLevel:      1,
		NullifierDomain:    99,
		NullifierScopeType: types.NullifierScopeType_NULLIFIER_SCOPE_EPOCH,
		Active:             true,
		BatchMode:          types.ShieldBatchMode_SHIELD_BATCH_MODE_IMMEDIATE_ONLY,
	}
	require.NoError(t, f.keeper.SetShieldedOp(f.ctx, reg))

	// Overwrite with different settings
	reg.MinTrustLevel = 3
	reg.Active = false
	require.NoError(t, f.keeper.SetShieldedOp(f.ctx, reg))

	got, found := f.keeper.GetShieldedOp(f.ctx, "/sparkdream.test.v1.MsgFoo")
	require.True(t, found)
	require.Equal(t, uint32(3), got.MinTrustLevel)
	require.False(t, got.Active)
}

func TestRegistrationDeleteNonexistent(t *testing.T) {
	f := initFixture(t)

	// Deleting a non-existent key should not error (collections.Remove is idempotent)
	err := f.keeper.DeleteShieldedOp(f.ctx, "/sparkdream.nonexistent.v1.MsgNone")
	require.NoError(t, err)
}

func TestRegistrationIterateEarlyStop(t *testing.T) {
	f := initFixture(t)

	// Default genesis has 12 ops; iterate and stop after 3
	var count int
	err := f.keeper.IterateShieldedOps(f.ctx, func(_ string, _ types.ShieldedOpRegistration) bool {
		count++
		return count >= 3
	})
	require.NoError(t, err)
	require.Equal(t, 3, count)
}

func TestRegistrationBatchModes(t *testing.T) {
	f := initFixture(t)

	modes := []types.ShieldBatchMode{
		types.ShieldBatchMode_SHIELD_BATCH_MODE_IMMEDIATE_ONLY,
		types.ShieldBatchMode_SHIELD_BATCH_MODE_ENCRYPTED_ONLY,
		types.ShieldBatchMode_SHIELD_BATCH_MODE_EITHER,
	}

	for i, mode := range modes {
		reg := types.ShieldedOpRegistration{
			MessageTypeUrl: fmt.Sprintf("/sparkdream.test.v1.MsgBatch%d", i),
			BatchMode:      mode,
			Active:         true,
		}
		require.NoError(t, f.keeper.SetShieldedOp(f.ctx, reg))

		got, found := f.keeper.GetShieldedOp(f.ctx, reg.MessageTypeUrl)
		require.True(t, found)
		require.Equal(t, mode, got.BatchMode)
	}
}

func TestRegistrationNullifierScopeTypes(t *testing.T) {
	f := initFixture(t)

	tests := []struct {
		name      string
		scopeType types.NullifierScopeType
	}{
		{"epoch", types.NullifierScopeType_NULLIFIER_SCOPE_EPOCH},
		{"global", types.NullifierScopeType_NULLIFIER_SCOPE_GLOBAL},
		{"message_field", types.NullifierScopeType_NULLIFIER_SCOPE_MESSAGE_FIELD},
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reg := types.ShieldedOpRegistration{
				MessageTypeUrl:     fmt.Sprintf("/sparkdream.test.v1.MsgScope%d", i),
				NullifierScopeType: tc.scopeType,
				NullifierDomain:    uint32(200 + i),
				Active:             true,
			}
			require.NoError(t, f.keeper.SetShieldedOp(f.ctx, reg))

			got, found := f.keeper.GetShieldedOp(f.ctx, reg.MessageTypeUrl)
			require.True(t, found)
			require.Equal(t, tc.scopeType, got.NullifierScopeType)
		})
	}
}
