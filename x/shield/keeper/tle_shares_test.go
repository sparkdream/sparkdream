package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4"
	"go.dedis.ch/kyber/v4/share"
	"go.dedis.ch/kyber/v4/util/random"

	"sparkdream/tools/tle"
	"sparkdream/x/shield/types"
)

// shamirKeySet is a real threshold key set: s Shamir-shared across n
// validators, G1/G2 public shares, and the per-validator secret shares a
// test uses to derive decryption shares.
type shamirKeySet struct {
	master  kyber.Scalar
	secrets map[string]kyber.Scalar
	ks      types.TLEKeySet
}

func newShamirKeySet(t *testing.T, vals []string, threshold int) shamirKeySet {
	t.Helper()
	master := tle.Suite.G1().Scalar().Pick(random.New())
	poly := share.NewPriPoly(tle.Suite.G1(), threshold, master, random.New())
	pri := poly.Shares(len(vals))
	out := shamirKeySet{master: master, secrets: map[string]kyber.Scalar{}}
	out.ks = types.TLEKeySet{
		MasterPublicKey:      tle.MasterPublicKeyG1(master),
		MasterPublicKeyG2:    tle.MasterPublicKeyG2(master),
		ThresholdNumerator:   uint64(threshold),
		ThresholdDenominator: uint64(len(vals)),
	}
	for i, v := range vals {
		out.secrets[v] = pri[i].V
		g1, _ := tle.Suite.G1().Point().Mul(pri[i].V, nil).MarshalBinary()
		g2, _ := tle.Suite.G2().Point().Mul(pri[i].V, nil).MarshalBinary()
		out.ks.ValidatorShares = append(out.ks.ValidatorShares, &types.TLEValidatorPublicShare{
			ValidatorAddress: v, PublicShare: g1, PublicShareG2: g2, ShareIndex: uint32(i + 1),
		})
	}
	require.NoError(t, out.ks.Validate())
	return out
}

func (s shamirKeySet) share(val string, epoch uint64) []byte {
	return tle.DecryptionShare(s.secrets[val], epoch)
}

func TestVerifyValidatorDecryptionShare(t *testing.T) {
	f := initFixture(t)
	vals := []string{"val1", "val2", "val3"}
	sk := newShamirKeySet(t, vals, 2)

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MaxPendingEpochs = 3
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 10, EpochStartHeight: 100}))

	t.Run("no key set", func(t *testing.T) {
		err := f.keeper.VerifyValidatorDecryptionShare(f.ctx, "val1", 9, sk.share("val1", 9))
		require.ErrorIs(t, err, types.ErrInvalidDecryptionShare)
	})

	require.NoError(t, f.keeper.SetTLEKeySetVal(f.ctx, sk.ks))

	t.Run("not demanded", func(t *testing.T) {
		err := f.keeper.VerifyValidatorDecryptionShare(f.ctx, "val1", 9, sk.share("val1", 9))
		require.ErrorContains(t, err, "no decryption demanded")
	})

	require.NoError(t, f.keeper.SetDecryptionDemand(f.ctx, 9, 100))
	require.NoError(t, f.keeper.SetDecryptionDemand(f.ctx, 10, 100))
	require.NoError(t, f.keeper.SetDecryptionDemand(f.ctx, 5, 60))

	t.Run("valid share", func(t *testing.T) {
		require.NoError(t, f.keeper.VerifyValidatorDecryptionShare(f.ctx, "val1", 9, sk.share("val1", 9)))
	})
	t.Run("unknown validator", func(t *testing.T) {
		err := f.keeper.VerifyValidatorDecryptionShare(f.ctx, "stranger", 9, sk.share("val1", 9))
		require.ErrorContains(t, err, "holds no share")
	})
	t.Run("current epoch is not closed", func(t *testing.T) {
		err := f.keeper.VerifyValidatorDecryptionShare(f.ctx, "val1", 10, sk.share("val1", 10))
		require.ErrorContains(t, err, "not closed")
	})
	t.Run("older than max_pending_epochs", func(t *testing.T) {
		err := f.keeper.VerifyValidatorDecryptionShare(f.ctx, "val1", 5, sk.share("val1", 5))
		require.ErrorContains(t, err, "older than max_pending_epochs")
	})
	t.Run("another validator's share is attributed to the wrong key", func(t *testing.T) {
		err := f.keeper.VerifyValidatorDecryptionShare(f.ctx, "val1", 9, sk.share("val2", 9))
		require.ErrorContains(t, err, "does not match")
	})
	t.Run("share for a different epoch", func(t *testing.T) {
		err := f.keeper.VerifyValidatorDecryptionShare(f.ctx, "val1", 9, sk.share("val1", 8))
		require.ErrorContains(t, err, "does not match")
	})
	t.Run("random point", func(t *testing.T) {
		forged, _ := tle.Suite.G1().Point().Pick(random.New()).MarshalBinary()
		err := f.keeper.VerifyValidatorDecryptionShare(f.ctx, "val1", 9, forged)
		require.ErrorContains(t, err, "does not match")
	})
	t.Run("raw master share for all epochs", func(t *testing.T) {
		// Submitting s_i*G1 instead of s_i*H1(epoch) fails the pairing.
		err := f.keeper.VerifyValidatorDecryptionShare(f.ctx, "val1", 9, sk.ks.ValidatorShares[0].PublicShare)
		require.ErrorContains(t, err, "does not match")
	})
}

