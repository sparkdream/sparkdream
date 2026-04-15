package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/types"
)

func TestShieldAwareCompatibility(t *testing.T) {
	f := initFixture(t)

	// MsgSubmitArbiterHash should be compatible
	compatible := f.keeper.IsShieldCompatible(f.ctx, &types.MsgSubmitArbiterHash{})
	require.True(t, compatible)

	// Other messages should not be compatible
	compatible = f.keeper.IsShieldCompatible(f.ctx, &types.MsgRegisterPeer{})
	require.False(t, compatible)

	compatible = f.keeper.IsShieldCompatible(f.ctx, &types.MsgBondVerifier{})
	require.False(t, compatible)

	compatible = f.keeper.IsShieldCompatible(f.ctx, &types.MsgVerifyContent{})
	require.False(t, compatible)
}
