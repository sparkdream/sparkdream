package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/storage/types"
)

func (k msgServer) CancelRequest(ctx context.Context, msg *types.MsgCancelRequest) (*types.MsgCancelRequestResponse, error) {
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
	if !k.canActAs(ctx, msg.Requester, r.Requester) {
		return nil, types.ErrUnauthorized.Wrap("only the requester may cancel")
	}
	// Refused while claimed or completed: an operator may be paying for the
	// upload at that moment.
	if r.Status != types.RequestStatus_REQUEST_STATUS_PENDING {
		return nil, types.ErrWrongStatus.Wrap(r.Status.String())
	}
	old := r
	if err := k.refund(ctx, &r); err != nil {
		return nil, err
	}
	closeRequest(&r, types.RequestStatus_REQUEST_STATUS_CANCELLED, types.ClosedReason_CLOSED_REASON_CANCELLED,
		sdk.UnwrapSDKContext(ctx).BlockHeight())
	if err := k.saveRequest(ctx, &old, r, p); err != nil {
		return nil, err
	}
	emit(ctx, requestEvent("request_cancelled", r))
	return &types.MsgCancelRequestResponse{}, nil
}
