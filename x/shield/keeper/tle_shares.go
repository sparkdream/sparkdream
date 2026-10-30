package keeper

import (
	"context"
	"encoding/hex"
	"fmt"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/shield/types"
)

// VerifyValidatorDecryptionShare is the single acceptance rule for a
// decryption share, applied in VerifyVoteExtension, ProcessProposal and the
// PreBlocker alike:
//
//   - the validator holds a share in the active TLE key set;
//   - the epoch is closed (strictly before the current one) and still inside
//     the max_pending_epochs window;
//   - the EndBlocker has demanded shares for that epoch, so a key is never
//     reconstructed on chain before the batch is ready to execute;
//   - the pairing check proves share = s_i·H1(epoch) for this validator.
//
// A share that fails here is a Byzantine or buggy validator's, never an
// honest one's, so callers may reject the vote extension or proposal and
// the PreBlocker leaves it out of reconstruction (which the liveness check
// then counts as a miss).
func (k Keeper) VerifyValidatorDecryptionShare(ctx context.Context, validatorAddr string, epoch uint64, share []byte) error {
	ks, found := k.GetTLEKeySetVal(ctx)
	if !found || len(ks.MasterPublicKey) == 0 {
		return errorsmod.Wrap(types.ErrInvalidDecryptionShare, "no TLE key set")
	}
	var vs *types.TLEValidatorPublicShare
	for _, s := range ks.ValidatorShares {
		if s.ValidatorAddress == validatorAddr {
			vs = s
			break
		}
	}
	if vs == nil {
		return errorsmod.Wrapf(types.ErrInvalidDecryptionShare, "%s holds no share in the key set", validatorAddr)
	}
	if err := k.checkShareEpoch(ctx, epoch); err != nil {
		return err
	}
	if err := verifyDecryptionShare(share, vs.PublicShareG2, epoch); err != nil {
		return errorsmod.Wrap(types.ErrInvalidDecryptionShare, err.Error())
	}
	return nil
}

// checkShareEpoch enforces the epoch window and the demand gate.
func (k Keeper) checkShareEpoch(ctx context.Context, epoch uint64) error {
	current := k.GetCurrentEpoch(ctx)
	if epoch >= current {
		return errorsmod.Wrapf(types.ErrInvalidDecryptionShare, "epoch %d is not closed (current %d)", epoch, current)
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	if current > uint64(params.MaxPendingEpochs) && epoch < current-uint64(params.MaxPendingEpochs) {
		return errorsmod.Wrapf(types.ErrInvalidDecryptionShare, "epoch %d is older than max_pending_epochs", epoch)
	}
	if _, demanded := k.GetDecryptionDemand(ctx, epoch); !demanded {
		return errorsmod.Wrapf(types.ErrInvalidDecryptionShare, "no decryption demanded for epoch %d", epoch)
	}
	return nil
}

// Demand values: a non-negative value is the height the demand was recorded
// at and liveness has not been judged; once judged the value becomes
// -(height+1) (so height 0 stays distinguishable). The demand itself stays
// until the epoch's ops expire so late shares are still accepted.
func demandHeight(v int64) (height int64, judged bool) {
	if v < 0 {
		return -(v + 1), true
	}
	return v, false
}

// GetDecryptionDemand returns the height at which shares were demanded for a
// closed epoch, if they were.
func (k Keeper) GetDecryptionDemand(ctx context.Context, epoch uint64) (int64, bool) {
	v, err := k.DecryptionDemands.Get(ctx, epoch)
	if err != nil {
		return 0, false
	}
	h, _ := demandHeight(v)
	return h, true
}

// IsDecryptionDemandJudged reports whether TLE liveness was already judged
// for the epoch's demand.
func (k Keeper) IsDecryptionDemandJudged(ctx context.Context, epoch uint64) bool {
	v, err := k.DecryptionDemands.Get(ctx, epoch)
	if err != nil {
		return false
	}
	_, judged := demandHeight(v)
	return judged
}

// SetDecryptionDemand records a demand (idempotent: the first height wins).
func (k Keeper) SetDecryptionDemand(ctx context.Context, epoch uint64, height int64) error {
	if _, exists := k.GetDecryptionDemand(ctx, epoch); exists {
		return nil
	}
	return k.DecryptionDemands.Set(ctx, epoch, height)
}

// RemoveDecryptionDemand forgets a demand (after liveness was judged, or when
// the epoch's ops expired).
func (k Keeper) RemoveDecryptionDemand(ctx context.Context, epoch uint64) error {
	return k.DecryptionDemands.Remove(ctx, epoch)
}

// IterateDecryptionDemands walks demands in ascending epoch order with the
// raw stored value (see demandHeight).
func (k Keeper) IterateDecryptionDemands(ctx context.Context, fn func(epoch uint64, raw int64) bool) error {
	iter, err := k.DecryptionDemands.Iterate(ctx, nil)
	if err != nil {
		return err
	}
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		kv, err := iter.KeyValue()
		if err != nil {
			return err
		}
		if fn(kv.Key, kv.Value) {
			break
		}
	}
	return nil
}

