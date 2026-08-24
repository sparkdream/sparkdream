package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// FundReviewBounty escrows DREAM against an initiative to attract reviewers.
//
// Open to anyone, not just the creator: the amount should say how much the work
// matters to the people who want it checked, not how much one person can spare.
func (k msgServer) FundReviewBounty(ctx context.Context, msg *types.MsgFundReviewBounty) (*types.MsgFundReviewBountyResponse, error) {
	funder, err := k.addressCodec.StringToBytes(msg.Funder)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrInvalidRequest, "invalid funder address")
	}
	if _, err := k.Keeper.GetMember(ctx, sdk.AccAddress(funder)); err != nil {
		return nil, errorsmod.Wrap(types.ErrNotMember, "funder must be a member")
	}
	total, err := k.Keeper.EscrowReviewBounty(ctx, sdk.AccAddress(funder), msg.InitiativeId, msg.Amount)
	if err != nil {
		return nil, err
	}
	return &types.MsgFundReviewBountyResponse{Total: total}, nil
}

// ReclaimReviewBounty returns a funder's own unpaid contribution.
func (k msgServer) ReclaimReviewBounty(ctx context.Context, msg *types.MsgReclaimReviewBounty) (*types.MsgReclaimReviewBountyResponse, error) {
	funder, err := k.addressCodec.StringToBytes(msg.Funder)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrInvalidRequest, "invalid funder address")
	}
	refunded, err := k.Keeper.WithdrawReviewBounty(ctx, sdk.AccAddress(funder), msg.InitiativeId)
	if err != nil {
		return nil, err
	}
	return &types.MsgReclaimReviewBountyResponse{Refunded: refunded}, nil
}
