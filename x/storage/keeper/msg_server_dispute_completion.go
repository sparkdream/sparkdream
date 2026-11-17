package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/storage/types"
)

func (k msgServer) DisputeCompletion(ctx context.Context, msg *types.MsgDisputeCompletion) (*types.MsgDisputeCompletionResponse, error) {
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
	if r.Status != types.RequestStatus_REQUEST_STATUS_COMPLETED {
		return nil, types.ErrWrongStatus.Wrap(r.Status.String())
	}
	now := sdk.UnwrapSDKContext(ctx).BlockHeight()
	// At or after settle_at the completion is due to settle, whether or not
	// the EndBlocker has reached it; only a void can stop it.
	if now >= r.SettleAt {
		return nil, types.ErrSettleTimePassed
	}
	if r.Disputed {
		return nil, types.ErrAlreadyDisputed
	}
	// The requester disputes for nothing. Anyone else who could have made
	// the request pays the request fee: the requester may be the party that
	// gains from a false completion.
	if !k.canActAs(ctx, msg.Member, r.Requester) {
		if _, err := k.eligibleMember(ctx, msg.Member, p); err != nil {
			return nil, err
		}
		if msg.MaxFee.IsNil() || p.RequestFee.GT(msg.MaxFee) {
			return nil, types.ErrFeeExceedsMax.Wrapf("fee %s", p.RequestFee)
		}
		if err := k.chargeAndBurn(ctx, msg.Member, p.RequestFee); err != nil {
			return nil, err
		}
	}
	old := r
	r.SettleAt = now + p.DisputeWindowBlocks
	r.Disputed = true
	if err := k.saveRequest(ctx, &old, r, p); err != nil {
		return nil, err
	}
	emit(ctx, requestEvent("completion_disputed", r,
		sdk.NewAttribute("member", msg.Member),
		sdk.NewAttribute("settle_at", i64(r.SettleAt)),
	))
	return &types.MsgDisputeCompletionResponse{}, nil
}
