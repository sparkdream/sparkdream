package keeper

import (
	"context"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// isMinter: the class owner or a listed minter.
func isMinter(c types.Class, addr string) bool {
	if c.Owner == addr {
		return true
	}
	for _, m := range c.Minters {
		if m == addr {
			return true
		}
	}
	return false
}

// mintable checks that a class accepts new mints.
func mintable(c types.Class) error {
	if c.Status == types.ContentStatus_CONTENT_STATUS_HIDDEN {
		return types.ErrContentHidden
	}
	if c.MintingClosed {
		return types.ErrMintingClosed
	}
	return nil
}

func (k msgServer) Mint(ctx context.Context, msg *types.MsgMint) (*types.MsgMintResponse, error) {
	minter, err := k.decodeAddr("minter", msg.Minter)
	if err != nil {
		return nil, err
	}
	p := k.GetParams(ctx)
	if err := checkBatch(len(msg.Entries), p); err != nil {
		return nil, err
	}
	if msg.AcceptedContentLicense != types.ContentLicense {
		return nil, errorsmod.Wrapf(types.ErrContentLicenseNotAccepted, "accepted_content_license must be %q", types.ContentLicense)
	}
	c, err := k.getClass(ctx, msg.ClassId)
	if err != nil {
		return nil, err
	}
	if err := mintable(c); err != nil {
		return nil, err
	}
	if !isMinter(c, msg.Minter) {
		return nil, types.ErrNotMinter
	}
	if err := checkSupply(c, uint64(len(msg.Entries))); err != nil {
		return nil, err
	}
	frozen := !c.Flags.TokenMetadataMutable || c.MetadataFrozen

	results := make([]types.MintResult, 0, len(msg.Entries))
	for _, e := range msg.Entries {
		recipient := e.Recipient
		if recipient == "" {
			recipient = msg.Minter
		}
		if _, err := k.validateRecipient("recipient", recipient); err != nil {
			return nil, err
		}
		if err := types.ValidateTokenMetadata(e.Metadata, p); err != nil {
			return nil, err
		}
		if err := k.collect(ctx, minter, p.TokenDeposit); err != nil {
			return nil, err
		}
		tokenID := c.NextTokenId
		c.NextTokenId++

		// A token its holder cannot burn needs the holder's consent, so an
		// issuer-only class always delivers through the inbox (§5.3.5).
		if recipient == msg.Minter || (c.Flags.HolderMayBurn() && k.deliversDirectly(ctx, msg.Minter, recipient)) {
			if _, err := k.createToken(ctx, c.Id, tokenID, recipient, msg.Minter, e.Metadata, frozen, p.TokenDeposit); err != nil {
				return nil, err
			}
			c.Supply++
			results = append(results, types.MintResult{TokenId: tokenID, Live: true})
			emit(ctx, types.EventMinted, classAttr(c.Id), tokenAttr(tokenID),
				sdk.NewAttribute("minter", msg.Minter), sdk.NewAttribute("owner", recipient),
				sdk.NewAttribute("deposit", p.TokenDeposit.String()))
			continue
		}

		if err := k.reserveInboxSlot(ctx, types.MintOrigin(c.Id), recipient, p); err != nil {
			return nil, err
		}
		if err := k.burnFrom(ctx, minter, p.InboxFee); err != nil {
			return nil, err
		}
		pm := types.PendingMint{
			ClassId:   c.Id,
			TokenId:   tokenID,
			Minter:    msg.Minter,
			To:        recipient,
			Metadata:  e.Metadata,
			Deposit:   p.TokenDeposit,
			CreatedAt: now(ctx),
			ExpiresAt: now(ctx) + p.PendingTtl,
		}
		if err := k.PendingMints.Set(ctx, collections.Join(c.Id, tokenID), pm); err != nil {
			return nil, err
		}
		if err := k.indexPending(ctx, types.InboxKind_INBOX_KIND_MINT, c.Id, tokenID, msg.Minter, recipient, pm.ExpiresAt); err != nil {
			return nil, err
		}
		c.ReservedSupply++
		results = append(results, types.MintResult{TokenId: tokenID, Live: false})
		emit(ctx, types.EventMintPending, classAttr(c.Id), tokenAttr(tokenID),
			sdk.NewAttribute("minter", msg.Minter), sdk.NewAttribute("to", recipient),
			sdk.NewAttribute("expires_at", i64(pm.ExpiresAt)), sdk.NewAttribute("inbox_fee", p.InboxFee.String()))
	}
	if err := k.saveClass(ctx, c); err != nil {
		return nil, err
	}
	return &types.MsgMintResponse{Results: results}, nil
}
