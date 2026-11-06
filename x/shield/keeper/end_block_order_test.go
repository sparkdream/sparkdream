package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/types"
)

// TestExpiryRunsWithBatchDisabled: turning batch mode off (governance, a
// drift re-key) drains the pending queue instead of freezing it.
func TestExpiryRunsWithBatchDisabled(t *testing.T) {
	f := initFixture(t)
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = false
	params.ShieldEpochInterval = 10
	params.MaxPendingEpochs = 2
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 1, EpochStartHeight: 10}))
	require.NoError(t, f.keeper.SetPendingOp(f.ctx, types.PendingShieldedOp{Id: 1, TargetEpoch: 1, SubmittedAtEpoch: 1, SubmissionNullifier: []byte{9}}))
	require.NoError(t, f.keeper.RecordPendingNullifier(f.ctx, "09"))
	require.NoError(t, f.keeper.SetDecryptionDemand(f.ctx, 1, 15))

	ctx := f.ctx
	for h := int64(20); h <= 40; h += 10 {
		ctx = ctx.WithBlockHeight(h)
		require.NoError(t, f.keeper.EndBlocker(ctx))
	}
	require.Equal(t, uint64(4), f.keeper.GetCurrentEpoch(ctx))
	require.Equal(t, uint64(0), f.keeper.GetPendingOpCountVal(ctx), "op from epoch 1 expired at the epoch-4 boundary")
	require.False(t, f.keeper.IsPendingNullifier(ctx, "09"))
	_, demanded := f.keeper.GetDecryptionDemand(ctx, 1)
	require.False(t, demanded)
	require.Zero(t, f.keeper.GetTLEMissCount(ctx, "val1"), "no liveness judgement while batch mode is off")
}

// TestForcedDemandIsJudged: a lone op's epoch is force-demanded at the
// max_pending_epochs cutoff and falls out of the window at the very boundary
// its liveness would be judged; the judge runs before expiry, so validators
// that ignored it still get the miss.
func TestForcedDemandIsJudged(t *testing.T) {
	f := initFixture(t)
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = true
	// Batch-mode liveness: the beacon would demand every closed epoch, which
	// this test counts as "no demand" (beacon liveness: beacon_test.go).
	params.BeaconEnabled = false
	params.ShieldEpochInterval = 10
	params.MaxPendingEpochs = 2
	params.MinBatchSize = 5 // never ready on size: only the max_pending_epochs rule
	params.TleMissWindow = 10
	params.TleMissTolerance = 5
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	sk := newShamirKeySet(t, []string{"val1", "val2"}, 2)
	require.NoError(t, f.keeper.SetTLEKeySetVal(f.ctx, sk.ks))
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 1, EpochStartHeight: 10}))
	require.NoError(t, f.keeper.SetPendingOp(f.ctx, types.PendingShieldedOp{Id: 1, TargetEpoch: 1, SubmittedAtEpoch: 1, SubmissionNullifier: []byte{1}, EncryptedPayload: []byte("x")}))

	// Epoch 2 boundary: closed epoch 1 has 1 op < min_batch_size, waited 1 < 2.
	ctx := f.ctx.WithBlockHeight(20)
	require.NoError(t, f.keeper.EndBlocker(ctx))
	_, demanded := f.keeper.GetDecryptionDemand(ctx, 1)
	require.False(t, demanded)

	// Epoch 3 boundary: waited max_pending_epochs, forced demand at height 30.
	ctx = ctx.WithBlockHeight(30)
	require.NoError(t, f.keeper.EndBlocker(ctx))
	h, demanded := f.keeper.GetDecryptionDemand(ctx, 1)
	require.True(t, demanded)
	require.Equal(t, int64(30), h)

	// val1 serves; val2 never does.
	_, err = f.keeper.StoreVerifiedDecryptionShare(ctx, "val1", 1, sk.share("val1", 1))
	require.NoError(t, err)

	// Epoch 4 boundary (height 40): the demand is one interval old and epoch
	// 1 is now past the cutoff. Judge first, then expire.
	ctx = ctx.WithBlockHeight(40)
	require.NoError(t, f.keeper.EndBlocker(ctx))
	require.Equal(t, uint64(0), f.keeper.GetTLEMissCount(ctx, "val1"))
	require.Equal(t, uint64(1), f.keeper.GetTLEMissCount(ctx, "val2"), "forced demand judged before expiry forgot it")
	require.Equal(t, uint64(0), f.keeper.GetPendingOpCountVal(ctx))
	_, demanded = f.keeper.GetDecryptionDemand(ctx, 1)
	require.False(t, demanded)
}

// TestMidEpochDemandJudgedOneIntervalLater: liveness is judged every block,
// exactly one epoch interval after the demand, not at the next boundary.
func TestMidEpochDemandJudgedOneIntervalLater(t *testing.T) {
	f := initFixture(t)
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = true
	params.ShieldEpochInterval = 10
	params.TleMissWindow = 10
	params.TleMissTolerance = 5
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	sk := newShamirKeySet(t, []string{"val1"}, 1)
	require.NoError(t, f.keeper.SetTLEKeySetVal(f.ctx, sk.ks))
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 3, EpochStartHeight: 30}))
	require.NoError(t, f.keeper.SetDecryptionDemand(f.ctx, 2, 34)) // recorded mid-epoch

	require.NoError(t, f.keeper.EndBlocker(f.ctx.WithBlockHeight(43)))
	require.False(t, f.keeper.IsDecryptionDemandJudged(f.ctx, 2))
	require.NoError(t, f.keeper.EndBlocker(f.ctx.WithBlockHeight(44)))
	require.True(t, f.keeper.IsDecryptionDemandJudged(f.ctx, 2))
	require.Equal(t, uint64(1), f.keeper.GetTLEMissCount(f.ctx, "val1"))
}

