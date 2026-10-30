package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/collect/types"
	commontypes "sparkdream/x/common/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// MakeCollectionPermanent flips an ephemeral collection to permanent. Burns
// the escrowed collection + item deposits (matching the pre-rework Pin
// semantics) and removes the CollectionsByExpiry entry so the TTL prune
// never fires. Pin markers are untouched — preservation and featuring are
// separate actions now.
//
// Gates:
//   - trust level ≥ params.make_permanent_min_trust_level (default PROVISIONAL)
//   - target must be ACTIVE; expired-but-not-yet-pruned targets are rejected
//     to avoid racing the next EndBlocker
//   - dedicated per-day MakePermanent rate-limit counter
//     (params.max_make_permanent_per_day) — independent of MaxPinsPerDay
//
// Idempotent on already-permanent collections (returns success without state
// change) so callers can blindly upgrade-then-pin without first querying
// expires_at.
func (k msgServer) MakeCollectionPermanent(ctx context.Context, msg *types.MsgMakeCollectionPermanent) (*types.MsgMakeCollectionPermanentResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockHeight := sdkCtx.BlockHeight()

	coll, err := k.Collection.Get(ctx, msg.CollectionId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrCollectionNotFound, fmt.Sprintf("collection %d not found", msg.CollectionId))
	}

	if coll.Status != types.CollectionStatus_COLLECTION_STATUS_ACTIVE {
		return nil, errorsmod.Wrap(types.ErrCollectionNotFound, fmt.Sprintf("collection %d is not active", msg.CollectionId))
	}

	// Expired-but-not-yet-pruned content would race the next EndBlocker.
	if coll.ExpiresAt > 0 && coll.ExpiresAt <= blockHeight {
		return nil, errorsmod.Wrap(types.ErrCollectionExpired, fmt.Sprintf("collection %d has expired", msg.CollectionId))
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	creatorAddr, _ := sdk.AccAddressFromBech32(msg.Creator)
	if k.late.repKeeper == nil {
		return nil, errorsmod.Wrap(types.ErrMakePermanentTrustLevelTooLow, "reputation module not available")
	}
	if !k.late.repKeeper.IsActiveMember(ctx, creatorAddr) {
		return nil, errorsmod.Wrap(types.ErrMakePermanentTrustLevelTooLow, "not a member")
	}
	tl, err := k.late.repKeeper.GetTrustLevel(ctx, creatorAddr)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrMakePermanentTrustLevelTooLow, "cannot determine trust level")
	}
	if commontypes.Saturate[uint32](tl) < params.MakePermanentMinTrustLevel {
		return nil, errorsmod.Wrap(types.ErrMakePermanentTrustLevelTooLow,
			fmt.Sprintf("does not meet make-permanent trust level requirement (have=%d, need=%d)", commontypes.Saturate[uint32](tl), params.MakePermanentMinTrustLevel))
	}

	// Idempotent on already-permanent (before the rate limit, so a blind
	// upgrade-then-pin never spends a daily slot on a no-op).
	if coll.ExpiresAt == 0 {
		return &types.MsgMakeCollectionPermanentResponse{}, nil
	}

	// Dedicated MakePermanent daily counter — independent of MaxPinsPerDay.
	if err := k.checkDailyLimit(ctx, msg.Creator, blockHeight, "make_permanent", params.MaxMakePermanentPerDay); err != nil {
		return nil, err
	}

	oldExpiresAt := coll.ExpiresAt
	coll.ExpiresAt = 0
	coll.DepositBurned = true
	if coll.ConvictionSustained {
		coll.ConvictionSustained = false
	}

	// The owner's escrowed deposits are refundable to the owner, so a third
	// party preserving someone else's collection pays the burn itself: the
	// caller burns an equal amount from its own account and the escrow goes
	// back to the owner (as sponsorship does). The owner (or its migrated
	// key) preserving its own collection burns the escrow directly.
	totalDeposit := coll.DepositAmount.Add(coll.ItemDepositTotal)
	paidByCaller := false
	if totalDeposit.IsPositive() {
		if k.isCollectionOwner(ctx, coll, msg.Creator) {
			if err := k.BurnSPARK(ctx, totalDeposit); err != nil {
				return nil, err
			}
		} else {
			if err := k.BurnSPARKFromAccount(ctx, creatorAddr, totalDeposit); err != nil {
				return nil, errorsmod.Wrap(types.ErrInsufficientFunds, err.Error())
			}
			ownerAddr, err := k.addressCodec.StringToBytes(coll.Owner)
			if err != nil {
				return nil, errorsmod.Wrap(err, "invalid owner address")
			}
			if err := k.RefundSPARK(ctx, ownerAddr, totalDeposit); err != nil {
				return nil, err
			}
			paidByCaller = true
		}
	}

	if err := k.Collection.Set(ctx, coll.Id, coll); err != nil {
		return nil, err
	}
	if err := k.CollectionsByExpiry.Remove(ctx, collections.Join(oldExpiresAt, coll.Id)); err != nil {
		return nil, err
	}
	// A direct conversion supersedes a pending sponsorship request, as in
	// MsgUpdateCollection: refund its escrow and delete it.
	if err := k.supersedeSponsorshipRequest(ctx, coll.Id); err != nil {
		return nil, err
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("collect.collection.upgraded",
		sdk.NewAttribute("collection_id", fmt.Sprintf("%d", msg.CollectionId)),
		sdk.NewAttribute("owner", coll.Owner),
		sdk.NewAttribute("by", msg.Creator),
		sdk.NewAttribute("via", "make_permanent"),
		sdk.NewAttribute("deposit_burned", totalDeposit.String()),
		sdk.NewAttribute("paid_by_caller", fmt.Sprintf("%t", paidByCaller)),
	))

	return &types.MsgMakeCollectionPermanentResponse{}, nil
}
