package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func TestBondVerifier(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	addr := testAddr(t, f, "bond-verifier")
	_, err := ms.BondVerifier(f.ctx, &types.MsgBondVerifier{Creator: addr, Amount: math.NewInt(500)})
	require.NoError(t, err)

	v, err := f.keeper.Verifiers.Get(f.ctx, addr)
	require.NoError(t, err)
	require.Equal(t, types.VerifierBondStatus_VERIFIER_BOND_STATUS_NORMAL, v.BondStatus)
	require.Equal(t, math.NewInt(500), v.CurrentBond)
}

func TestBondVerifierRecovery(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	addr := testAddr(t, f, "recovery-verif")
	_, err := ms.BondVerifier(f.ctx, &types.MsgBondVerifier{Creator: addr, Amount: math.NewInt(300)})
	require.NoError(t, err)

	v, _ := f.keeper.Verifiers.Get(f.ctx, addr)
	require.Equal(t, types.VerifierBondStatus_VERIFIER_BOND_STATUS_RECOVERY, v.BondStatus)
}

func TestBondVerifierTopUp(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	addr := testAddr(t, f, "topup-verif")
	_, err := ms.BondVerifier(f.ctx, &types.MsgBondVerifier{Creator: addr, Amount: math.NewInt(300)})
	require.NoError(t, err)

	_, err = ms.BondVerifier(f.ctx, &types.MsgBondVerifier{Creator: addr, Amount: math.NewInt(200)})
	require.NoError(t, err)

	v, _ := f.keeper.Verifiers.Get(f.ctx, addr)
	require.Equal(t, math.NewInt(500), v.CurrentBond)
	require.Equal(t, types.VerifierBondStatus_VERIFIER_BOND_STATUS_NORMAL, v.BondStatus)
}
