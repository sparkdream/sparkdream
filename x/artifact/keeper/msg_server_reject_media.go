package keeper

import (
	"context"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// RejectMedia discards a media proposal: the holder declines it, or the
// class owner withdraws it.
func (k msgServer) RejectMedia(ctx context.Context, msg *types.MsgRejectMedia) (*types.MsgRejectMediaResponse, error) {
	if _, err := k.decodeAddr("signer", msg.Signer); err != nil {
		return nil, err
	}
	c, err := k.getClass(ctx, msg.ClassId)
	if err != nil {
		return nil, err
	}
	t, err := k.getToken(ctx, msg.ClassId, msg.TokenId)
	if err != nil {
		return nil, err
	}
	if !k.canActAs(ctx, msg.Signer, t.Owner) && !k.canActAs(ctx, msg.Signer, c.Owner) {
		return nil, errorsmod.Wrap(types.ErrNotAuthorized, "only the holder or the class owner may reject a media proposal")
	}
	key := collections.Join3(t.ClassId, t.Id, msg.ProposalId)
	if has, _ := k.MediaProposals.Has(ctx, key); !has {
		return nil, errorsmod.Wrapf(types.ErrMediaProposalNotFound, "%d/%d #%d", t.ClassId, t.Id, msg.ProposalId)
	}
	if err := k.MediaProposals.Remove(ctx, key); err != nil {
		return nil, err
	}
	emit(ctx, types.EventMediaRejected, classAttr(t.ClassId), tokenAttr(t.Id),
		sdk.NewAttribute("proposal_id", u64(msg.ProposalId)), sdk.NewAttribute("by", msg.Signer))
	return &types.MsgRejectMediaResponse{}, nil
}
