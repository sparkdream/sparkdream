//go:build mainnet || testnet || devnet

package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/types"
)

// Run with: go test -tags devnet -run TestProduction ./x/shield/keeper/
// (make test-shield-production). Every other keeper test assumes the
// testparams build, where an absent verification key skips verification.

// TestProductionGenesisRequiresVerificationKey: the launch gate. Shield
// enabled without a shield_v1 key would refuse every exec forever (no
// message can add a key), so InitGenesis refuses the genesis instead.
func TestProductionGenesisRequiresVerificationKey(t *testing.T) {
	gs := types.DefaultGenesis()
	gs.Params.Enabled = true
	f := initFixtureEmpty(t)
	require.ErrorIs(t, f.keeper.InitGenesis(f.ctx, *gs), types.ErrNoVerificationKey)

	gs.Params.Enabled = false
	f = initFixtureEmpty(t)
	require.NoError(t, f.keeper.InitGenesis(f.ctx, *gs), "disabled shield may ship without a key")

	gs.Params.Enabled = true
	gs.VerificationKeys = []types.VerificationKey{{CircuitId: types.ShieldCircuitID, VkBytes: []byte{1}}}
	f = initFixtureEmpty(t)
	require.NoError(t, f.keeper.InitGenesis(f.ctx, *gs))
}

// TestProductionProofWithoutKeyRejected: with no key stored a production
// build refuses the proof rather than skipping verification.
func TestProductionProofWithoutKeyRejected(t *testing.T) {
	gs := types.DefaultGenesis()
	gs.Params.Enabled = false
	f := initFixtureEmpty(t)
	require.NoError(t, f.keeper.InitGenesis(f.ctx, *gs))

	msg := &types.MsgShieldedExec{
		Proof:              make([]byte, 256),
		Nullifier:          make([]byte, 32),
		RateLimitNullifier: make([]byte, 32),
		MerkleRoot:         make([]byte, 32),
	}
	require.ErrorIs(t, f.keeper.VerifyConsumeProof(f.ctx, msg, 1, 0, 0), types.ErrNoVerificationKey)
}
