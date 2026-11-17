package keeper

import (
	"context"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/storage/types"
)

func (k msgServer) VoidCompletion(ctx context.Context, msg *types.MsgVoidCompletion) (*types.MsgVoidCompletionResponse, error) {
	if err := k.wired(); err != nil {
		return nil, err
	}
	p, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	if uint64(len(msg.Reason)) > uint64(p.MaxReasonBytes) {
		return nil, types.ErrInvalidReason.Wrapf("at most %d bytes", p.MaxReasonBytes)
	}
	r, err := k.GetRequest(ctx, msg.RequestId)
	if err != nil {
		return nil, err
	}
	gov := k.isGov(msg.Authority)
	now := sdk.UnwrapSDKContext(ctx).BlockHeight()
	old := r

	// A flagged hash is never voided back to life. A first flag closes any
	// COMPLETED request for the hash, so this matters for SETTLED ones; it
	// is checked for both so the rule does not rest on that.
	switch r.Status {
	case types.RequestStatus_REQUEST_STATUS_COMPLETED, types.RequestStatus_REQUEST_STATUS_SETTLED:
		if flagged, err := k.isFlagged(ctx, r.Sha256); err != nil {
			return nil, err
		} else if flagged {
			return nil, types.ErrHashRemoved
		}
	}

	switch r.Status {
	case types.RequestStatus_REQUEST_STATUS_COMPLETED:
		if !gov && (r.Controller == "" || msg.Authority != r.Controller) {
			return nil, types.ErrUnauthorized.Wrap("the request's controller or governance voids a completion")
		}
		k.emitVoided(ctx, r, msg.Authority, msg.Reason)
		voidToPending(&r, now, p)

	case types.RequestStatus_REQUEST_STATUS_SETTLED:
		switch {
		case !gov && r.PaidTo != "" && msg.Authority == r.PaidTo:
			// The paid council returns the fee; the request is funded again.
			if err := k.charge(ctx, msg.Authority, r.PaidAmount); err != nil {
				return nil, err
			}
			r.CopyFee = r.PaidAmount
			k.emitVoided(ctx, r, msg.Authority, msg.Reason)
			voidToPending(&r, now, p)
		case gov:
			// The fee stays where it was paid. The request closes, the hash
			// is free again, and the event records what the council owes.
			emit(ctx, requestEvent("settled_copy_voided", r,
				sdk.NewAttribute("authority", msg.Authority),
				sdk.NewAttribute("reason", msg.Reason),
				sdk.NewAttribute("requester", r.Requester),
				sdk.NewAttribute("paid_to", r.PaidTo),
				sdk.NewAttribute("paid_amount", r.PaidAmount.String()),
				sdk.NewAttribute("operator", r.Operator),
				sdk.NewAttribute("locator", r.Locator),
			))
			closeRequest(&r, types.RequestStatus_REQUEST_STATUS_REJECTED, types.ClosedReason_CLOSED_REASON_VOIDED, now)
		default:
			return nil, types.ErrUnauthorized.Wrap("the paid council or governance voids a settled copy")
		}

	default:
		return nil, types.ErrWrongStatus.Wrap(r.Status.String())
	}
	if err := k.saveRequest(ctx, &old, r, p); err != nil {
		return nil, err
	}
	return &types.MsgVoidCompletionResponse{}, nil
}

// emitVoided records the voided completion: it is the evidence for a report
// against the operator, and the void clears it from the request.
func (k Keeper) emitVoided(ctx context.Context, r types.Request, authority, reason string) {
	emit(ctx, requestEvent("completion_voided", r,
		sdk.NewAttribute("authority", authority),
		sdk.NewAttribute("reason", reason),
		sdk.NewAttribute("operator", r.Operator),
		sdk.NewAttribute("controller", r.Controller),
		sdk.NewAttribute("locator", r.Locator),
		sdk.NewAttribute("attestations", strings.Join(r.Attestations, ",")),
		sdk.NewAttribute("verdicts_locator", r.VerdictsLocator),
		sdk.NewAttribute("claimed_at", i64(r.ClaimedAt)),
		sdk.NewAttribute("completed_at", i64(r.CompletedAt)),
		sdk.NewAttribute("was_settled", boolStr(r.Status == types.RequestStatus_REQUEST_STATUS_SETTLED)),
	))
}
