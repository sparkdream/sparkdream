package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/storage/types"
)

func (k msgServer) FlagRemoved(ctx context.Context, msg *types.MsgFlagRemoved) (*types.MsgFlagRemovedResponse, error) {
	if err := k.wired(); err != nil {
		return nil, err
	}
	p, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	if err := types.ValidateHash(msg.Sha256); err != nil {
		return nil, err
	}
	if err := types.ValidateHash(msg.AttestationHash); err != nil {
		return nil, types.ErrInvalidHash.Wrap("attestation_hash")
	}
	if !k.scannerCounts(ctx, msg.Scanner) {
		return nil, types.ErrScannerNotApproved
	}
	now := sdk.UnwrapSDKContext(ctx).BlockHeight()

	rec, err := k.RemovedHashes.Get(ctx, msg.Sha256)
	first := false
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return nil, err
		}
		first = true
		rec = types.RemovedHash{Sha256: msg.Sha256}
	}
	for _, f := range rec.Flags {
		if f.Scanner == msg.Scanner {
			return nil, types.ErrAlreadyFlagged
		}
	}
	if uint64(len(rec.Flags)) >= uint64(p.MaxFlagsPerHash) {
		return nil, types.ErrTooManyFlags
	}
	if err := k.bumpCapped(ctx, msg.Scanner, types.CounterKind_COUNTER_KIND_FLAG, p.MaxFlagsPerScannerPerWindow, now, p); err != nil {
		return nil, err
	}
	rec.Flags = append(rec.Flags, types.Flag{Scanner: msg.Scanner, AttestationHash: msg.AttestationHash, Height: now})

	// A later flag restarts the hold on fees already held, so the new
	// flag's controller has the whole hold to lift it.
	for _, id := range rec.Held {
		r, err := k.GetRequest(ctx, id)
		if err != nil {
			return nil, err
		}
		old := r
		r.ForfeitAt = now + p.FlagHoldBlocks
		if err := k.saveRequest(ctx, &old, r, p); err != nil {
			return nil, err
		}
	}

	if first {
		// Reach the one live or settled request each backend holds for the
		// hash. The cost depends on the number of backends, never on how
		// many closed requests the hash has had.
		var ids []uint64
		err := k.Backends.Walk(ctx, nil, func(name string, _ types.Backend) (bool, error) {
			id, err := k.LiveByHash.Get(ctx, collections.Join(name, msg.Sha256))
			if err == nil {
				ids = append(ids, id)
				return false, nil
			}
			if errors.Is(err, collections.ErrNotFound) {
				return false, nil
			}
			return true, err
		})
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			held, err := k.flagRequest(ctx, id, msg.Scanner, now, p)
			if err != nil {
				return nil, err
			}
			if held {
				rec.Held = append(rec.Held, id)
			}
		}
	}
	if err := k.RemovedHashes.Set(ctx, msg.Sha256, rec); err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("storage.hash_flagged",
		sdk.NewAttribute("sha256", msg.Sha256),
		sdk.NewAttribute("scanner", msg.Scanner),
		sdk.NewAttribute("attestation_hash", msg.AttestationHash),
	))
	return &types.MsgFlagRemovedResponse{}, nil
}

// flagRequest applies a first flag to one request. held reports whether the
// request's fee is now held (it was claimed or completed).
func (k Keeper) flagRequest(ctx context.Context, id uint64, scanner string, now int64, p types.Params) (held bool, err error) {
	r, err := k.GetRequest(ctx, id)
	if err != nil {
		return false, err
	}
	old := r
	switch r.Status {
	case types.RequestStatus_REQUEST_STATUS_PENDING:
		if err := k.refund(ctx, &r); err != nil {
			return false, err
		}
		closeRequest(&r, types.RequestStatus_REQUEST_STATUS_REJECTED, types.ClosedReason_CLOSED_REASON_FLAGGED, now)
	case types.RequestStatus_REQUEST_STATUS_CLAIMED, types.RequestStatus_REQUEST_STATUS_COMPLETED:
		// Neither refunded nor paid: held, then burned. The claim and
		// completion fields stay, so a lifted flag can reopen the request.
		closeRequest(&r, types.RequestStatus_REQUEST_STATUS_REJECTED, types.ClosedReason_CLOSED_REASON_FLAGGED, now)
		r.ForfeitAt = now + p.FlagHoldBlocks
		held = true
	default:
		// SETTLED stays as a record; clients never fetch it.
		return false, nil
	}
	if err := k.saveRequest(ctx, &old, r, p); err != nil {
		return false, err
	}
	emit(ctx, requestEvent("request_rejected", r,
		sdk.NewAttribute("reason", r.ClosedReason.String()),
		sdk.NewAttribute("requester", r.Requester),
		sdk.NewAttribute("scanner", scanner),
		sdk.NewAttribute("forfeit_at", i64(r.ForfeitAt)),
	))
	return held, nil
}