func TestStoreSharesAndReconstruct(t *testing.T) {
	f := initFixture(t)
	vals := []string{"val1", "val2", "val3", "val4"}
	sk := newShamirKeySet(t, vals, 3)
	require.NoError(t, f.keeper.SetTLEKeySetVal(f.ctx, sk.ks))
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 4, EpochStartHeight: 40}))
	require.NoError(t, f.keeper.SetDecryptionDemand(f.ctx, 3, 40))

	ct, err := tle.Encrypt(sk.ks.MasterPublicKeyG2, 3, []byte("ballot"))
	require.NoError(t, err)

	// Two shares: below threshold, no key.
	for _, v := range vals[:2] {
		stored, err := f.keeper.StoreVerifiedDecryptionShare(f.ctx, v, 3, sk.share(v, 3))
		require.NoError(t, err)
		require.True(t, stored)
	}
	f.keeper.TryReconstructEpochKey(f.ctx, 3)
	_, found := f.keeper.GetShieldEpochDecryptionKeyVal(f.ctx, 3)
	require.False(t, found)

	// Duplicate is a no-op.
	stored, err := f.keeper.StoreVerifiedDecryptionShare(f.ctx, "val1", 3, sk.share("val1", 3))
	require.NoError(t, err)
	require.False(t, stored)
	require.Equal(t, uint32(2), f.keeper.CountDecryptionShares(f.ctx, 3))

	// Third share reaches the threshold: the key is reconstructed and opens
	// what a client sealed to the master public key.
	_, err = f.keeper.StoreVerifiedDecryptionShare(f.ctx, "val4", 3, sk.share("val4", 3))
	require.NoError(t, err)
	f.keeper.TryReconstructEpochKey(f.ctx, 3)
	key, found := f.keeper.GetShieldEpochDecryptionKeyVal(f.ctx, 3)
	require.True(t, found)
	require.Equal(t, tle.EpochDecryptionKey(sk.master, 3), key.DecryptionKey)
	pt, err := tle.Decrypt(key.DecryptionKey, 3, ct)
	require.NoError(t, err)
	require.Equal(t, []byte("ballot"), pt)

	var available bool
	for _, ev := range f.ctx.EventManager().Events() {
		if ev.Type == types.EventTypeShieldDecryptionKeyAvailable {
			available = true
		}
	}
	require.True(t, available)

	// Shares for another epoch are not counted (prefix range).
	require.Equal(t, uint32(0), f.keeper.CountDecryptionShares(f.ctx, 2))
	require.Len(t, f.keeper.GetDecryptionSharesForEpoch(f.ctx, 3), 3)
}

