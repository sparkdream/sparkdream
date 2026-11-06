package keeper

import (
	"context"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
	commontypes "sparkdream/x/common/types"
)

// Shared issuance path for MsgMint and MsgIssue (docs/x-artifact-spec.md
// §5.2).

// isMinter: the class owner or a listed minter (either may be a migrated
// predecessor of addr).
func (k Keeper) isMinter(ctx context.Context, c types.Class, addr string) bool {
	if k.canActAs(ctx, addr, c.Owner) {
		return true
	}
	for _, m := range c.Minters {
		if k.canActAs(ctx, addr, m) {
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

// forcesInbox reports whether every mint of the class waits for the
// recipient: tokens the holder cannot burn (ISSUER, NEITHER) need consent,
// and a mutual attestation exists only once both parties sign.
func forcesInbox(c types.Class) bool {
	return c.Flags.NeedsConsent()
}

// validateMintContent checks what a minter supplies for one token against
// the class: blind classes mint empty metadata and no schedule (the
// commitment fixes both), everything else passes the metadata rules.
func validateMintContent(ctx context.Context, c types.Class, meta types.TokenMetadata, schedule []types.MetadataStage, p types.Params) error {
	if c.Reveal.Kind == types.RevealKind_REVEAL_KIND_BLIND_METADATA {
		if !meta.IsEmpty() || len(schedule) > 0 {
			return errorsmod.Wrap(types.ErrInvalidReveal, "blind classes mint empty metadata; reveal it with MsgReveal")
		}
		return nil
	}
	if err := types.ValidateTokenMetadata(meta, p); err != nil {
		return err
	}
	return types.ValidateSchedule(schedule, now(ctx), p)
}

// mintOne issues one token of c from minter to recipient: collects the
// deposit, assigns the id, and either creates the token or parks a
// PendingMint in the recipient's inbox. The caller validated the content
// and saves the class.
func (k Keeper) mintOne(ctx context.Context, c *types.Class, minter sdk.AccAddress, minterStr, recipient string,
	meta types.TokenMetadata, schedule []types.MetadataStage, p types.Params,
) (types.MintResult, error) {
	if _, err := k.validateRecipient("recipient", recipient); err != nil {
		return types.MintResult{}, err
	}
	if c.Flags.Mutual && k.sameMember(ctx, recipient, minterStr) {
		return types.MintResult{}, errorsmod.Wrap(types.ErrInvalidRecipient, "a mutual attestation needs a recipient other than the minter")
	}
	tokenID := c.NextTokenId
	c.NextTokenId++

	if k.sameMember(ctx, recipient, minterStr) || (!forcesInbox(*c) && k.deliversDirectly(ctx, minterStr, recipient)) {
		deposit, err := k.takeDeposit(ctx, *c, minter, p)
		if err != nil {
			return types.MintResult{}, err
		}
		t, err := k.createToken(ctx, *c, tokenID, recipient, minterStr, meta, schedule, isBlind(*c), deposit, math.ZeroInt())
		if err != nil {
			return types.MintResult{}, err
		}
		if payer := sessionDepositPayer(ctx, minterStr); payer != "" && deposit.IsPositive() {
			t.DepositPayer = payer
			if err := k.saveToken(ctx, t); err != nil {
				return types.MintResult{}, err
			}
		}
		c.Supply++
		emit(ctx, types.EventMinted, classAttr(c.Id), tokenAttr(tokenID),
			sdk.NewAttribute("minter", minterStr), sdk.NewAttribute("owner", recipient),
			sdk.NewAttribute("deposit", deposit.String()))
		return types.MintResult{TokenId: tokenID, Live: true}, nil
	}

	// The deposit is escrowed until the recipient answers (and, for a
	// NEITHER class, burned on acceptance).
	if err := k.collect(ctx, minter, p.TokenDeposit); err != nil {
		return types.MintResult{}, err
	}
	if err := k.reserveInboxSlot(ctx, types.MintOrigin(c.Id), recipient, p); err != nil {
		return types.MintResult{}, err
	}
	if err := k.burnFrom(ctx, minter, p.InboxFee); err != nil {
		return types.MintResult{}, err
	}
	pm := types.PendingMint{
		ClassId:    c.Id,
		TokenId:    tokenID,
		Minter:     minterStr,
		To:         recipient,
		Metadata:   meta,
		Schedule:   schedule,
		Unrevealed: isBlind(*c),
		Deposit:    p.TokenDeposit,
		CreatedAt:  now(ctx),
		ExpiresAt:  now(ctx) + p.PendingTtl,
		// Read from the minting context, never the accepting one: the
		// recipient may accept through its own session key.
		DepositPayer: sessionDepositPayer(ctx, minterStr),
	}
	if err := k.PendingMints.Set(ctx, collections.Join(c.Id, tokenID), pm); err != nil {
		return types.MintResult{}, err
	}
	if err := k.indexPending(ctx, types.InboxKind_INBOX_KIND_MINT, c.Id, tokenID, minterStr, recipient, pm.ExpiresAt); err != nil {
		return types.MintResult{}, err
	}
	c.ReservedSupply++
	emit(ctx, types.EventMintPending, classAttr(c.Id), tokenAttr(tokenID),
		sdk.NewAttribute("minter", minterStr), sdk.NewAttribute("to", recipient),
		sdk.NewAttribute("expires_at", i64(pm.ExpiresAt)), sdk.NewAttribute("inbox_fee", p.InboxFee.String()))
	return types.MintResult{TokenId: tokenID, Live: false}, nil
}

// sessionDepositPayer returns payer when the message being handled was
// dispatched by an x/session key (MsgExecSession), else "". A deposit paid
// that way records its payer and is refunded to it, not to the holder who
// burns the token or empties the record (SES-101): a delegated key may
// mint to any recipient, and without this a leaked game-server key could
// mint the granter's tokens to itself and burn them for the deposits.
// payer is the message signer, which x/session rewrote to the granter.
func sessionDepositPayer(ctx context.Context, payer string) string {
	if commontypes.SessionExecGranter(ctx) == "" {
		return ""
	}
	return payer
}

// mintPreconditions checks the shared head of MsgMint and MsgIssue.
func (k Keeper) mintPreconditions(ctx context.Context, classID uint64, minter, license string, n uint64) (types.Class, error) {
	if license != types.ContentLicense {
		return types.Class{}, errorsmod.Wrapf(types.ErrContentLicenseNotAccepted, "accepted_content_license must be %q", types.ContentLicense)
	}
	c, err := k.getClass(ctx, classID)
	if err != nil {
		return c, err
	}
	if err := mintable(c); err != nil {
		return c, err
	}
	if !k.isMinter(ctx, c, minter) {
		return c, types.ErrNotMinter
	}
	return c, checkSupply(c, n)
}
