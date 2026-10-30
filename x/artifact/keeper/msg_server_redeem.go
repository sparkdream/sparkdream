package keeper

import (
	"context"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// Redeem marks tokens of a redeemable class as redeemed (docs/x-artifact-spec.md
// §5.3.8). Signed by the class owner or a listed minter (the venue). It
// never moves ownership; an active listing is cancelled so nobody buys a
// spent ticket. Expired, hidden and pending-transfer tokens are refused.
func (k msgServer) Redeem(ctx context.Context, msg *types.MsgRedeem) (*types.MsgRedeemResponse, error) {
	if _, err := k.decodeAddr("signer", msg.Signer); err != nil {
		return nil, err
	}
	if err := checkBatch(len(msg.TokenIds), k.GetParams(ctx)); err != nil {
		return nil, err
	}
	c, err := k.getClass(ctx, msg.ClassId)
	if err != nil {
		return nil, err
	}
	if !c.Flags.Redeemable {
		return nil, types.ErrNotRedeemable
	}
	if !k.isMinter(ctx, c, msg.Signer) {
		return nil, types.ErrNotMinter
	}
	refs := make([]U64Pair, len(msg.TokenIds))
	for i, id := range msg.TokenIds {
		refs[i] = collections.Join(c.Id, id)
	}
	if err := checkNoDuplicateRefs(refs); err != nil {
		return nil, err
	}
	t0 := now(ctx)
	for _, id := range msg.TokenIds {
		t, err := k.getToken(ctx, c.Id, id)
		if err != nil {
			return nil, err
		}
		if isTargetHidden(c, t) {
			return nil, types.ErrContentHidden
		}
		if t.RedeemedAt != 0 {
			return nil, types.ErrAlreadyRedeemed
		}
		if types.IsExpired(t, t0) {
			return nil, types.ErrTokenExpired
		}
		// A token on its way to someone else, under auction (a running sale
		// with escrowed bids) or nested inside another token (it travels
		// with that tree) is not redeemed; detach or settle first. A rented
		// token can be: the renter may be the one using the ticket.
		switch t.Lock {
		case types.TokenLock_TOKEN_LOCK_PENDING_TRANSFER, types.TokenLock_TOKEN_LOCK_AUCTION, types.TokenLock_TOKEN_LOCK_NESTED:
			return nil, errorsmod.Wrapf(types.ErrTokenLocked, "token %d/%d is %s", t.ClassId, t.Id, t.Lock)
		}
		if t.Lock == types.TokenLock_TOKEN_LOCK_LISTED {
			if err := k.removeListing(ctx, c.Id, id, "redeemed"); err != nil {
				return nil, err
			}
			if t, err = k.getToken(ctx, c.Id, id); err != nil {
				return nil, err
			}
		}
		t.RedeemedAt = t0
		if err := k.saveToken(ctx, t); err != nil {
			return nil, err
		}
		emit(ctx, types.EventRedeemed, classAttr(c.Id), tokenAttr(id),
			sdk.NewAttribute("by", msg.Signer), sdk.NewAttribute("holder", t.Owner))
	}
	return &types.MsgRedeemResponse{}, nil
}