func TestNextEpochNeedingShare(t *testing.T) {
	f := initFixture(t)
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MaxPendingEpochs = 4
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 10, EpochStartHeight: 100}))

	// Nothing demanded: nothing to do.
	_, ok := f.keeper.NextEpochNeedingShare(f.ctx, "val1")
	require.False(t, ok)

	require.NoError(t, f.keeper.SetDecryptionDemand(f.ctx, 4, 50))   // too old (cutoff 6)
	require.NoError(t, f.keeper.SetDecryptionDemand(f.ctx, 6, 70))   // key reconstructed AND judged: done
	require.NoError(t, f.keeper.SetDecryptionDemand(f.ctx, 7, 80))   // key reconstructed, not judged yet: still asked
	require.NoError(t, f.keeper.SetDecryptionDemand(f.ctx, 8, 90))   // val1 already served it
	require.NoError(t, f.keeper.SetDecryptionDemand(f.ctx, 9, 100))  // this one
	require.NoError(t, f.keeper.SetDecryptionDemand(f.ctx, 10, 100)) // not closed
	require.NoError(t, f.keeper.SetShieldEpochDecryptionKey(f.ctx, types.ShieldEpochDecryptionKey{Epoch: 6, DecryptionKey: []byte("k")}))
	require.NoError(t, f.keeper.DecryptionDemands.Set(f.ctx, 6, -(70+1))) // judged sentinel
	require.NoError(t, f.keeper.SetShieldEpochDecryptionKey(f.ctx, types.ShieldEpochDecryptionKey{Epoch: 7, DecryptionKey: []byte("k")}))
	require.NoError(t, f.keeper.SetDecryptionShare(f.ctx, types.ShieldDecryptionShare{Epoch: 8, Validator: "val1", Share: []byte("s")}))

	// Epoch 7 already has its key, but liveness has not judged it: a
	// validator that was not in the reconstructing commit is still asked, so
	// it is never judged for a share the chain stopped requesting.
	epoch, ok := f.keeper.NextEpochNeedingShare(f.ctx, "val1")
	require.True(t, ok)
	require.Equal(t, uint64(7), epoch)
	require.NoError(t, f.keeper.SetDecryptionShare(f.ctx, types.ShieldDecryptionShare{Epoch: 7, Validator: "val1", Share: []byte("s")}))

	epoch, ok = f.keeper.NextEpochNeedingShare(f.ctx, "val1")
	require.True(t, ok)
	require.Equal(t, uint64(9), epoch)

	// val2 has not served epoch 7 yet: oldest first.
	epoch, ok = f.keeper.NextEpochNeedingShare(f.ctx, "val2")
	require.True(t, ok)
	require.Equal(t, uint64(7), epoch)
}

func TestDecryptionDemandJudgedSentinel(t *testing.T) {
	f := initFixture(t)
	require.NoError(t, f.keeper.SetDecryptionDemand(f.ctx, 1, 0))
	h, ok := f.keeper.GetDecryptionDemand(f.ctx, 1)
	require.True(t, ok)
	require.Equal(t, int64(0), h)
	require.False(t, f.keeper.IsDecryptionDemandJudged(f.ctx, 1))
	// Idempotent: first height wins.
	require.NoError(t, f.keeper.SetDecryptionDemand(f.ctx, 1, 77))
	h, _ = f.keeper.GetDecryptionDemand(f.ctx, 1)
	require.Equal(t, int64(0), h)

	// Judge it through the EndBlocker path.
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = true
	params.ShieldEpochInterval = 10
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 2, EpochStartHeight: 10}))
	ctx := f.ctx.WithBlockHeight(20)
	require.NoError(t, f.keeper.EndBlocker(ctx))
	require.True(t, f.keeper.IsDecryptionDemandJudged(ctx, 1))
	h, ok = f.keeper.GetDecryptionDemand(ctx, 1)
	require.True(t, ok)
	require.Equal(t, int64(0), h, "height 0 survives the judged sentinel")

	// Round-trips through genesis with the sentinel intact.
	exported, err := f.keeper.ExportGenesis(ctx)
	require.NoError(t, err)
	require.Len(t, exported.DecryptionDemands, 1)
	f2 := initFixtureEmpty(t)
	require.NoError(t, f2.keeper.InitGenesis(f2.ctx, *exported))
	require.True(t, f2.keeper.IsDecryptionDemandJudged(f2.ctx, 1))
}

