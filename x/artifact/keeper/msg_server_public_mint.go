package keeper

import (
	"context"
	"strings"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// publicMintCheck returns why a public mint of quantity would fail now, or nil.
func (k Keeper) publicMintCheck(ctx context.Context, c types.Class, buyer string, quantity uint32, p types.Params) error {
	if !p.PublicMintEnabled || !c.MintPolicy.PublicMintEnabled {
		return errorsmod.Wrap(types.ErrMintWindow, "public mint is disabled")
	}
	if err := mintable(c); err != nil {
		return err
	}
	t := now(ctx)
	if c.MintPolicy.StartTime > 0 && t < c.MintPolicy.StartTime {
		return errorsmod.Wrap(types.ErrMintWindow, "public mint has not started")
	}
	if c.MintPolicy.EndTime > 0 && t >= c.MintPolicy.EndTime {
		return errorsmod.Wrap(types.ErrMintWindow, "public mint has ended")
	}
	if c.MintPolicy.PerAddressLimit > 0 {
		used, _ := k.PublicMintCount.Get(ctx, collections.Join(c.Id, buyer))
		if used+uint64(quantity) > uint64(c.MintPolicy.PerAddressLimit) {
			return errorsmod.Wrapf(types.ErrPerAddressLimit, "used %d of %d", used, c.MintPolicy.PerAddressLimit)
		}
	}
	return checkSupply(c, uint64(quantity))
}

func (k msgServer) PublicMint(ctx context.Context, msg *types.MsgPublicMint) (*types.MsgPublicMintResponse, error) {
	buyer, err := k.decodeAddr("buyer", msg.Buyer)
	if err != nil {
		return nil, err
	}
	p := k.GetParams(ctx)
	if err := checkBatch(int(msg.Quantity), p); err != nil {
		return nil, err
	}
	if err := k.validatePrice(ctx, msg.MaxPrice, true); err != nil {
		return nil, err
	}
	c, err := k.getClass(ctx, msg.ClassId)
	if err != nil {
		return nil, err
	}
	if err := k.publicMintCheck(ctx, c, msg.Buyer, msg.Quantity, p); err != nil {
		return nil, err
	}
	price := c.MintPolicy.Price
	if price.Amount.GT(msg.MaxPrice.Amount) {
		return nil, errorsmod.Wrapf(types.ErrPriceExceeded, "price %s > max %s", price, msg.MaxPrice)
	}

	total := price.Amount.MulRaw(int64(msg.Quantity))
	fee := total.MulRaw(int64(p.SaleFeeBps)).QuoRaw(10000)
	if err := k.fundCommunityPool(ctx, buyer, fee); err != nil {
		return nil, err
	}
	if err := k.send(ctx, buyer, c.PayoutAddress, total.Sub(fee)); err != nil {
		return nil, err
	}
	if err := k.collect(ctx, buyer, p.TokenDeposit.MulRaw(int64(msg.Quantity))); err != nil {
		return nil, err
	}

	ids := make([]uint64, 0, msg.Quantity)
	for i := uint32(0); i < msg.Quantity; i++ {
		tokenID := c.NextTokenId
		c.NextTokenId++
		frozen := !c.Flags.TokenMetadataMutable || c.MetadataFrozen
		if _, err := k.createToken(ctx, c.Id, tokenID, msg.Buyer, msg.Buyer, types.TokenMetadata{}, frozen, p.TokenDeposit); err != nil {
			return nil, err
		}
		c.Supply++
		ids = append(ids, tokenID)
	}
	// Counted for the class's life, so a limit set later still sees past mints.
	key := collections.Join(c.Id, msg.Buyer)
	used, _ := k.PublicMintCount.Get(ctx, key)
	if err := k.PublicMintCount.Set(ctx, key, used+uint64(msg.Quantity)); err != nil {
		return nil, err
	}
	if err := k.saveClass(ctx, c); err != nil {
		return nil, err
	}
	idStrs := make([]string, len(ids))
	for i, id := range ids {
		idStrs[i] = u64(id)
	}
	emit(ctx, types.EventPublicMinted, classAttr(c.Id),
		sdk.NewAttribute("buyer", msg.Buyer), sdk.NewAttribute("token_ids", strings.Join(idStrs, ",")),
		sdk.NewAttribute("price", price.String()), sdk.NewAttribute("fee", fee.String()))
	return &types.MsgPublicMintResponse{TokenIds: ids}, nil
}
