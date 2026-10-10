package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	commontypes "sparkdream/x/common/types"

	"sparkdream/x/artifact/types"
)

// ---------------------------------------------------------------------------
// Classes
// ---------------------------------------------------------------------------

// setClassOwner moves the ClassesByOwner index entry and the owner field.
func (k Keeper) setClassOwner(ctx context.Context, c *types.Class, newOwner string) error {
	if err := k.ClassesByOwner.Remove(ctx, collections.Join(c.Owner, c.Id)); err != nil {
		return err
	}
	c.Owner = newOwner
	return k.ClassesByOwner.Set(ctx, collections.Join(newOwner, c.Id))
}

func (k Keeper) saveClass(ctx context.Context, c types.Class) error {
	c.UpdatedAt = now(ctx)
	return k.Classes.Set(ctx, c.Id, c)
}

func (k Keeper) adjustCreatorCount(ctx context.Context, creator string, delta int64) error {
	cur, err := k.ClassCountByCreator.Get(ctx, creator)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return err
	}
	next := int64(cur) + delta
	if next <= 0 {
		return k.ClassCountByCreator.Remove(ctx, creator)
	}
	return k.ClassCountByCreator.Set(ctx, creator, uint64(next))
}

// issued is the count the supply cap applies to (docs/x-artifact-spec.md §3.1).
func issued(c types.Class) uint64 { return c.Supply + c.Burned + c.ReservedSupply }

