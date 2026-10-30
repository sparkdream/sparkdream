package keeper

import (
	"context"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	"sparkdream/x/collect/types"
	reptypes "sparkdream/x/rep/types"
)

// escrowedEndorsementFee returns the endorsement creation fee actually held
// in escrow for coll (zero when none, or once settled). Refunds, endorser
// payouts and burns use this amount, never the current
// endorsement_creation_fee param, so a param change can neither drain the
// module account (which also holds other owners' TTL deposits) nor strand
// escrowed SPARK.
func escrowedEndorsementFee(coll types.Collection) math.Int {
	if coll.EndorsementFeeEscrowed.IsNil() || !coll.EndorsementFeeEscrowed.IsPositive() {
		return math.ZeroInt()
	}
	return coll.EndorsementFeeEscrowed
}

// clearEndorsementPending removes coll's EndorsementPending index entry using
// the deadline stored on the collection at creation, and zeroes the stored
// deadline. The key is never rebuilt from the endorsement_expiry_blocks
// operational param: after a param change that would miss the real entry and
// the pruner would later delete an endorsed or promoted collection.
//
// Collections written before the deadline was stored (genesis imports of
// older state) carry a zero deadline; for those the index is walked once to
// find the entry by collection id; wasPending limits that walk to
// collections that were actually in the endorsement stage.
func (k Keeper) clearEndorsementPending(ctx context.Context, coll *types.Collection, wasPending bool) {
	if coll.EndorsementPendingUntil > 0 {
		if err := k.EndorsementPending.Remove(ctx, collections.Join(coll.EndorsementPendingUntil, coll.Id)); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("clearEndorsementPending: EndorsementPending.Remove failed", "err", err)
		}
		coll.EndorsementPendingUntil = 0
		return
	}
	if !wasPending {
		return
	}
	var found *collections.Pair[int64, uint64]
	if err := k.EndorsementPending.Walk(ctx, nil, func(key collections.Pair[int64, uint64]) (bool, error) {
		if key.K2() == coll.Id {
			found = &key
			return true, nil
		}
		return false, nil
	}); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("clearEndorsementPending: EndorsementPending.Walk failed", "err", err)
	}
	if found != nil {
		if err := k.EndorsementPending.Remove(ctx, *found); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("clearEndorsementPending: EndorsementPending.Remove failed", "err", err)
		}
	}
}

// flagExpiryBlock returns the block height of flag's FlagExpiry index entry:
// the height stored when it was last flagged, or (for flags written before
// the height was stored) last_flag_at + the current flag_expiration_blocks.
func flagExpiryBlock(flag types.CollectionFlag, params types.Params) int64 {
	if flag.ExpiresAt > 0 {
		return flag.ExpiresAt
	}
	return flag.LastFlagAt + params.FlagExpirationBlocks
}

// selfCorrectedBondHeld reports whether hr is a self-corrected hide whose
// sentinel bond is still reserved until appeal_deadline: MsgUnhideContent
// keeps the HideRecordExpiry entry for it, and the EndBlocker zeroes
// CommittedAmount when it releases the bond.
func selfCorrectedBondHeld(hr types.HideRecord) bool {
	return hr.Resolved && hr.SelfCorrected && !hr.CommittedAmount.IsNil() && hr.CommittedAmount.IsPositive()
}

// closeHideRecordOnTargetDelete settles hr when its target is deleted. An
// open hide releases the sentinel's reservation now. A self-corrected hide
// whose bond is still held keeps its expiry entry, so the EndBlocker
// releases the bond at the original appeal_deadline exactly as if the
// target still existed (deleting the target must neither leak the
// reservation nor shorten the anti-cycling hold).
func (k Keeper) closeHideRecordOnTargetDelete(ctx context.Context, hr types.HideRecord) {
	if selfCorrectedBondHeld(hr) {
		return
	}
	if !hr.Resolved && k.late.repKeeper != nil && !hr.CommittedAmount.IsNil() && hr.CommittedAmount.IsPositive() {
		if err := k.late.repKeeper.ReleaseBond(ctx, reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, hr.Sentinel, hr.CommittedAmount); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("closeHideRecordOnTargetDelete: repKeeper.ReleaseBond failed", "err", err)
		}
	}
	hr.Resolved = true
	if err := k.HideRecord.Set(ctx, hr.Id, hr); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("closeHideRecordOnTargetDelete: HideRecord.Set failed", "err", err)
	}
	if err := k.HideRecordExpiry.Remove(ctx, collections.Join(hr.AppealDeadline, hr.Id)); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("closeHideRecordOnTargetDelete: HideRecordExpiry.Remove failed", "err", err)
	}
}
