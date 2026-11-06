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

// TestRegisterShieldedOpDomainConflict (SHD-05): an op can't join a nullifier
// domain whose existing ops scope nullifiers differently (here an EPOCH op in
// the MESSAGE_FIELD domain of anonymous proposal votes, which pruning would
// then wipe), but may share a domain with identical scoping.
func TestRegisterShieldedOpDomainConflict(t *testing.T) {
	f, ms := initMsgServer(t)
	authority, err := f.addressCodec.BytesToString(f.keeper.GetAuthority())
	require.NoError(t, err)

	votes, found := f.keeper.GetShieldedOp(f.ctx, "/sparkdream.commons.v1.MsgAnonymousVoteProposal")
	require.True(t, found)

	clash := types.ShieldedOpRegistration{
		MessageTypeUrl:     "/sparkdream.phoenix.v1.MsgSpark",
		ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
		NullifierDomain:    votes.NullifierDomain,
		NullifierScopeType: types.NullifierScopeType_NULLIFIER_SCOPE_EPOCH,
		Active:             true,
	}
	_, err = ms.RegisterShieldedOp(f.ctx, &types.MsgRegisterShieldedOp{Authority: authority, Registration: clash})
	require.ErrorIs(t, err, types.ErrNullifierDomainConflict)

	same := votes
	same.MessageTypeUrl = "/sparkdream.phoenix.v1.MsgSpark"
	_, err = ms.RegisterShieldedOp(f.ctx, &types.MsgRegisterShieldedOp{Authority: authority, Registration: same})
	require.NoError(t, err)

	// Updating an op in place is checked against the others only.
	votes.Active = false
	_, err = ms.RegisterShieldedOp(f.ctx, &types.MsgRegisterShieldedOp{Authority: authority, Registration: votes})
	require.NoError(t, err)

	// The genesis op table obeys the same rule.
	require.NoError(t, types.DefaultGenesis().Validate())
	gen := types.DefaultGenesis()
	gen.RegisteredOps = append(gen.RegisteredOps, clash)
	require.ErrorIs(t, gen.Validate(), types.ErrNullifierDomainConflict)
}

// TestRegisterShieldedOpInPlaceKeepsScoping (SHD-103): an op alone in its
// domain can't change its scoping in place (the domain's stored nullifiers
// were written under the old rule; an EPOCH rescope would let pruning delete
// them). A new domain is required; settings that don't touch scoping still
// update in place.
func TestRegisterShieldedOpInPlaceKeepsScoping(t *testing.T) {
	f, ms := initMsgServer(t)
	authority, err := f.addressCodec.BytesToString(f.keeper.GetAuthority())
	require.NoError(t, err)
	register := func(reg types.ShieldedOpRegistration) error {
		_, err := ms.RegisterShieldedOp(f.ctx, &types.MsgRegisterShieldedOp{Authority: authority, Registration: reg})
		return err
	}
	reg := types.ShieldedOpRegistration{
		MessageTypeUrl:     "/sparkdream.zenith.v1.MsgAlone",
		ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
		NullifierDomain:    97,
		NullifierScopeType: types.NullifierScopeType_NULLIFIER_SCOPE_MESSAGE_FIELD,
		ScopeFieldPath:     "content_id",
		Active:             true,
		BatchMode:          types.ShieldBatchMode_SHIELD_BATCH_MODE_IMMEDIATE_ONLY,
	}
	require.NoError(t, register(reg))

	rescoped := reg
	rescoped.NullifierScopeType = types.NullifierScopeType_NULLIFIER_SCOPE_EPOCH
	rescoped.ScopeFieldPath = ""
	require.ErrorIs(t, register(rescoped), types.ErrNullifierDomainConflict)
	got, found := f.keeper.GetShieldedOp(f.ctx, reg.MessageTypeUrl)
	require.True(t, found)
	require.Equal(t, types.NullifierScopeType_NULLIFIER_SCOPE_MESSAGE_FIELD, got.NullifierScopeType)

	// Same rule under a new domain: allowed (the old domain is retired).
	rescoped.NullifierDomain = 98
	require.NoError(t, register(rescoped))

	// A non-scoping change in place is fine.
	rescoped.MinTrustLevel = 2
	require.NoError(t, register(rescoped))
}