// TestLivenessRingCountsJudgedEpochs: the ring slot is the judged-epoch
// counter, not the epoch number, so sparse demands (epochs 3, 6, 9: all the
// same residue mod a window of 3) each take their own slot.
func TestLivenessRingCountsJudgedEpochs(t *testing.T) {
	f := initFixture(t)
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = true
	// Batch-mode liveness: the beacon would demand every closed epoch, which
	// this test counts as "no demand" (beacon liveness: beacon_test.go).
	params.BeaconEnabled = false
	params.ShieldEpochInterval = 10
	params.TleMissWindow = 3
	params.TleMissTolerance = 10
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	sk := newShamirKeySet(t, []string{"val1"}, 1)
	require.NoError(t, f.keeper.SetTLEKeySetVal(f.ctx, sk.ks))

	ctx := f.ctx
	for _, epoch := range []uint64{3, 6, 9} {
		require.NoError(t, f.keeper.SetShieldEpochStateVal(ctx, types.ShieldEpochState{CurrentEpoch: epoch + 1, EpochStartHeight: int64((epoch + 1) * 10)}))
		require.NoError(t, f.keeper.SetDecryptionDemand(ctx, epoch, int64((epoch+1)*10)))
		ctx = ctx.WithBlockHeight(int64((epoch + 2) * 10))
		require.NoError(t, f.keeper.EndBlocker(ctx))
	}
	require.Equal(t, uint64(3), f.keeper.GetTLEMissCount(ctx, "val1"), "three judged epochs, three slots")
	judged, _ := f.keeper.TLEJudgedEpochs.Get(ctx)
	require.Equal(t, uint64(3), judged)

	// A fourth judged epoch with participation overwrites the oldest slot.
	_, err = f.keeper.StoreVerifiedDecryptionShare(ctx, "val1", 12, sk.share("val1", 12))
	require.NoError(t, err)
	require.NoError(t, f.keeper.SetShieldEpochStateVal(ctx, types.ShieldEpochState{CurrentEpoch: 13, EpochStartHeight: 130}))
	require.NoError(t, f.keeper.SetDecryptionDemand(ctx, 12, 130))
	ctx = ctx.WithBlockHeight(140)
	require.NoError(t, f.keeper.EndBlocker(ctx))
	require.Equal(t, uint64(2), f.keeper.GetTLEMissCount(ctx, "val1"))

	// A new key set starts clean.
	require.NoError(t, f.keeper.ResetTLELiveness(ctx))
	require.Zero(t, f.keeper.GetTLEMissCount(ctx, "val1"))
	judged, _ = f.keeper.TLEJudgedEpochs.Get(ctx)
	require.Zero(t, judged)
}

// TestDriftRekeyRetiresPendingOps: a drift re-key drops what was sealed to
// the retired key set, like MsgTriggerDKG does.
func TestDriftRekeyRetiresPendingOps(t *testing.T) {
	f := initFixture(t)
	require.NoError(t, f.keeper.SetDKGStateVal(f.ctx, types.DKGState{
		Round: 1, Phase: types.DKGPhase_DKG_PHASE_ACTIVE,
		ExpectedValidators: []string{"val_gone1", "val_gone2", "sprkdrmvaloper1aaaaa"},
	}))
	require.NoError(t, f.keeper.SetTLEKeySetVal(f.ctx, types.TLEKeySet{MasterPublicKey: []byte("mpk")}))
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MaxValidatorSetDrift = 50
	params.EncryptedBatchEnabled = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	require.NoError(t, f.keeper.SetPendingOp(f.ctx, types.PendingShieldedOp{Id: 1, TargetEpoch: 2, SubmittedAtEpoch: 2, SubmissionNullifier: []byte{0xaa}}))
	require.NoError(t, f.keeper.RecordPendingNullifier(f.ctx, "aa"))
	require.NoError(t, f.keeper.SetDecryptionDemand(f.ctx, 2, 20))
	require.NoError(t, f.keeper.SetDecryptionShare(f.ctx, types.ShieldDecryptionShare{Epoch: 2, Validator: "v", Share: []byte("s")}))

	require.NoError(t, f.keeper.BeginBlocker(f.ctx))
	require.Equal(t, uint64(0), f.keeper.GetPendingOpCountVal(f.ctx))
	require.False(t, f.keeper.IsPendingNullifier(f.ctx, "aa"))
	_, demanded := f.keeper.GetDecryptionDemand(f.ctx, 2)
	require.False(t, demanded)
	require.Equal(t, uint32(0), f.keeper.CountDecryptionShares(f.ctx, 2))
	var expired bool
	for _, ev := range f.ctx.EventManager().Events() {
		if ev.Type == types.EventTypeShieldBatchExpired {
			expired = true
		}
	}
	require.True(t, expired)
}