// ClearDecryptionDemands removes every demand.
func (k Keeper) ClearDecryptionDemands(ctx context.Context) error {
	var epochs []uint64
	if err := k.IterateDecryptionDemands(ctx, func(epoch uint64, _ int64) bool {
		epochs = append(epochs, epoch)
		return false
	}); err != nil {
		return err
	}
	for _, e := range epochs {
		if err := k.DecryptionDemands.Remove(ctx, e); err != nil {
			return err
		}
	}
	return nil
}

// NextEpochNeedingShare tells a validator which closed epoch, if any, it
// should produce a decryption share for: the oldest demanded epoch inside
// the window with no share from this validator, unless the epoch already has
// its key *and* its liveness has been judged. A validator keeps serving an
// epoch whose key exists until it is judged, so liveness never counts a
// share the chain stopped asking for as a miss; it keeps serving a judged
// epoch without a key so a late threshold can still reconstruct it. One
// epoch per vote keeps extensions small; the next block covers the next
// epoch.
func (k Keeper) NextEpochNeedingShare(ctx context.Context, validatorAddr string) (uint64, bool) {
	current := k.GetCurrentEpoch(ctx)
	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0, false
	}
	var cutoff uint64
	if current > uint64(params.MaxPendingEpochs) {
		cutoff = current - uint64(params.MaxPendingEpochs)
	}
	var (
		result uint64
		ok     bool
	)
	if err := k.IterateDecryptionDemands(ctx, func(epoch uint64, raw int64) bool {
		if epoch < cutoff || epoch >= current {
			return false
		}
		_, judged := demandHeight(raw)
		if _, haveKey := k.GetShieldEpochDecryptionKeyVal(ctx, epoch); haveKey && judged {
			return false
		}
		if _, mine := k.GetDecryptionShare(ctx, epoch, validatorAddr); mine {
			return false
		}
		result, ok = epoch, true
		return true
	}); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("NextEpochNeedingShare: k.IterateDecryptionDemands failed", "err", err)
	}
	return result, ok
}

// RetireKeySetMaterial drops everything derived from the current TLE key set:
// decryption shares, reconstructed epoch keys, decryption demands, the TLE
// liveness ring, and every pending op (sealed to the retiring key, it can
// never be opened under the next one) with its submission nullifier. Returns
// the number of pending ops dropped. Used by MsgTriggerDKG and by drift
// re-keying.
func (k Keeper) RetireKeySetMaterial(ctx context.Context) (int, error) {
	if err := k.ClearDecryptionShares(ctx); err != nil {
		return 0, err
	}
	if err := k.ClearDecryptionKeys(ctx); err != nil {
		return 0, err
	}
	if err := k.ClearDecryptionDemands(ctx); err != nil {
		return 0, err
	}
	if err := k.ResetTLELiveness(ctx); err != nil {
		return 0, err
	}
	var dropped []types.PendingShieldedOp
	if err := k.IteratePendingOps(ctx, func(op types.PendingShieldedOp) bool {
		dropped = append(dropped, op)
		return false
	}); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("RetireKeySetMaterial: k.IteratePendingOps failed", "err", err)
	}
	for _, op := range dropped {
		if err := k.DeletePendingOp(ctx, op.TargetEpoch, op.Id); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("RetireKeySetMaterial: k.DeletePendingOp failed", "err", err)
		}
		if err := k.DeletePendingNullifier(ctx, hex.EncodeToString(op.SubmissionNullifier)); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("RetireKeySetMaterial: k.DeletePendingNullifier failed", "err", err)
		}
	}
	return len(dropped), nil
}

