package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/storage/types"
)

func (k msgServer) ClaimRequest(ctx context.Context, msg *types.MsgClaimRequest) (*types.MsgClaimRequestResponse, error) {
	if err := k.wired(); err != nil {
		return nil, err
	}
	p, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	r, err := k.GetRequest(ctx, msg.RequestId)
	if err != nil {
		return nil, err
	}
	if r.Status != types.RequestStatus_REQUEST_STATUS_PENDING {
		return nil, types.ErrWrongStatus.Wrap(r.Status.String())
	}
	now := sdk.UnwrapSDKContext(ctx).BlockHeight()
	if now > r.Deadline {
		return nil, types.ErrDeadlinePassed
	}
	b, err := k.Backends.Get(ctx, r.Backend)
	if err != nil {
		return nil, types.ErrBackendNotFound.Wrap(r.Backend)
	}
	approval, err := k.couldClaim(ctx, b, msg.Operator)
	if err != nil {
		return nil, err
	}
	if k.late.rep.SameMember(ctx, msg.Operator, r.Requester) {
		return nil, types.ErrSelfDealing
	}
	if k.isBarred(ctx, r, msg.Operator) {
		return nil, types.ErrOperatorBarred
	}
	// A claim that lapses must be recordable, or the operator could claim
	// the same request for ever.
	if uint64(len(r.Barred)) >= uint64(p.MaxBarredPerRequest) {
		return nil, types.ErrBarredListFull
	}
	if n, err := k.countClaims(ctx, msg.Operator, p.MaxOpenClaimsPerOperator); err != nil {
		return nil, err
	} else if n >= p.MaxOpenClaimsPerOperator {
		return nil, types.ErrTooManyClaims
	}
	lapses, err := k.counter(ctx, msg.Operator, types.CounterKind_COUNTER_KIND_LAPSE, now, p)
	if err != nil {
		return nil, err
	}
	if lapses.Count > uint64(p.MaxLapsesPerWindow) {
		return nil, types.ErrLapseLimit
	}

	old := r
	if r.Scanned {
		// The claim fixes the quorum: a later governance change never
		// strands an upload the operator has paid for.
		q, err := k.scannerQuorum(ctx, r.RequesterTrustLevel, p)
		if err != nil {
			return nil, err
		}
		r.RequiredAttestations = q
	}
	r.Status = types.RequestStatus_REQUEST_STATUS_CLAIMED
	r.Operator = msg.Operator
	// The claim fixes the controller: the council that approved the
	// operator is paid at settlement and may void.
	r.Controller = approval.Controller
	r.ClaimedAt = now
	r.ClaimExpiresAt = now + p.ClaimWindowBlocks
	r.ClaimExtended = false
	if err := k.saveRequest(ctx, &old, r, p); err != nil {
		return nil, err
	}
	emit(ctx, requestEvent("request_claimed", r,
		sdk.NewAttribute("operator", r.Operator),
		sdk.NewAttribute("controller", r.Controller),
		sdk.NewAttribute("claim_expires_at", i64(r.ClaimExpiresAt)),
		sdk.NewAttribute("required_attestations", u64(uint64(r.RequiredAttestations))),
	))
	return &types.MsgClaimRequestResponse{}, nil
}
