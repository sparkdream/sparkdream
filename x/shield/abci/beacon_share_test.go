package abci

import (
	"testing"

	abcitypes "github.com/cometbft/cometbft/abci/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/types"
)

// runCleanCeremony drives a complaint-free DKG to ACTIVE.
func runCleanCeremony(t *testing.T) *ceremonyFixture {
	t.Helper()
	c := newCeremonyFixture(t)
	regs := map[string]*types.DKGVoteExtension{}
	for _, v := range c.vals {
		regs[v] = c.extend(t, v)
	}
	require.Equal(t, abcitypes.ResponseProcessProposal_ACCEPT, c.inject(t, regs))
	c.setPhase(t, types.DKGPhase_DKG_PHASE_CONTRIBUTING)
	contribs := map[string]*types.DKGVoteExtension{}
	for _, v := range c.vals {
		contribs[v] = c.extend(t, v)
	}
	require.Equal(t, abcitypes.ResponseProcessProposal_ACCEPT, c.inject(t, contribs))
	c.setPhase(t, types.DKGPhase_DKG_PHASE_COMPLAINING)
	c.setPhase(t, types.DKGPhase_DKG_PHASE_JUSTIFYING)
	require.NoError(t, c.k.BeginBlocker(c.ctx.WithBlockHeight(40)))
	st, _ := c.k.GetDKGStateVal(c.ctx)
	require.Equal(t, types.DKGPhase_DKG_PHASE_ACTIVE, st.Phase)
	c.state = st
	return c
}

// TestBeaconShareWithBatchDisabled: with encrypted batch mode off and the
// beacon on, the EndBlocker demands the key of the epoch that just closed,
// validators serve it in their vote extensions, the PreBlocker reconstructs
// it, and the beacon reports AVAILABLE. With the beacon off too, no demand
// exists and no validator produces a share.
func TestBeaconShareWithBatchDisabled(t *testing.T) {
	c := runCleanCeremony(t)
	params, err := c.k.Params.Get(c.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = false
	params.BeaconEnabled = true
	params.ShieldEpochInterval = 10
	params.MaxPendingEpochs = 6
	require.NoError(t, c.k.Params.Set(c.ctx, params))
	require.NoError(t, c.k.SetShieldEpochStateVal(c.ctx, types.ShieldEpochState{CurrentEpoch: 5, EpochStartHeight: 50}))

	// Mid-epoch: nothing closed, nothing demanded, nothing to serve.
	ctx := c.ctx.WithBlockHeight(55)
	require.NoError(t, c.k.EndBlocker(ctx))
	for _, v := range c.vals {
		require.Nil(t, c.extend(t, v), "%s served a share with nothing demanded", v)
	}

	// Epoch 5 closes at 60: the beacon demands it.
	ctx = ctx.WithBlockHeight(60)
	require.NoError(t, c.k.EndBlocker(ctx))
	const epoch = uint64(5)
	_, demanded := c.k.GetDecryptionDemand(ctx, epoch)
	require.True(t, demanded)
	require.Equal(t, types.BeaconStatus_BEACON_STATUS_PENDING, c.k.BeaconStatus(ctx, epoch))

	c.ctx = ctx
	shares := map[string][]byte{}
	for _, v := range c.vals {
		ext := c.extend(t, v)
		require.NotNil(t, ext, "%s produced no share with batch mode off", v)
		require.Equal(t, types.DKGPhase_DKG_PHASE_ACTIVE, ext.Phase)
		require.Equal(t, epoch, ext.DecryptionEpoch)
		require.NoError(t, c.k.VerifyValidatorDecryptionShare(ctx, v, epoch, ext.DecryptionShare), "%s's share", v)
		shares[v] = ext.DecryptionShare
	}

	// Threshold 2 of 3: two shares reconstruct the beacon value.
	data := c.injected(epoch, map[string][]byte{"val1": shares["val1"], "val2": shares["val2"]})
	bz, err := EncodeDKGInjection(data)
	require.NoError(t, err)
	ProcessDKGInjection(ctx, c.k, &abcitypes.RequestFinalizeBlock{Txs: [][]byte{bz}})
	require.Equal(t, types.BeaconStatus_BEACON_STATUS_AVAILABLE, c.k.BeaconStatus(ctx, epoch))
	key, ok := c.k.BeaconKey(ctx, epoch)
	require.True(t, ok)
	require.Len(t, key, types.G1PointLen)

	// Served validators have nothing more to say for that epoch; the one
	// that has not served still does until liveness is judged.
	require.Nil(t, c.extend(t, "val1"))
	require.NotNil(t, c.extend(t, "val3"))

	// Beacon off as well: the next boundary demands nothing.
	params.BeaconEnabled = false
	require.NoError(t, c.k.Params.Set(ctx, params))
	ctx = ctx.WithBlockHeight(70)
	require.NoError(t, c.k.EndBlocker(ctx))
	_, demanded = c.k.GetDecryptionDemand(ctx, 6)
	require.False(t, demanded)
	c.ctx = ctx
	require.Nil(t, c.extend(t, "val2"))
}