// StoreVerifiedDecryptionShare stores a share that passed
// VerifyValidatorDecryptionShare and, once the key set's threshold is
// reached for that epoch, reconstructs and stores the epoch key. Returns
// true when the share was new.
func (k Keeper) StoreVerifiedDecryptionShare(ctx context.Context, validatorAddr string, epoch uint64, share []byte) (bool, error) {
	if _, dup := k.GetDecryptionShare(ctx, epoch, validatorAddr); dup {
		return false, nil
	}
	if err := k.SetDecryptionShare(ctx, types.ShieldDecryptionShare{
		Epoch: epoch, Validator: validatorAddr, Share: share,
	}); err != nil {
		return false, err
	}
	return true, nil
}

// TryReconstructEpochKey reconstructs epoch's decryption key when enough
// verified shares are stored and no key exists yet. Emits the usual events.
func (k Keeper) TryReconstructEpochKey(ctx context.Context, epoch uint64) {
	if _, found := k.GetShieldEpochDecryptionKeyVal(ctx, epoch); found {
		return
	}
	ks, found := k.GetTLEKeySetVal(ctx)
	if !found {
		return
	}
	threshold := keySetThreshold(ks)
	shareCount := k.CountDecryptionShares(ctx, epoch)
	if uint64(shareCount) < threshold {
		return
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	key, err := ReconstructEpochDecryptionKey(k.GetDecryptionSharesForEpoch(ctx, epoch), ks)
	if err != nil {
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypeShieldDecryptionKeyFailed,
			sdk.NewAttribute(types.AttributeKeyEpoch, fmt.Sprintf("%d", epoch)),
			sdk.NewAttribute(types.AttributeKeyError, err.Error()),
		))
		return
	}
	if err := k.SetShieldEpochDecryptionKey(ctx, types.ShieldEpochDecryptionKey{
		Epoch:                 epoch,
		DecryptionKey:         key,
		ReconstructedAtHeight: sdkCtx.BlockHeight(),
	}); err != nil {
		return
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeShieldDecryptionKeyAvailable,
		sdk.NewAttribute(types.AttributeKeyEpoch, fmt.Sprintf("%d", epoch)),
		sdk.NewAttribute(types.AttributeKeySharesSubmitted, fmt.Sprintf("%d", shareCount)),
		sdk.NewAttribute(types.AttributeKeyThresholdRequired, fmt.Sprintf("%d", threshold)),
	))
}

// ClearDecryptionShares removes every stored share (used when a new DKG
// round retires the key set they were derived from).
func (k Keeper) ClearDecryptionShares(ctx context.Context) error {
	iter, err := k.ShieldDecryptionShares.Iterate(ctx, nil)
	if err != nil {
		return err
	}
	var keys []collections.Pair[uint64, string]
	for ; iter.Valid(); iter.Next() {
		key, err := iter.Key()
		if err != nil {
			iter.Close()
			return err
		}
		keys = append(keys, key)
	}
	iter.Close()
	for _, key := range keys {
		if err := k.ShieldDecryptionShares.Remove(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

// ClearDecryptionKeys removes every reconstructed epoch key.
func (k Keeper) ClearDecryptionKeys(ctx context.Context) error {
	iter, err := k.ShieldDecryptionKeys.Iterate(ctx, nil)
	if err != nil {
		return err
	}
	var keys []uint64
	for ; iter.Valid(); iter.Next() {
		key, err := iter.Key()
		if err != nil {
			iter.Close()
			return err
		}
		keys = append(keys, key)
	}
	iter.Close()
	for _, key := range keys {
		if err := k.ShieldDecryptionKeys.Remove(ctx, key); err != nil {
			return err
		}
	}
	return nil
}