func checkSupply(c types.Class, n uint64) error {
	if c.MaxSupply > 0 && issued(c)+n > c.MaxSupply {
		return errorsmod.Wrapf(types.ErrSupplyExceeded, "issued %d + %d > max %d", issued(c), n, c.MaxSupply)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Tokens
// ---------------------------------------------------------------------------

// createToken is the single path that brings a token into existence. The
// caller increments class supply and saves the class.
func (k Keeper) createToken(ctx context.Context, classID, tokenID uint64, owner, minter string,
	meta types.TokenMetadata, frozen bool, deposit math.Int,
) (types.Token, error) {
	t := types.Token{
		ClassId:           classID,
		Id:                tokenID,
		Owner:             owner,
		Minter:            minter,
		Metadata:          meta,
		MetadataFrozen:    frozen,
		Lock:              types.TokenLock_TOKEN_LOCK_NONE,
		Deposit:           deposit,
		Status:            types.ContentStatus_CONTENT_STATUS_ACTIVE,
		MediaFlags:        types.LabelToken(meta),
		MediaRulesVersion: commontypes.MediaRulesVersion,
		MintedAt:          now(ctx),
	}
	if err := k.Tokens.Set(ctx, collections.Join(classID, tokenID), t); err != nil {
		return t, err
	}
	return t, k.TokensByOwner.Set(ctx, collections.Join3(owner, classID, tokenID))
}

// moveToken is the only code path that changes Token.owner on an existing
// token (docs/x-artifact-spec.md §7.1). The caller clears any lock first.
func (k Keeper) moveToken(ctx context.Context, t *types.Token, newOwner string) error {
	if t.Lock != types.TokenLock_TOKEN_LOCK_NONE {
		return errorsmod.Wrap(types.ErrTokenLocked, "moveToken requires an unlocked token")
	}
	if err := k.TokensByOwner.Remove(ctx, collections.Join3(t.Owner, t.ClassId, t.Id)); err != nil {
		return err
	}
	t.Owner = newOwner
	if err := k.TokensByOwner.Set(ctx, collections.Join3(newOwner, t.ClassId, t.Id)); err != nil {
		return err
	}
	return k.Tokens.Set(ctx, collections.Join(t.ClassId, t.Id), *t)
}

func (k Keeper) saveToken(ctx context.Context, t types.Token) error {
	return k.Tokens.Set(ctx, collections.Join(t.ClassId, t.Id), t)
}

// destroyToken removes a token (burn or revoke), refunding its deposit to
// refundTo. Any listing is removed first; a pending transfer must already be
// resolved. Resolves an open hide record as TARGET_BURNED.
func (k Keeper) destroyToken(ctx context.Context, c *types.Class, t types.Token, refundTo string) error {
	if t.Lock == types.TokenLock_TOKEN_LOCK_LISTED {
		if err := k.removeListing(ctx, t.ClassId, t.Id, "burned"); err != nil {
			return err
		}
		t.Lock = types.TokenLock_TOKEN_LOCK_NONE
	}
	if t.Lock != types.TokenLock_TOKEN_LOCK_NONE {
		return errorsmod.Wrap(types.ErrTokenLocked, "cancel the pending transfer first")
	}
	if err := k.Tokens.Remove(ctx, collections.Join(t.ClassId, t.Id)); err != nil {
		return err
	}
	if err := k.TokensByOwner.Remove(ctx, collections.Join3(t.Owner, t.ClassId, t.Id)); err != nil {
		return err
	}
	c.Supply--
	c.Burned++
	if err := k.refund(ctx, refundTo, t.Deposit); err != nil {
		return err
	}
	return k.resolveHideOnBurn(ctx, t.ClassId, t.Id)
}

// ---------------------------------------------------------------------------
// Inbox
// ---------------------------------------------------------------------------

func (k Keeper) getCount(ctx context.Context, m collections.Map[string, uint64], key string) uint64 {
	v, _ := m.Get(ctx, key)
	return v
}

// reserveInboxSlot enforces both inbox caps and bumps the counters (§7.6).
func (k Keeper) reserveInboxSlot(ctx context.Context, origin, to string, p types.Params) error {
	pairKey := collections.Join(origin, to)
	pair, _ := k.PendingPairCount.Get(ctx, pairKey)
	if pair >= uint64(p.MaxPendingPerPair) {
		return errorsmod.Wrapf(types.ErrInboxFull, "%d open items from this origin", pair)
	}
	total := k.getCount(ctx, k.InboxCount, to)
	if total >= uint64(p.MaxInboxPerRecipient) {
		return errorsmod.Wrapf(types.ErrInboxFull, "recipient has %d open items", total)
	}
	if err := k.PendingPairCount.Set(ctx, pairKey, pair+1); err != nil {
		return err
	}
	return k.InboxCount.Set(ctx, to, total+1)
}

func (k Keeper) releaseInboxSlot(ctx context.Context, origin, to string) error {
	pairKey := collections.Join(origin, to)
	pair, _ := k.PendingPairCount.Get(ctx, pairKey)
	if pair <= 1 {
		if err := k.PendingPairCount.Remove(ctx, pairKey); err != nil {
			return err
		}
	} else if err := k.PendingPairCount.Set(ctx, pairKey, pair-1); err != nil {
		return err
	}
	total := k.getCount(ctx, k.InboxCount, to)
	if total <= 1 {
		return k.InboxCount.Remove(ctx, to)
	}
	return k.InboxCount.Set(ctx, to, total-1)
}

func (k Keeper) indexPending(ctx context.Context, kind types.InboxKind, classID, tokenID uint64, from, to string, expiresAt int64) error {
	if err := k.InboxByRecipient.Set(ctx, collections.Join3(to, classID, tokenID), uint64(kind)); err != nil {
		return err
	}
	if err := k.OutboxBySender.Set(ctx, collections.Join3(from, classID, tokenID), uint64(kind)); err != nil {
		return err
	}
	return k.PendingExpiry.Set(ctx, collections.Join3(expiresAt, classID, tokenID))
}

func (k Keeper) unindexPending(ctx context.Context, classID, tokenID uint64, from, to string, expiresAt int64) error {
	if err := k.InboxByRecipient.Remove(ctx, collections.Join3(to, classID, tokenID)); err != nil {
		return err
	}
	if err := k.OutboxBySender.Remove(ctx, collections.Join3(from, classID, tokenID)); err != nil {
		return err
	}
	return k.PendingExpiry.Remove(ctx, collections.Join3(expiresAt, classID, tokenID))
}

// PendingOutcome is how an inbox item resolved.
type PendingOutcome string

const (
	OutcomeAccepted  PendingOutcome = "accepted"
	OutcomeRejected  PendingOutcome = "rejected"
	OutcomeCancelled PendingOutcome = "cancelled"
	OutcomeExpired   PendingOutcome = "expired"
)

// resolvePendingTransfer settles a pending transfer. Accepted moves the
// token to the recipient; anything else returns it to the sender.
func (k Keeper) resolvePendingTransfer(ctx context.Context, pt types.PendingTransfer, outcome PendingOutcome) error {
	t, err := k.getToken(ctx, pt.ClassId, pt.TokenId)
	if err != nil {
		return err
	}
	if err := k.PendingTransfers.Remove(ctx, collections.Join(pt.ClassId, pt.TokenId)); err != nil {
		return err
	}
	if err := k.unindexPending(ctx, pt.ClassId, pt.TokenId, pt.From, pt.To, pt.ExpiresAt); err != nil {
		return err
	}
	if err := k.releaseInboxSlot(ctx, pt.From, pt.To); err != nil {
		return err
	}
	t.Lock = types.TokenLock_TOKEN_LOCK_NONE
	if outcome == OutcomeAccepted {
		if err := k.moveToken(ctx, &t, pt.To); err != nil {
			return err
		}
		emit(ctx, types.EventTransferred, classAttr(pt.ClassId), tokenAttr(pt.TokenId),
			sdk.NewAttribute("from", pt.From), sdk.NewAttribute("to", pt.To), sdk.NewAttribute("via", "accept"))
	} else if err := k.saveToken(ctx, t); err != nil {
		return err
	}
	emit(ctx, types.EventPendingResolved, classAttr(pt.ClassId), tokenAttr(pt.TokenId),
		sdk.NewAttribute("kind", "transfer"), sdk.NewAttribute("outcome", string(outcome)), sdk.NewAttribute("refund", "0"))
	return nil
}

// resolvePendingMint settles a pending mint. Accepted creates the token;
// anything else frees the reserved slot and refunds the deposit to the
// minter. The token id stays consumed.
func (k Keeper) resolvePendingMint(ctx context.Context, pm types.PendingMint, outcome PendingOutcome) error {
	c, err := k.getClass(ctx, pm.ClassId)
	if err != nil {
		return err
	}
	if err := k.PendingMints.Remove(ctx, collections.Join(pm.ClassId, pm.TokenId)); err != nil {
		return err
	}
	if err := k.unindexPending(ctx, pm.ClassId, pm.TokenId, pm.Minter, pm.To, pm.ExpiresAt); err != nil {
		return err
	}
	if err := k.releaseInboxSlot(ctx, types.MintOrigin(pm.ClassId), pm.To); err != nil {
		return err
	}
	c.ReservedSupply--
	refund := math.ZeroInt()
	if outcome == OutcomeAccepted {
		frozen := !c.Flags.TokenMetadataMutable || c.MetadataFrozen
		if _, err := k.createToken(ctx, pm.ClassId, pm.TokenId, pm.To, pm.Minter, pm.Metadata, frozen, pm.Deposit); err != nil {
			return err
		}
		c.Supply++
		emit(ctx, types.EventMinted, classAttr(pm.ClassId), tokenAttr(pm.TokenId),
			sdk.NewAttribute("minter", pm.Minter), sdk.NewAttribute("owner", pm.To), sdk.NewAttribute("deposit", pm.Deposit.String()))
	} else {
		refund = pm.Deposit
		if err := k.refund(ctx, pm.Minter, pm.Deposit); err != nil {
			return err
		}
	}
	if err := k.saveClass(ctx, c); err != nil {
		return err
	}
	emit(ctx, types.EventPendingResolved, classAttr(pm.ClassId), tokenAttr(pm.TokenId),
		sdk.NewAttribute("kind", "mint"), sdk.NewAttribute("outcome", string(outcome)), sdk.NewAttribute("refund", refund.String()))
	return nil
}

// resolvePending dispatches on the item kind.
func (k Keeper) resolvePending(ctx context.Context, classID, tokenID uint64, outcome PendingOutcome) error {
	key := collections.Join(classID, tokenID)
	if pt, err := k.PendingTransfers.Get(ctx, key); err == nil {
		return k.resolvePendingTransfer(ctx, pt, outcome)
	}
	if pm, err := k.PendingMints.Get(ctx, key); err == nil {
		return k.resolvePendingMint(ctx, pm, outcome)
	}
	return errorsmod.Wrapf(types.ErrPendingNotFound, "%d/%d", classID, tokenID)
}

// ---------------------------------------------------------------------------
// Listings
// ---------------------------------------------------------------------------

// removeListing deletes a listing and its indexes and clears the token lock.
func (k Keeper) removeListing(ctx context.Context, classID, tokenID uint64, reason string) error {
	key := collections.Join(classID, tokenID)
	l, err := k.Listings.Get(ctx, key)
	if err != nil {
		return errorsmod.Wrapf(types.ErrListingNotFound, "%d/%d", classID, tokenID)
	}
	if err := k.Listings.Remove(ctx, key); err != nil {
		return err
	}
	if err := k.ListingsBySeller.Remove(ctx, collections.Join3(l.Seller, classID, tokenID)); err != nil {
		return err
	}
	if err := k.ListingExpiry.Remove(ctx, collections.Join3(l.ExpiresAt, classID, tokenID)); err != nil {
		return err
	}
	t, err := k.getToken(ctx, classID, tokenID)
	if err == nil && t.Lock == types.TokenLock_TOKEN_LOCK_LISTED {
		t.Lock = types.TokenLock_TOKEN_LOCK_NONE
		if err := k.saveToken(ctx, t); err != nil {
			return err
		}
	}
	emit(ctx, types.EventDelisted, classAttr(classID), tokenAttr(tokenID),
		sdk.NewAttribute("seller", l.Seller), sdk.NewAttribute("reason", reason))
	return nil
}

// Settlement is the split of one sale (docs/x-artifact-spec.md §7.7).
type Settlement struct {
	Fee, Royalty, Proceeds math.Int
}

// ComputeSettlement splits price with integer floor division. Royalty is
// clamped to the current max_royalty_bps; fee + royalty + proceeds == price.
func ComputeSettlement(price math.Int, saleFeeBps, classRoyaltyBps, maxRoyaltyBps uint32) Settlement {
	royaltyBps := classRoyaltyBps
	if royaltyBps > maxRoyaltyBps {
		royaltyBps = maxRoyaltyBps
	}
	fee := price.MulRaw(int64(saleFeeBps)).QuoRaw(10000)
	royalty := price.MulRaw(int64(royaltyBps)).QuoRaw(10000)
	proceeds := price.Sub(fee).Sub(royalty)
	if proceeds.IsNegative() {
		// Unreachable while the hard bounds keep fee + royalty <= 35%.
		panic(fmt.Sprintf("artifact settlement: negative proceeds for price %s", price))
	}
	return Settlement{Fee: fee, Royalty: royalty, Proceeds: proceeds}
}

// SplitRoyalty divides a royalty across a class's shares with floor
// division. The remainder goes to the first share, so the parts sum to
// royalty exactly. Shares are assumed valid (types.ValidateRoyaltyShares).
func SplitRoyalty(royalty math.Int, shares []types.RoyaltyShare) []math.Int {
	parts := make([]math.Int, len(shares))
	rest := royalty
	for i, s := range shares {
		parts[i] = royalty.MulRaw(int64(s.WeightBps)).QuoRaw(int64(types.RoyaltyWeightTotal))
		rest = rest.Sub(parts[i])
	}
	if len(parts) > 0 {
		parts[0] = parts[0].Add(rest)
	}
	return parts
}
