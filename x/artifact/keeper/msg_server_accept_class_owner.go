package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

func (k msgServer) AcceptClassOwner(ctx context.Context, msg *types.MsgAcceptClassOwner) (*types.MsgAcceptClassOwnerResponse, error) {
	if _, err := k.decodeAddr("proposed_owner", msg.ProposedOwner); err != nil {
		return nil, err
	}
	c, err := k.getClass(ctx, msg.ClassId)
	if err != nil {
		return nil, err
	}
	po, err := k.PendingClassOwners.Get(ctx, msg.ClassId)
	if err != nil || po.ProposedOwner != msg.ProposedOwner || po.ExpiresAt <= now(ctx) {
		return nil, types.ErrPendingOwnerNotFound
	}
	if err := k.clearPendingOwner(ctx, c.Id); err != nil {
		return nil, err
	}
	from := c.Owner
	if err := k.setClassOwner(ctx, &c, msg.ProposedOwner); err != nil {
		return nil, err
	}
	// No backdoors survive the handover (docs/x-artifact-spec.md §5.1.8).
	c.Minters = nil
	c.MintPolicy.PublicMintEnabled = false
	c.PayoutAddress = msg.ProposedOwner
	c.RoyaltyRecipients = types.SoleRoyaltyRecipient(msg.ProposedOwner)
	if err := k.saveClass(ctx, c); err != nil {
		return nil, err
	}
	emit(ctx, types.EventClassOwnerAccepted, classAttr(c.Id),
		sdk.NewAttribute("from", from), sdk.NewAttribute("to", c.Owner))
	return &types.MsgAcceptClassOwnerResponse{}, nil
}