// TestEndBlockerDemandAndBatchReadiness covers the release rule: shares are
// demanded (and a seeded key is used) only once a closed epoch's batch is
// ready, where ready is min_batch_size alone, min_batch_size across all
// closed epochs together, or max_pending_epochs of waiting.
func TestEndBlockerDemandAndBatchReadiness(t *testing.T) {
	setup := func(t *testing.T) *fixture {
		f := initFixture(t)
		params, err := f.keeper.Params.Get(f.ctx)
		require.NoError(t, err)
		params.EncryptedBatchEnabled = true
		params.ShieldEpochInterval = 10
		params.MinBatchSize = 3
		params.MaxPendingEpochs = 4
		require.NoError(t, f.keeper.Params.Set(f.ctx, params))
		require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 5, EpochStartHeight: 50}))
		return f
	}
	addOps := func(t *testing.T, f *fixture, epoch uint64, n int, firstID uint64) {
		for i := 0; i < n; i++ {
			id := firstID + uint64(i)
			require.NoError(t, f.keeper.SetPendingOp(f.ctx, types.PendingShieldedOp{
				Id: id, TargetEpoch: epoch, SubmittedAtEpoch: epoch,
				SubmissionNullifier: []byte{byte(id)}, EncryptedPayload: []byte("x"),
			}))
		}
	}

	t.Run("small batch waits, no demand", func(t *testing.T) {
		f := setup(t)
		addOps(t, f, 4, 2, 1)
		ctx := f.ctx.WithBlockHeight(55) // mid-epoch 5
		require.NoError(t, f.keeper.EndBlocker(ctx))
		_, demanded := f.keeper.GetDecryptionDemand(ctx, 4)
		require.False(t, demanded)
		require.Equal(t, uint64(2), f.keeper.GetPendingOpCountVal(ctx))
	})

	t.Run("min_batch_size reached mid-epoch demands immediately", func(t *testing.T) {
		f := setup(t)
		addOps(t, f, 4, 3, 1)
		ctx := f.ctx.WithBlockHeight(55)
		require.NoError(t, f.keeper.EndBlocker(ctx))
		h, demanded := f.keeper.GetDecryptionDemand(ctx, 4)
		require.True(t, demanded)
		require.Equal(t, int64(55), h)
		// Current epoch's own ops are never demanded.
		addOps(t, f, 5, 5, 10)
		require.NoError(t, f.keeper.EndBlocker(ctx))
		_, demanded = f.keeper.GetDecryptionDemand(ctx, 5)
		require.False(t, demanded)
	})

	t.Run("closed epochs count together", func(t *testing.T) {
		f := setup(t)
		addOps(t, f, 3, 2, 1)
		addOps(t, f, 4, 1, 3)
		ctx := f.ctx.WithBlockHeight(55)
		require.NoError(t, f.keeper.EndBlocker(ctx))
		_, d3 := f.keeper.GetDecryptionDemand(ctx, 3)
		_, d4 := f.keeper.GetDecryptionDemand(ctx, 4)
		require.True(t, d3)
		require.True(t, d4)
	})

	t.Run("max_pending_epochs forces a lone op", func(t *testing.T) {
		f := setup(t)
		addOps(t, f, 1, 1, 1) // waited 4 epochs at current 5
		addOps(t, f, 4, 1, 2)
		ctx := f.ctx.WithBlockHeight(55)
		require.NoError(t, f.keeper.EndBlocker(ctx))
		_, d1 := f.keeper.GetDecryptionDemand(ctx, 1)
		_, d4 := f.keeper.GetDecryptionDemand(ctx, 4)
		require.True(t, d1)
		require.False(t, d4)
	})

	t.Run("key alone does not execute an undemanded batch; demand plus key does", func(t *testing.T) {
		f := setup(t)
		addOps(t, f, 4, 2, 1)
		require.NoError(t, f.keeper.SetShieldEpochDecryptionKey(f.ctx, types.ShieldEpochDecryptionKey{Epoch: 4, DecryptionKey: []byte("seeded")}))
		ctx := f.ctx.WithBlockHeight(55)
		require.NoError(t, f.keeper.EndBlocker(ctx))
		require.Equal(t, uint64(2), f.keeper.GetPendingOpCountVal(ctx), "two ops wait for company")

		addOps(t, f, 4, 1, 3)
		require.NoError(t, f.keeper.EndBlocker(ctx))
		require.Equal(t, uint64(0), f.keeper.GetPendingOpCountVal(ctx), "ready: executed in the same block, mid-epoch")
		var executed bool
		for _, ev := range ctx.EventManager().Events() {
			if ev.Type == types.EventTypeShieldBatchExecuted {
				executed = true
			}
		}
		require.True(t, executed)
	})

	t.Run("expiry drops ops and their demand", func(t *testing.T) {
		f := setup(t)
		addOps(t, f, 1, 1, 1)
		ctx := f.ctx.WithBlockHeight(55)
		require.NoError(t, f.keeper.EndBlocker(ctx)) // demanded (forced)
		_, d := f.keeper.GetDecryptionDemand(ctx, 1)
		require.True(t, d)
		// Cross into epoch 6: cutoff 2, epoch 1 expires.
		ctx = ctx.WithBlockHeight(60)
		require.NoError(t, f.keeper.EndBlocker(ctx))
		require.Equal(t, uint64(0), f.keeper.GetPendingOpCountVal(ctx))
		_, d = f.keeper.GetDecryptionDemand(ctx, 1)
		require.False(t, d)
	})
}

