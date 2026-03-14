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
