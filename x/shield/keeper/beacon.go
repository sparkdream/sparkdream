package keeper

import (
	"context"
	"math"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/shield/types"
)

// Randomness beacon.
//
// The TLE epoch key d = s·H1(epoch) is a threshold BLS signature over the
// epoch number by the DKG master secret: unique for the epoch, unpredictable
// to anyone holding fewer than threshold shares, and impossible to bias
// (there is exactly one valid value, and the pairing check rejects any
// other). Once a DKG has completed and beacon_enabled is set, the EndBlocker
// demands the key of every epoch as it closes, batch or no batch, and the
// usual share pipeline (vote extensions → ProcessProposal → PreBlocker →
// TryReconstructEpochKey, with liveness judged) delivers it a block or two
// later. Consumers read it through BeaconKey / BeaconStatus; x/rep seeds its
// jury draws from it.
//
// Releasing a key for an epoch with no batch leaks nothing: a key only opens
// ciphertexts sealed to its own epoch (tools/tle: the epoch tag is a
// hash-to-curve of the epoch number), and the epoch is closed before the
// demand is recorded, so no later submission can target it. With batch mode
// on, an epoch that carries a batch below min_batch_size is left to the batch
// readiness rule (recordDecryptionDemands), so small batches still wait for
// company rather than sitting decryptable: the beacon value for that epoch
// arrives when its batch is demanded, at the latest after max_pending_epochs.

// BeaconActive reports whether the beacon is producing values: the param is
// on and a DKG has completed (a TLE key set with a master public key exists).
func (k Keeper) BeaconActive(ctx context.Context) bool {
	params, err := k.Params.Get(ctx)
	if err != nil || !params.BeaconEnabled {
		return false
	}
	return k.beaconKeySetReady(ctx)
}

func (k Keeper) beaconKeySetReady(ctx context.Context) bool {
	ks, found := k.GetTLEKeySetVal(ctx)
	return found && len(ks.MasterPublicKey) > 0 && len(ks.ValidatorShares) > 0
}

// CurrentEpoch is the shield epoch now open.
func (k Keeper) CurrentEpoch(ctx context.Context) uint64 {
	return k.GetCurrentEpoch(ctx)
}

// BeaconEpochInterval is the shield epoch length in blocks.
func (k Keeper) BeaconEpochInterval(ctx context.Context) uint64 {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0
	}
	return params.ShieldEpochInterval
}

// BeaconKey returns the beacon value for epoch (the canonical G1 encoding of
// its reconstructed TLE key) when it is available.
func (k Keeper) BeaconKey(ctx context.Context, epoch uint64) ([]byte, bool) {
	dk, found := k.GetShieldEpochDecryptionKeyVal(ctx, epoch)
	if !found || len(dk.DecryptionKey) == 0 {
		return nil, false
	}
	return dk.DecryptionKey, true
}

// BeaconStatus tells a consumer whether epoch's beacon value exists
// (AVAILABLE), is still on its way (PENDING: the epoch is open, or closed
// and demanded, and the threshold has not been reached) or will never come
// (UNAVAILABLE: beacon off or no DKG, epoch older than max_pending_epochs so
// its key is pruned or was never demanded, closed without a demand, or its
// demand was judged for liveness without a key). A consumer that sees
// UNAVAILABLE should fall back; one that sees PENDING should wait, bounded
// by its own deadline. A stored key counts as AVAILABLE whatever the param
// says now, since it is already public.
func (k Keeper) BeaconStatus(ctx context.Context, epoch uint64) types.BeaconStatus {
	if _, ok := k.BeaconKey(ctx, epoch); ok {
		return types.BeaconStatus_BEACON_STATUS_AVAILABLE
	}
	if !k.BeaconActive(ctx) {
		return types.BeaconStatus_BEACON_STATUS_UNAVAILABLE
	}
	current := k.GetCurrentEpoch(ctx)
	if epoch >= current {
		// Open or future: demanded when it closes.
		return types.BeaconStatus_BEACON_STATUS_PENDING
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return types.BeaconStatus_BEACON_STATUS_UNAVAILABLE
	}
	if current > uint64(params.MaxPendingEpochs) && epoch < current-uint64(params.MaxPendingEpochs) {
		return types.BeaconStatus_BEACON_STATUS_UNAVAILABLE
	}
	raw, err := k.DecryptionDemands.Get(ctx, epoch)
	if err != nil {
		if params.EncryptedBatchEnabled && k.hasPendingOpsForEpoch(ctx, epoch) {
			// A small batch still waiting for company; demanded at the
			// latest after max_pending_epochs.
			return types.BeaconStatus_BEACON_STATUS_PENDING
		}
		return types.BeaconStatus_BEACON_STATUS_UNAVAILABLE
	}
	if _, judged := demandHeight(raw); judged {
		// Liveness was judged a full epoch interval after the demand and
		// the threshold never came; a late share may still land, but a
		// consumer should not wait for it.
		return types.BeaconStatus_BEACON_STATUS_UNAVAILABLE
	}
	return types.BeaconStatus_BEACON_STATUS_PENDING
}

// recordBeaconDemand demands the key of closedEpoch, which just closed, when
// the beacon is active. Under batch mode an epoch with pending ops is left
// to recordDecryptionDemands so min_batch_size keeps its meaning.
func (k Keeper) recordBeaconDemand(ctx context.Context, params types.Params, closedEpoch uint64, currentHeight int64) {
	if !params.BeaconEnabled || !k.beaconKeySetReady(ctx) {
		return
	}
	if params.EncryptedBatchEnabled && k.hasPendingOpsForEpoch(ctx, closedEpoch) {
		return
	}
	if err := k.SetDecryptionDemand(ctx, closedEpoch, currentHeight); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("recordBeaconDemand: k.SetDecryptionDemand failed", "err", err)
	}
}

// hasPendingOpsForEpoch reports whether any pending op targets epoch,
// walking keys only.
func (k Keeper) hasPendingOpsForEpoch(ctx context.Context, epoch uint64) bool {
	if k.GetPendingOpCountVal(ctx) == 0 {
		return false
	}
	rng := new(collections.Range[collections.Pair[uint64, uint64]]).
		StartInclusive(collections.Join(epoch, uint64(0)))
	if epoch < math.MaxUint64 {
		rng = rng.EndExclusive(collections.Join(epoch+1, uint64(0)))
	}
	iter, err := k.PendingOps.Iterate(ctx, rng)
	if err != nil {
		return false
	}
	defer iter.Close()
	return iter.Valid()
}