// TestTLELivenessOnlyForDemandedEpochs: an idle epoch (no batch) must not
// count as a miss, and a demand is judged exactly once, one interval later.
func TestTLELivenessOnlyForDemandedEpochs(t *testing.T) {
	f := initFixture(t)
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = true
	params.ShieldEpochInterval = 10
	params.MinBatchSize = 1
	params.TleMissWindow = 10
	params.TleMissTolerance = 5
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	sk := newShamirKeySet(t, []string{"val1", "val2"}, 2)
	require.NoError(t, f.keeper.SetTLEKeySetVal(f.ctx, sk.ks))
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 1, EpochStartHeight: 10}))

	// Idle boundaries: no demand, no misses.
	ctx := f.ctx
	for h := int64(20); h <= 40; h += 10 {
		ctx = ctx.WithBlockHeight(h)
		require.NoError(t, f.keeper.EndBlocker(ctx))
	}
	require.Equal(t, uint64(0), f.keeper.GetTLEMissCount(ctx, "val1"))
	require.Equal(t, uint64(0), f.keeper.GetTLEMissCount(ctx, "val2"))
	require.Equal(t, uint64(4), f.keeper.GetCurrentEpoch(ctx))

	// An op in epoch 4 becomes a demand at the epoch-5 boundary (height 50).
	require.NoError(t, f.keeper.SetPendingOp(ctx, types.PendingShieldedOp{Id: 1, TargetEpoch: 4, SubmittedAtEpoch: 4, SubmissionNullifier: []byte{1}, EncryptedPayload: []byte("x")}))
	ctx = ctx.WithBlockHeight(50)
	require.NoError(t, f.keeper.EndBlocker(ctx))
	h, demanded := f.keeper.GetDecryptionDemand(ctx, 4)
	require.True(t, demanded)
	require.Equal(t, int64(50), h)
	// Not judged at this boundary: validators get the whole interval.
	require.Equal(t, uint64(0), f.keeper.GetTLEMissCount(ctx, "val1"))

	// val1 serves it (as the PreBlocker would), val2 does not.
	_, err = f.keeper.StoreVerifiedDecryptionShare(ctx, "val1", 4, sk.share("val1", 4))
	require.NoError(t, err)

	ctx = ctx.WithBlockHeight(60)
	require.NoError(t, f.keeper.EndBlocker(ctx))
	require.Equal(t, uint64(0), f.keeper.GetTLEMissCount(ctx, "val1"))
	require.Equal(t, uint64(1), f.keeper.GetTLEMissCount(ctx, "val2"))
	require.True(t, f.keeper.IsDecryptionDemandJudged(ctx, 4))

	// Later boundaries do not judge it again.
	ctx = ctx.WithBlockHeight(70)
	require.NoError(t, f.keeper.EndBlocker(ctx))
	require.Equal(t, uint64(1), f.keeper.GetTLEMissCount(ctx, "val2"))

	// A late share is still accepted while the demand stands.
	require.NoError(t, f.keeper.VerifyValidatorDecryptionShare(ctx, "val2", 4, sk.share("val2", 4)))
}

func TestTriggerDKGRetiresOldKeyMaterial(t *testing.T) {
	f, ms := initMsgServer(t)
	authority, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("gov"))
	require.NoError(t, err)

	require.NoError(t, f.keeper.SetPendingOp(f.ctx, types.PendingShieldedOp{Id: 1, TargetEpoch: 2, SubmittedAtEpoch: 2, SubmissionNullifier: []byte{0xaa}}))
	require.NoError(t, f.keeper.RecordPendingNullifier(f.ctx, "aa"))
	require.NoError(t, f.keeper.SetDecryptionShare(f.ctx, types.ShieldDecryptionShare{Epoch: 2, Validator: "v", Share: []byte("s")}))
	require.NoError(t, f.keeper.SetShieldEpochDecryptionKey(f.ctx, types.ShieldEpochDecryptionKey{Epoch: 1, DecryptionKey: []byte("k")}))
	require.NoError(t, f.keeper.SetDecryptionDemand(f.ctx, 2, 20))

	_, err = ms.TriggerDKG(f.ctx, &types.MsgTriggerDkg{Authority: authority})
	require.NoError(t, err)

	require.Equal(t, uint64(0), f.keeper.GetPendingOpCountVal(f.ctx))
	require.False(t, f.keeper.IsPendingNullifier(f.ctx, "aa"))
	require.Equal(t, uint32(0), f.keeper.CountDecryptionShares(f.ctx, 2))
	_, found := f.keeper.GetShieldEpochDecryptionKeyVal(f.ctx, 1)
	require.False(t, found)
	_, found = f.keeper.GetDecryptionDemand(f.ctx, 2)
	require.False(t, found)

	var expired bool
	for _, ev := range f.ctx.EventManager().Events() {
		if ev.Type == types.EventTypeShieldBatchExpired {
			expired = true
			for _, a := range ev.Attributes {
				if a.Key == types.AttributeKeyCount {
					require.Equal(t, "1", a.Value)
				}
			}
		}
	}
	require.True(t, expired, "dropped ops are announced")
}

var _ sdk.Context
