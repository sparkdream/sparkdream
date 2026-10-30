package keeper_test

import (
	"testing"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/types"
)

func TestDeregisterShieldedOpInvalidAuthority(t *testing.T) {
	f, ms := initMsgServer(t)

	_, err := ms.DeregisterShieldedOp(f.ctx, &types.MsgDeregisterShieldedOp{
		Authority:      "not_valid_bech32!!!",
		MessageTypeUrl: "/sparkdream.blog.v1.MsgCreatePost",
	})
	require.Error(t, err)
}

func TestDeregisterShieldedOpThenReregister(t *testing.T) {
	f, ms := initMsgServer(t)

	authority, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("gov"))
	require.NoError(t, err)

	// Deregister an op
	_, err = ms.DeregisterShieldedOp(f.ctx, &types.MsgDeregisterShieldedOp{
		Authority:      authority,
		MessageTypeUrl: "/sparkdream.blog.v1.MsgCreatePost",
	})
	require.NoError(t, err)
	_, found := f.keeper.GetShieldedOp(f.ctx, "/sparkdream.blog.v1.MsgCreatePost")
	require.False(t, found)

	// Re-register with different settings
	_, err = ms.RegisterShieldedOp(f.ctx, &types.MsgRegisterShieldedOp{
		Authority: authority,
		Registration: types.ShieldedOpRegistration{
			MessageTypeUrl:  "/sparkdream.blog.v1.MsgCreatePost",
			MinTrustLevel:   3,
			NullifierDomain: 99,
			Active:          true,
		},
	})
	require.NoError(t, err)

	got, found := f.keeper.GetShieldedOp(f.ctx, "/sparkdream.blog.v1.MsgCreatePost")
	require.True(t, found)
	require.Equal(t, uint32(3), got.MinTrustLevel)
	require.Equal(t, uint32(99), got.NullifierDomain)
}

func TestDeregisterShieldedOpDoubleDeregister(t *testing.T) {
	f, ms := initMsgServer(t)

	authority, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("gov"))
	require.NoError(t, err)

	// First deregister succeeds
	_, err = ms.DeregisterShieldedOp(f.ctx, &types.MsgDeregisterShieldedOp{
		Authority:      authority,
		MessageTypeUrl: "/sparkdream.blog.v1.MsgCreatePost",
	})
	require.NoError(t, err)

	// Second deregister fails (not found)
	_, err = ms.DeregisterShieldedOp(f.ctx, &types.MsgDeregisterShieldedOp{
		Authority:      authority,
		MessageTypeUrl: "/sparkdream.blog.v1.MsgCreatePost",
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrUnregisteredOperation)
}

// Deregistering the last op of an EPOCH domain keeps the domain's scoping
// rule: its nullifiers still expire, the domain cannot be reused under a
// different rule while they are stored, and the rule is dropped once
// pruning has emptied the domain.
func TestDeregisterLastEpochOpRetiresDomain(t *testing.T) {
	f, ms := initMsgServer(t)
	authority, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("gov"))
	require.NoError(t, err)

	// Domain 31 is used only by the EPOCH-scoped anonymous-proposal op.
	const opURL = "/sparkdream.commons.v1.MsgSubmitAnonymousProposal"
	orig, found := f.keeper.GetShieldedOp(f.ctx, opURL)
	require.True(t, found)
	require.NoError(t, f.keeper.RecordNullifier(f.ctx, 31, 2, "old", 1))
	require.NoError(t, f.keeper.RecordNullifier(f.ctx, 31, 9, "live", 1))

	_, err = ms.DeregisterShieldedOp(f.ctx, &types.MsgDeregisterShieldedOp{Authority: authority, MessageTypeUrl: opURL})
	require.NoError(t, err)
	retired, err := f.keeper.RetiredDomains.Get(f.ctx, 31)
	require.NoError(t, err)
	require.Equal(t, opURL, retired.MessageTypeUrl)

	// Pruning still finds the domain.
	require.NoError(t, f.keeper.PruneEpochScopedNullifiers(f.ctx, 5))
	require.False(t, f.keeper.IsNullifierUsed(f.ctx, 31, 2, "old"))
	require.True(t, f.keeper.IsNullifierUsed(f.ctx, 31, 9, "live"))

	// A different rule on the domain is refused while it holds nullifiers.
	conflicting := types.ShieldedOpRegistration{
		MessageTypeUrl:     "/sparkdream.blog.v1.MsgReact",
		NullifierDomain:    31,
		NullifierScopeType: types.NullifierScopeType_NULLIFIER_SCOPE_GLOBAL,
		Active:             true,
	}
	_, err = ms.RegisterShieldedOp(f.ctx, &types.MsgRegisterShieldedOp{Authority: authority, Registration: conflicting})
	require.ErrorIs(t, err, types.ErrNullifierDomainConflict)

	// The retired rule survives a genesis round-trip.
	gs, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NoError(t, gs.Validate())
	require.Len(t, gs.RetiredDomains, 1)

	// Once pruning empties the domain, the rule is dropped.
	require.NoError(t, f.keeper.PruneEpochScopedNullifiers(f.ctx, 100))
	require.False(t, f.keeper.IsNullifierUsed(f.ctx, 31, 9, "live"))
	has, err := f.keeper.RetiredDomains.Has(f.ctx, 31)
	require.NoError(t, err)
	require.False(t, has)

	// Re-registering the original op is always compatible and makes the
	// domain live again.
	_, err = ms.RegisterShieldedOp(f.ctx, &types.MsgRegisterShieldedOp{Authority: authority, Registration: orig})
	require.NoError(t, err)
}

// A non-EPOCH domain keeps its retired rule (its nullifiers never expire),
// and re-registering a compatible op clears it.
func TestDeregisterMessageFieldDomainStaysRetired(t *testing.T) {
	f, ms := initMsgServer(t)
	authority, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("gov"))
	require.NoError(t, err)

	const opURL = "/sparkdream.commons.v1.MsgAnonymousVoteProposal" // domain 32, MESSAGE_FIELD
	orig, found := f.keeper.GetShieldedOp(f.ctx, opURL)
	require.True(t, found)
	require.NoError(t, f.keeper.RecordNullifier(f.ctx, 32, 1, "vote", 1))

	_, err = ms.DeregisterShieldedOp(f.ctx, &types.MsgDeregisterShieldedOp{Authority: authority, MessageTypeUrl: opURL})
	require.NoError(t, err)
	require.NoError(t, f.keeper.PruneEpochScopedNullifiers(f.ctx, 1_000_000))
	require.True(t, f.keeper.IsNullifierUsed(f.ctx, 32, 1, "vote"))
	has, err := f.keeper.RetiredDomains.Has(f.ctx, 32)
	require.NoError(t, err)
	require.True(t, has)

	_, err = ms.RegisterShieldedOp(f.ctx, &types.MsgRegisterShieldedOp{Authority: authority, Registration: orig})
	require.NoError(t, err)
	has, err = f.keeper.RetiredDomains.Has(f.ctx, 32)
	require.NoError(t, err)
	require.False(t, has)
}
