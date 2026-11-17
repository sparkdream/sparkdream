package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	servicetypes "sparkdream/x/service/types"
	"sparkdream/x/storage/types"
)

func (k msgServer) UnflagRemoved(ctx context.Context, msg *types.MsgUnflagRemoved) (*types.MsgUnflagRemovedResponse, error) {
	if err := k.wired(); err != nil {
		return nil, err
	}
	p, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	if len(msg.Hashes) == 0 || uint64(len(msg.Hashes)) > uint64(p.MaxHashesPerUnflag) {
		return nil, types.ErrTooManyHashes.Wrapf("1 to %d hashes", p.MaxHashesPerUnflag)
	}
	gov := k.isGov(msg.Authority)
	if !gov && !k.late.commons.IsGroupPolicyAddress(ctx, msg.Authority) {
		return nil, types.ErrUnauthorized.Wrap("a council or governance lifts flags")
	}
	now := sdk.UnwrapSDKContext(ctx).BlockHeight()
	seen := make(map[string]bool, len(msg.Hashes))
	for _, h := range msg.Hashes {
		if err := types.ValidateHash(h); err != nil {
			return nil, err
		}
		if seen[h] {
			return nil, types.ErrTooManyHashes.Wrap("hashes must be distinct")
		}
		seen[h] = true
	}
	// The message fails as a whole if any hash is refused: an error below
	// discards the writes made for the hashes before it.
	for _, h := range msg.Hashes {
		rec, err := k.RemovedHashes.Get(ctx, h)
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				return nil, types.ErrNotFlagged.Wrap(h)
			}
			return nil, err
		}
		kept := rec.Flags[:0:0]
		for _, f := range rec.Flags {
			if gov {
				continue
			}
			// A council lifts the flags of scanners it controls now.
			// A flag whose scanner has no operator record is governance's.
			op, live := k.liveOperator(ctx, f.Scanner, types.ContentScannerServiceType)
			if live && op.Controller == msg.Authority {
				continue
			}
			kept = append(kept, f)
		}
		if len(kept) == len(rec.Flags) {
			return nil, types.ErrUnauthorized.Wrapf("no flag on %s is yours to lift", h)
		}
		rec.Flags = kept
		if len(rec.Flags) > 0 {
			// Nothing is reopened until the last flag is lifted.
			if err := k.RemovedHashes.Set(ctx, h, rec); err != nil {
				return nil, err
			}
			continue
		}
		if err := k.RemovedHashes.Remove(ctx, h); err != nil {
			return nil, err
		}
		for _, id := range rec.Held {
			if err := k.reopen(ctx, id, now, p); err != nil {
				return nil, err
			}
		}
		sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("storage.hash_unflagged",
			sdk.NewAttribute("sha256", h),
			sdk.NewAttribute("authority", msg.Authority),
		))
	}
	return &types.MsgUnflagRemovedResponse{}, nil
}

// reopen restores a request whose fee was held by a flag that is now lifted.
// One closed after its completion returns to COMPLETED with a full settle
// window and may be disputed again; one closed while claimed returns to
// PENDING and its former claimant is not barred. Either way it is a fresh
// attempt: verdicts scanned before now no longer count.
//
// A completion is restored only while its operator is ACTIVE or UNDERFUNDED.
// Flags can hold a request for any length of time (each later flag restarts
// the hold, and a reopened completion can be flagged again), so an operator
// that began to unbond after completing may have its bond back before the
// restored completion could be voided. Such a request returns to PENDING
// for another operator instead.
func (k Keeper) reopen(ctx context.Context, id uint64, now int64, p types.Params) error {
	r, err := k.GetRequest(ctx, id)
	if err != nil {
		if errors.Is(err, types.ErrRequestNotFound) {
			return nil
		}
		return err
	}
	if r.Status != types.RequestStatus_REQUEST_STATUS_REJECTED ||
		r.ClosedReason != types.ClosedReason_CLOSED_REASON_FLAGGED || r.ForfeitAt == 0 {
		return nil // the fee was already burned
	}
	old := r
	if r.CompletedAt != 0 && k.bondStays(ctx, r) {
		r.Status = types.RequestStatus_REQUEST_STATUS_COMPLETED
		r.ClosedReason = types.ClosedReason_CLOSED_REASON_UNSPECIFIED
		r.ClosedAt = 0
		r.ForfeitAt = 0
		r.OpenedAt = now
		r.SettleAt = now + p.SettleWindowBlocks
		r.Disputed = false
	} else {
		restart(&r, now, p)
	}
	if err := k.saveRequest(ctx, &old, r, p); err != nil {
		return err
	}
	emit(ctx, requestEvent("request_reopened", r,
		sdk.NewAttribute("status", r.Status.String()),
		sdk.NewAttribute("opened_at", i64(r.OpenedAt)),
	))
	return nil
}

// bondStays reports whether the request's operator has a bond that will be
// held for a whole unbonding period from now: its x/service record is ACTIVE
// or UNDERFUNDED. An UNBONDING, retired or slashed operator does not.
func (k Keeper) bondStays(ctx context.Context, r types.Request) bool {
	b, err := k.Backends.Get(ctx, r.Backend)
	if err != nil {
		return false
	}
	op, live := k.liveOperator(ctx, r.Operator, b.ServiceType)
	return live && (op.Status == servicetypes.OperatorStatus_OPERATOR_STATUS_ACTIVE ||
		op.Status == servicetypes.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED)
}
