package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/keeper"
	"sparkdream/x/shield/types"
)

// beaconFixture: a completed DKG (real Shamir key set), 10-block epochs,
// epoch 5 open since height 50, batch mode off, beacon on.
func beaconFixture(t *testing.T) (*fixture, shamirKeySet) {
	t.Helper()
	f := initFixture(t)
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = false
	params.BeaconEnabled = true
	params.ShieldEpochInterval = 10
	params.MinBatchSize = 3
	params.MaxPendingEpochs = 4
	params.TleMissWindow = 10
	params.TleMissTolerance = 5
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	sk := newShamirKeySet(t, []string{"val1", "val2", "val3"}, 2)
	require.NoError(t, f.keeper.SetTLEKeySetVal(f.ctx, sk.ks))
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 5, EpochStartHeight: 50}))
	return f, sk
}

func setBeaconParams(t *testing.T, f *fixture, mutate func(*types.Params)) {
	t.Helper()
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	mutate(&params)
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
}

// The EndBlocker demands the key of every epoch as it closes once a DKG has
// completed, with nothing pending in it; not with the beacon off, not
// without a key set, and never for an epoch that is still open.
func TestBeaconDemandsEveryClosedEpoch(t *testing.T) {
	t.Run("closing an idle epoch records a demand", func(t *testing.T) {
		f, _ := beaconFixture(t)
		ctx := f.ctx.WithBlockHeight(55)
		require.NoError(t, f.keeper.EndBlocker(ctx))
		_, demanded := f.keeper.GetDecryptionDemand(ctx, 4)
		require.False(t, demanded, "no boundary crossed yet")

		ctx = ctx.WithBlockHeight(60)
		require.NoError(t, f.keeper.EndBlocker(ctx))
		require.Equal(t, uint64(6), f.keeper.GetCurrentEpoch(ctx))
		h, demanded := f.keeper.GetDecryptionDemand(ctx, 5)
		require.True(t, demanded)
		require.Equal(t, int64(60), h)
		require.Equal(t, uint64(0), f.keeper.GetPendingOpCountVal(ctx))
		require.Equal(t, types.BeaconStatus_BEACON_STATUS_PENDING, f.keeper.BeaconStatus(ctx, 5))
		_, demanded = f.keeper.GetDecryptionDemand(ctx, 6)
		require.False(t, demanded, "the open epoch is never demanded")
	})

	t.Run("beacon off: no demand", func(t *testing.T) {
		f, _ := beaconFixture(t)
		setBeaconParams(t, f, func(p *types.Params) { p.BeaconEnabled = false })
		ctx := f.ctx.WithBlockHeight(60)
		require.NoError(t, f.keeper.EndBlocker(ctx))
		_, demanded := f.keeper.GetDecryptionDemand(ctx, 5)
		require.False(t, demanded)
		require.False(t, f.keeper.BeaconActive(ctx))
		require.Equal(t, types.BeaconStatus_BEACON_STATUS_UNAVAILABLE, f.keeper.BeaconStatus(ctx, 5))
	})

	t.Run("no DKG: no demand", func(t *testing.T) {
		f := initFixture(t)
		setBeaconParams(t, f, func(p *types.Params) {
			p.BeaconEnabled = true
			p.ShieldEpochInterval = 10
		})
		require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 5, EpochStartHeight: 50}))
		ctx := f.ctx.WithBlockHeight(60)
		require.NoError(t, f.keeper.EndBlocker(ctx))
		_, demanded := f.keeper.GetDecryptionDemand(ctx, 5)
		require.False(t, demanded)
		require.False(t, f.keeper.BeaconActive(ctx))
		require.Equal(t, types.BeaconStatus_BEACON_STATUS_UNAVAILABLE, f.keeper.BeaconStatus(ctx, 6))
	})

	t.Run("batch mode: a small batch keeps waiting, an idle epoch is demanded", func(t *testing.T) {
		f, _ := beaconFixture(t)
		setBeaconParams(t, f, func(p *types.Params) { p.EncryptedBatchEnabled = true })
		// Two ops in epoch 5, below min_batch_size 3.
		for i := uint64(1); i <= 2; i++ {
			require.NoError(t, f.keeper.SetPendingOp(f.ctx, types.PendingShieldedOp{
				Id: i, TargetEpoch: 5, SubmittedAtEpoch: 5,
				SubmissionNullifier: []byte{byte(i)}, EncryptedPayload: []byte("x"),
			}))
		}
		ctx := f.ctx.WithBlockHeight(60)
		require.NoError(t, f.keeper.EndBlocker(ctx))
		_, demanded := f.keeper.GetDecryptionDemand(ctx, 5)
		require.False(t, demanded, "the batch readiness rule decides for an epoch with ops")
		require.Equal(t, types.BeaconStatus_BEACON_STATUS_PENDING, f.keeper.BeaconStatus(ctx, 5),
			"its key comes when the batch is demanded, at the latest after max_pending_epochs")

		// Epoch 6 closes idle: demanded by the beacon.
		ctx = ctx.WithBlockHeight(70)
		require.NoError(t, f.keeper.EndBlocker(ctx))
		_, demanded = f.keeper.GetDecryptionDemand(ctx, 6)
		require.True(t, demanded)

		// A third op would have made epoch 5 ready through the batch rule.
		require.NoError(t, f.keeper.SetPendingOp(ctx, types.PendingShieldedOp{
			Id: 3, TargetEpoch: 5, SubmittedAtEpoch: 5, SubmissionNullifier: []byte{3}, EncryptedPayload: []byte("x"),
		}))
		ctx = ctx.WithBlockHeight(71)
		require.NoError(t, f.keeper.EndBlocker(ctx))
		_, demanded = f.keeper.GetDecryptionDemand(ctx, 5)
		require.True(t, demanded)
	})
}

