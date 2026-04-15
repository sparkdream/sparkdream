package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func TestUnbondVerifier(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	addr := bondTestVerifier(t, f, ms, "unbond-verif")

	// Partial unbond below recovery threshold → DEMOTED
	_, err := ms.UnbondVerifier(f.ctx, &types.MsgUnbondVerifier{Creator: addr, Amount: math.NewInt(300)})
	require.NoError(t, err)

	v, _ := f.keeper.Verifiers.Get(f.ctx, addr)
	require.Equal(t, math.NewInt(200), v.CurrentBond)
	require.Equal(t, types.VerifierBondStatus_VERIFIER_BOND_STATUS_DEMOTED, v.BondStatus)
	require.NotZero(t, v.DemotionCooldownUntil)
}

func TestUnbondVerifierFull(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	addr := bondTestVerifier(t, f, ms, "full-unbond-v")

	_, err := ms.UnbondVerifier(f.ctx, &types.MsgUnbondVerifier{Creator: addr, Amount: math.NewInt(500)})
	require.NoError(t, err)
}

func TestUnbondVerifierExceedsAvailable(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	addr := bondTestVerifier(t, f, ms, "exceed-unbond")

	_, err := ms.UnbondVerifier(f.ctx, &types.MsgUnbondVerifier{Creator: addr, Amount: math.NewInt(600)})
	require.Error(t, err)
	require.Contains(t, err.Error(), "available")
}
