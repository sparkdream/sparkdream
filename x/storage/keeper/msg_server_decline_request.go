package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/storage/types"
)

func (k msgServer) DeclineRequest(ctx context.Context, msg *types.MsgDeclineRequest) (*types.MsgDeclineRequestResponse, error) {
	if err := k.wired(); err != nil {
		return nil, err
	}
	p, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	var kind types.CounterKind
	var max uint32
	switch msg.Reason {
	case types.DeclineReason_DECLINE_REASON_UNREACHABLE:
		kind, max = types.CounterKind_COUNTER_KIND_DECLINE_UNREACHABLE, p.MaxUnreachableDeclinesPerOperatorPerWindow
	case types.DeclineReason_DECLINE_REASON_MISMATCH:
		kind, max = types.CounterKind_COUNTER_KIND_DECLINE_MISMATCH, p.MaxMismatchDeclinesPerOperatorPerWindow
	default:
		return nil, types.ErrInvalidDecline.Wrap("unknown reason")
	}
	r, err := k.GetRequest(ctx, msg.RequestId)
	if err != nil {
		return nil, err
	}
	now := sdk.UnwrapSDKContext(ctx).BlockHeight()
	claimed := false
	switch r.Status {
	case types.RequestStatus_REQUEST_STATUS_PENDING:
		if now > r.Deadline {
			return nil, types.ErrDeadlinePassed
		}
	case types.RequestStatus_REQUEST_STATUS_CLAIMED:
		if r.Operator != msg.Operator {
			return nil, types.ErrNotClaimant
		}
		if now > r.ClaimExpiresAt {
			return nil, types.ErrClaimExpired
		}
		claimed = true
	default:
		return nil, types.ErrInvalidDecline.Wrap(r.Status.String())
	}
	b, err := k.Backends.Get(ctx, r.Backend)
	if err != nil {
		return nil, types.ErrBackendNotFound.Wrap(r.Backend)
	}
	if _, err := k.couldClaim(ctx, b, msg.Operator); err != nil {
		return nil, err
	}
	// One decline per operator per request, whichever way it was barred.
	if k.isBarred(ctx, r, msg.Operator) {
		return nil, types.ErrOperatorBarred
	}
	if uint64(len(r.Barred)) >= uint64(p.MaxBarredPerRequest) {
		return nil, types.ErrBarredListFull
	}
	if msg.Reason == types.DeclineReason_DECLINE_REASON_UNREACHABLE {
		var newest int64
		for _, s := range r.Sources {
			if s.AddedAt > newest {
				newest = s.AddedAt
			}
		}
		if now < newest+p.MinUnreachableBlocks {
			return nil, types.ErrDeclineTooEarly
		}
	}
	if err := k.bumpCapped(ctx, msg.Operator, kind, max, now, p); err != nil {
		return nil, err
	}

	old := r
	// Operators that are the same member count once. The canonical member is
	// stored with the decline, and a decliner whose earlier address already
	// declined is stored under that member.
	member := k.canonical(ctx, msg.Operator)
	known := make(map[string]bool, len(r.DeclinedBy))
	for _, d := range r.DeclinedBy {
		known[d.Member] = true
	}
	if !known[member] {
		for _, a := range k.late.rep.RecentAddressesOf(ctx, msg.Operator) {
			if known[a] {
				member = a
				break
			}
		}
	}
	r.DeclinedBy = append(append([]types.Decline(nil), r.DeclinedBy...), types.Decline{Operator: msg.Operator, Member: member, Reason: msg.Reason})
	if claimed {
		releaseClaim(&r) // no lapse is counted
	} else {
		bar(&r, msg.Operator)
	}
	members := make(map[string]bool, len(r.DeclinedBy))
	for _, d := range r.DeclinedBy {
		members[d.Member] = true
	}
	emit(ctx, requestEvent("request_declined", r,
		sdk.NewAttribute("operator", msg.Operator),
		sdk.NewAttribute("reason", msg.Reason.String()),
	))
	if uint64(len(members)) >= uint64(b.DeclinesToClose) {
		if err := k.refund(ctx, &r); err != nil {
			return nil, err
		}
		closeRequest(&r, types.RequestStatus_REQUEST_STATUS_REJECTED, types.ClosedReason_CLOSED_REASON_DECLINED, now)
		emit(ctx, requestEvent("request_rejected", r, sdk.NewAttribute("reason", r.ClosedReason.String())))
	}
	if err := k.saveRequest(ctx, &old, r, p); err != nil {
		return nil, err
	}
	return &types.MsgDeclineRequestResponse{}, nil
}