// BeaconStatus / BeaconKey across the three states, and the value is the
// reconstructed TLE key.
func TestBeaconStatusAndKey(t *testing.T) {
	f, sk := beaconFixture(t)
	ctx := f.ctx.WithBlockHeight(60)
	require.NoError(t, f.keeper.EndBlocker(ctx)) // epoch 5 closes and is demanded

	require.True(t, f.keeper.BeaconActive(ctx))
	require.Equal(t, uint64(6), f.keeper.CurrentEpoch(ctx))
	require.Equal(t, uint64(10), f.keeper.BeaconEpochInterval(ctx))
	require.Equal(t, types.BeaconStatus_BEACON_STATUS_PENDING, f.keeper.BeaconStatus(ctx, 6), "open epoch")
	require.Equal(t, types.BeaconStatus_BEACON_STATUS_PENDING, f.keeper.BeaconStatus(ctx, 7), "future epoch")
	require.Equal(t, types.BeaconStatus_BEACON_STATUS_PENDING, f.keeper.BeaconStatus(ctx, 5), "demanded, no key yet")
	require.Equal(t, types.BeaconStatus_BEACON_STATUS_UNAVAILABLE, f.keeper.BeaconStatus(ctx, 4), "closed before the beacon ran, never demanded")
	_, ok := f.keeper.BeaconKey(ctx, 5)
	require.False(t, ok)

	// One share is below the threshold of two.
	_, err := f.keeper.StoreVerifiedDecryptionShare(ctx, "val1", 5, sk.share("val1", 5))
	require.NoError(t, err)
	f.keeper.TryReconstructEpochKey(ctx, 5)
	require.Equal(t, types.BeaconStatus_BEACON_STATUS_PENDING, f.keeper.BeaconStatus(ctx, 5))

	_, err = f.keeper.StoreVerifiedDecryptionShare(ctx, "val2", 5, sk.share("val2", 5))
	require.NoError(t, err)
	ctx = ctx.WithEventManager(sdk.NewEventManager())
	f.keeper.TryReconstructEpochKey(ctx, 5)
	require.Equal(t, types.BeaconStatus_BEACON_STATUS_AVAILABLE, f.keeper.BeaconStatus(ctx, 5))
	key, ok := f.keeper.BeaconKey(ctx, 5)
	require.True(t, ok)
	require.NotEmpty(t, key)
	dk, found := f.keeper.GetShieldEpochDecryptionKeyVal(ctx, 5)
	require.True(t, found)
	require.Equal(t, dk.DecryptionKey, key, "the beacon value is the epoch key's canonical encoding")

	var source string
	for _, ev := range ctx.EventManager().Events() {
		if ev.Type != types.EventTypeShieldDecryptionKeyAvailable {
			continue
		}
		for _, a := range ev.Attributes {
			if a.Key == types.AttributeKeySource {
				source = a.Value
			}
		}
	}
	require.Equal(t, "beacon", source)

	// A stored key stays AVAILABLE even if governance turns the beacon off:
	// it is already public.
	setBeaconParams(t, f, func(p *types.Params) { p.BeaconEnabled = false })
	require.Equal(t, types.BeaconStatus_BEACON_STATUS_AVAILABLE, f.keeper.BeaconStatus(ctx, 5))
	require.Equal(t, types.BeaconStatus_BEACON_STATUS_UNAVAILABLE, f.keeper.BeaconStatus(ctx, 6))
}

