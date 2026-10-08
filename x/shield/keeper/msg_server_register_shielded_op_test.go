package keeper_test

import (
	"testing"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/types"
)

func TestRegisterShieldedOpOverwrite(t *testing.T) {
	f, ms := initMsgServer(t)

	authority, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("gov"))
	require.NoError(t, err)

	// Register an op
	reg := types.ShieldedOpRegistration{
		MessageTypeUrl:     "/sparkdream.test.v1.MsgOverwrite",
		ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
		MinTrustLevel:      1,
		NullifierDomain:    88,
		NullifierScopeType: types.NullifierScopeType_NULLIFIER_SCOPE_EPOCH,
		Active:             true,
		BatchMode:          types.ShieldBatchMode_SHIELD_BATCH_MODE_IMMEDIATE_ONLY,
	}
	_, err = ms.RegisterShieldedOp(f.ctx, &types.MsgRegisterShieldedOp{
		Authority:    authority,
		Registration: reg,
	})
	require.NoError(t, err)

	// Overwrite with different settings
	reg.MinTrustLevel = 3
	reg.Active = false
	_, err = ms.RegisterShieldedOp(f.ctx, &types.MsgRegisterShieldedOp{
		Authority:    authority,
		Registration: reg,
	})
	require.NoError(t, err)

	got, found := f.keeper.GetShieldedOp(f.ctx, "/sparkdream.test.v1.MsgOverwrite")
	require.True(t, found)
	require.Equal(t, uint32(3), got.MinTrustLevel)
	require.False(t, got.Active)
}

func TestRegisterShieldedOpInvalidAuthority(t *testing.T) {
	f, ms := initMsgServer(t)

	_, err := ms.RegisterShieldedOp(f.ctx, &types.MsgRegisterShieldedOp{
		Authority: "not_valid_bech32!!!",
		Registration: types.ShieldedOpRegistration{
			MessageTypeUrl: "/sparkdream.test.v1.MsgBadAuth",
		},
	})
	require.Error(t, err)
}

func TestRegisterShieldedOpAllBatchModes(t *testing.T) {
	f, ms := initMsgServer(t)

	authority, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("gov"))
	require.NoError(t, err)

	modes := []struct {
		name string
		mode types.ShieldBatchMode
	}{
		{"immediate_only", types.ShieldBatchMode_SHIELD_BATCH_MODE_IMMEDIATE_ONLY},
		{"encrypted_only", types.ShieldBatchMode_SHIELD_BATCH_MODE_ENCRYPTED_ONLY},
		{"either", types.ShieldBatchMode_SHIELD_BATCH_MODE_EITHER},
	}

	for i, tc := range modes {
		t.Run(tc.name, func(t *testing.T) {
			reg := types.ShieldedOpRegistration{
				MessageTypeUrl:  "/sparkdream.test.v1.MsgMode" + tc.name,
				NullifierDomain: uint32(200 + i),
				BatchMode:       tc.mode,
				Active:          true,
			}
			_, err := ms.RegisterShieldedOp(f.ctx, &types.MsgRegisterShieldedOp{
				Authority:    authority,
				Registration: reg,
			})
			require.NoError(t, err)

			got, found := f.keeper.GetShieldedOp(f.ctx, reg.MessageTypeUrl)
			require.True(t, found)
			require.Equal(t, tc.mode, got.BatchMode)
		})
	}
}

// epoch_window only applies to EPOCH-scoped one-time ops.
func TestRegisterShieldedOpEpochWindow(t *testing.T) {
	f, ms := initMsgServer(t)

	authority, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("gov"))
	require.NoError(t, err)
	register := func(reg types.ShieldedOpRegistration) error {
		_, err := ms.RegisterShieldedOp(f.ctx, &types.MsgRegisterShieldedOp{Authority: authority, Registration: reg})
		return err
	}
	reg := types.ShieldedOpRegistration{
		MessageTypeUrl:     "/sparkdream.test.v1.MsgWindowed",
		ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
		NullifierDomain:    88,
		NullifierScopeType: types.NullifierScopeType_NULLIFIER_SCOPE_EPOCH,
		EpochWindow:        12,
		Active:             true,
		BatchMode:          types.ShieldBatchMode_SHIELD_BATCH_MODE_EITHER,
	}
	require.NoError(t, register(reg))
	got, found := f.keeper.GetShieldedOp(f.ctx, reg.MessageTypeUrl)
	require.True(t, found)
	require.Equal(t, uint64(12), got.EpochWindow)

	reg.NullifierScopeType = types.NullifierScopeType_NULLIFIER_SCOPE_MESSAGE_FIELD
	reg.ScopeFieldPath = "post_id"
	require.Error(t, register(reg))

	reg.NullifierScopeType = types.NullifierScopeType_NULLIFIER_SCOPE_EPOCH
	reg.ScopeFieldPath = ""
	reg.NullifierMode = types.NullifierMode_NULLIFIER_MODE_OWNERSHIP
	reg.BatchMode = types.ShieldBatchMode_SHIELD_BATCH_MODE_IMMEDIATE_ONLY
	require.Error(t, register(reg))
}
