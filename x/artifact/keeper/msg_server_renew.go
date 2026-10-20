package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// Renew extends an expiring token by the class term (docs/x-artifact-spec.md
// §5.3.7). The holder pays renewal_price to the class (sale fee to the
// community pool); the class owner or a listed minter renews for free. An
// already-expired token can still be renewed while it exists.
func (k msgServer) Renew(ctx context.Context, msg *types.MsgRenew) (*types.MsgRenewResponse, error) {
	signer, err := k.decodeAddr("signer", msg.Signer)
	if err != nil {
		return nil, err
	}
	c, err := k.getClass(ctx, msg.ClassId)
	if err != nil {
		return nil, err
	}
	if !c.Expiry.Expires() {
		return nil, types.ErrNotExpiring
	}
	t, err := k.getToken(ctx, msg.ClassId, msg.TokenId)
	if err != nil {
		return nil, err
	}
	if isTargetHidden(c, t) {
		return nil, types.ErrContentHidden
	}
	issuer := isMinter(c, msg.Signer)
	if !issuer && t.Owner != msg.Signer {
		return nil, errorsmod.Wrap(types.ErrNotAuthorized, "only the holder, the class owner or a listed minter may renew")
	}
	price := c.Expiry.RenewalPrice.Amount
	paid := "0"
	if !issuer && price.IsPositive() {
		if err := k.validatePrice(ctx, msg.MaxPrice, true); err != nil {
			return nil, err
		}
		if price.GT(msg.MaxPrice.Amount) {
			return nil, errorsmod.Wrapf(types.ErrPriceExceeded, "renewal price %s > max %s", price, msg.MaxPrice.Amount)
		}
		fee := price.MulRaw(int64(k.GetParams(ctx).SaleFeeBps)).QuoRaw(10000)
		if err := k.fundCommunityPool(ctx, signer, fee); err != nil {
			return nil, err
		}
		if err := k.send(ctx, signer, c.PayoutAddress, price.Sub(fee)); err != nil {
			return nil, err
		}
		paid = price.String()
	}

	if err := k.unindexTokenExpiry(ctx, c, t); err != nil {
		return nil, err
	}
	base := t.ExpiresAt
	if n := now(ctx); n > base {
		base = n
	}
	t.ExpiresAt = base + c.Expiry.Term
	if err := k.indexTokenExpiry(ctx, c, t); err != nil {
		return nil, err
	}
	if err := k.saveToken(ctx, t); err != nil {
		return nil, err
	}
	emit(ctx, types.EventRenewed, classAttr(t.ClassId), tokenAttr(t.Id),
		sdk.NewAttribute("by", msg.Signer), sdk.NewAttribute("expires_at", i64(t.ExpiresAt)),
		sdk.NewAttribute("paid", paid))
	return &types.MsgRenewResponse{ExpiresAt: t.ExpiresAt}, nil
}