// A demanded epoch whose liveness was judged without reaching the threshold
// is UNAVAILABLE (consumers fall back); the judge runs with batch mode off.
func TestBeaconLivenessJudgedWithoutKeyIsUnavailable(t *testing.T) {
	f, sk := beaconFixture(t)
	ctx := f.ctx.WithBlockHeight(60)
	require.NoError(t, f.keeper.EndBlocker(ctx)) // epoch 5 demanded at 60
	_, err := f.keeper.StoreVerifiedDecryptionShare(ctx, "val1", 5, sk.share("val1", 5))
	require.NoError(t, err)
	for h := int64(61); h < 70; h++ {
		require.NoError(t, f.keeper.EndBlocker(ctx.WithBlockHeight(h)))
		require.Equal(t, types.BeaconStatus_BEACON_STATUS_PENDING, f.keeper.BeaconStatus(ctx, 5))
	}
	// One interval after the demand it is judged: val2 and val3 missed.
	ctx = ctx.WithBlockHeight(70)
	require.NoError(t, f.keeper.EndBlocker(ctx))
	require.True(t, f.keeper.IsDecryptionDemandJudged(ctx, 5))
	require.Equal(t, uint64(0), f.keeper.GetTLEMissCount(ctx, "val1"))
	require.Equal(t, uint64(1), f.keeper.GetTLEMissCount(ctx, "val2"))
	require.Equal(t, types.BeaconStatus_BEACON_STATUS_UNAVAILABLE, f.keeper.BeaconStatus(ctx, 5))
}

// Beacon keys are bounded like batch keys: pruned max_pending_epochs after
// their epoch, when the status becomes UNAVAILABLE.
func TestBeaconKeysArePruned(t *testing.T) {
	f, sk := beaconFixture(t)
	ctx := f.ctx.WithBlockHeight(60)
	require.NoError(t, f.keeper.EndBlocker(ctx))
	for _, v := range []string{"val1", "val2"} {
		_, err := f.keeper.StoreVerifiedDecryptionShare(ctx, v, 5, sk.share(v, 5))
		require.NoError(t, err)
	}
	f.keeper.TryReconstructEpochKey(ctx, 5)
	require.Equal(t, types.BeaconStatus_BEACON_STATUS_AVAILABLE, f.keeper.BeaconStatus(ctx, 5))

	// Epochs 6, 7, 8 close (current 9): 9 - 4 = 5, still kept.
	for h := int64(70); h <= 90; h += 10 {
		ctx = ctx.WithBlockHeight(h)
		require.NoError(t, f.keeper.EndBlocker(ctx))
	}
	require.Equal(t, uint64(9), f.keeper.GetCurrentEpoch(ctx))
	require.Equal(t, types.BeaconStatus_BEACON_STATUS_AVAILABLE, f.keeper.BeaconStatus(ctx, 5))

	// Current 10: cutoff 6, epoch 5's key and demand are gone.
	ctx = ctx.WithBlockHeight(100)
	require.NoError(t, f.keeper.EndBlocker(ctx))
	_, ok := f.keeper.BeaconKey(ctx, 5)
	require.False(t, ok)
	_, demanded := f.keeper.GetDecryptionDemand(ctx, 5)
	require.False(t, demanded)
	require.Equal(t, types.BeaconStatus_BEACON_STATUS_UNAVAILABLE, f.keeper.BeaconStatus(ctx, 5))
	// Every closed epoch in the window was demanded by the beacon.
	for e := uint64(6); e <= 9; e++ {
		_, demanded := f.keeper.GetDecryptionDemand(ctx, e)
		require.True(t, demanded, "epoch %d", e)
	}
}

// The beacon query: epoch 0 resolves to the latest closed epoch.
func TestBeaconQuery(t *testing.T) {
	f, sk := beaconFixture(t)
	ctx := f.ctx.WithBlockHeight(60)
	require.NoError(t, f.keeper.EndBlocker(ctx))
	q := keeper.NewQueryServerImpl(f.keeper)

	resp, err := q.Beacon(ctx, &types.QueryBeaconRequest{})
	require.NoError(t, err)
	require.Equal(t, uint64(5), resp.Epoch)
	require.Equal(t, uint64(6), resp.CurrentEpoch)
	require.Equal(t, types.BeaconStatus_BEACON_STATUS_PENDING, resp.Status)
	require.Empty(t, resp.KeyHex)

	for _, v := range []string{"val2", "val3"} {
		_, err := f.keeper.StoreVerifiedDecryptionShare(ctx, v, 5, sk.share(v, 5))
		require.NoError(t, err)
	}
	f.keeper.TryReconstructEpochKey(ctx, 5)
	resp, err = q.Beacon(ctx, &types.QueryBeaconRequest{Epoch: 5})
	require.NoError(t, err)
	require.Equal(t, types.BeaconStatus_BEACON_STATUS_AVAILABLE, resp.Status)
	require.Len(t, resp.KeyHex, 2*types.G1PointLen)
	require.Equal(t, int64(60), resp.ReconstructedAtHeight)

	resp, err = q.Beacon(ctx, &types.QueryBeaconRequest{Epoch: 3})
	require.NoError(t, err)
	require.Equal(t, types.BeaconStatus_BEACON_STATUS_UNAVAILABLE, resp.Status)
}
