package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/storage/types"
)

func (k msgServer) ExtendClaim(ctx context.Context, msg *types.MsgExtendClaim) (*types.MsgExtendClaimResponse, error) {
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
	if r.Status != types.RequestStatus_REQUEST_STATUS_CLAIMED || r.Operator != msg.Operator {
		return nil, types.ErrNotClaimant
	}
	b, err := k.Backends.Get(ctx, r.Backend)
	if err != nil {
		return nil, types.ErrBackendNotFound.Wrap(r.Backend)
	}
	if _, ok := k.operatorApproval(ctx, b, msg.Operator); !ok {
		return nil, types.ErrOperatorNotApproved
	}
	now := sdk.UnwrapSDKContext(ctx).BlockHeight()
	if now > r.ClaimExpiresAt {
		return nil, types.ErrClaimExpired
	}
	if r.ClaimExtended {
		return nil, types.ErrClaimAlreadyExtended
	}
	old := r
	r.ClaimExpiresAt += p.ClaimWindowBlocks
	r.ClaimExtended = true
	if err := k.saveRequest(ctx, &old, r, p); err != nil {
		return nil, err
	}
	emit(ctx, requestEvent("claim_extended", r,
		sdk.NewAttribute("operator", r.Operator),
		sdk.NewAttribute("claim_expires_at", i64(r.ClaimExpiresAt)),
	))
	return &types.MsgExtendClaimResponse{}, nil
}
