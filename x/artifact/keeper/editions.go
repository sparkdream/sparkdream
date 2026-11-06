package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	commontypes "sparkdream/x/common/types"
	reptypes "sparkdream/x/rep/types"

	"sparkdream/x/artifact/types"
)

// Editions: semi-fungible token ids held as per-address balances of
// identical units (docs/x-artifact-spec.md §3.15, §7.20). An edition takes
// its id from Class.next_token_id; Class.supply counts live editions and
// Class.max_supply caps them. Every balance record carries a token_deposit
// paid by whoever created it and refunded to whoever empties it.

// BalanceKey keys a balance: (class_id, edition_id, owner).
type BalanceKey = collections.Triple[uint64, uint64, string]

func balanceKey(classID, editionID uint64, owner string) BalanceKey {
	return collections.Join3(classID, editionID, owner)
}

// unitOrigin is the inbox namespace for pending unit items: class 0 is
// never a real class, so (0, pending_id) cannot collide with a token ref.
const unitNamespace uint64 = 0

// issuedUnits is the count an edition's max_supply applies to.
func issuedUnits(e types.Edition) uint64 { return e.Supply + e.Burned + e.Reserved }

// checkUnitSupply checks that n more units fit the edition: under the
// compiled HardMaxEditionSupply always (an uncapped edition included) and
// under max_supply when set. The sum is checked, never wrapped.
func checkUnitSupply(e types.Edition, n uint64) error {
	issued, err := types.AddUnits(issuedUnits(e), n)
	if err != nil {
		return err
	}
	if e.MaxSupply > 0 && issued > e.MaxSupply {
		return errorsmod.Wrapf(types.ErrSupplyExceeded, "edition issued %d + %d > max %d", issuedUnits(e), n, e.MaxSupply)
	}
	return nil
}

func (k Keeper) getEditionsClass(ctx context.Context, classID uint64) (types.Class, error) {
	c, err := k.getClass(ctx, classID)
	if err != nil {
		return c, err
	}
	if !c.IsEditions() {
		return c, errorsmod.Wrapf(types.ErrWrongTokenModel, "class %d does not issue editions", classID)
	}
	return c, nil
}

func (k Keeper) getEdition(ctx context.Context, classID, editionID uint64) (types.Edition, error) {
	e, err := k.Editions.Get(ctx, collections.Join(classID, editionID))
	if errors.Is(err, collections.ErrNotFound) {
		return e, errorsmod.Wrapf(types.ErrEditionNotFound, "edition %d/%d", classID, editionID)
	}
	return e, err
}

func (k Keeper) saveEdition(ctx context.Context, e types.Edition) error {
	return k.Editions.Set(ctx, collections.Join(e.ClassId, e.Id), e)
}

// isEditionHidden reports whether an edition or its class is hidden.
func isEditionHidden(c types.Class, e types.Edition) bool {
	return c.Status == types.ContentStatus_CONTENT_STATUS_HIDDEN || e.Status == types.ContentStatus_CONTENT_STATUS_HIDDEN
}

// getBalance returns the stored record, or a zero record and false.
func (k Keeper) getBalance(ctx context.Context, classID, editionID uint64, owner string) (types.Balance, bool, error) {
	b, err := k.Balances.Get(ctx, balanceKey(classID, editionID, owner))
	if errors.Is(err, collections.ErrNotFound) {
		return types.Balance{ClassId: classID, EditionId: editionID, Owner: owner,
			Deposit: math.ZeroInt(), RewardDebt: math.ZeroInt(), Unclaimed: math.ZeroInt()}, false, nil
	}
	return b, err == nil, err
}

// balanceHolder resolves which of signer's addresses a handler should debit
// or lock units from: signer itself when it holds a record in the edition,
// else the first recent predecessor that does (a balance written before the
// member migrated its key stays keyed by the old address; nothing is
// re-keyed). Only addresses signer may act as count. A tombstoned signer
// acts for nothing, not even its own address (ART-107).
func (k Keeper) balanceHolder(ctx context.Context, classID, editionID uint64, signer string) (string, error) {
	if !k.canActAs(ctx, signer, signer) {
		return "", errorsmod.Wrap(types.ErrNotAuthorized, "a migrated address cannot act; sign with the successor")
	}
	return k.unitsHolderOf(ctx, classID, editionID, signer), nil
}

// unitsHolderOf is balanceHolder's lookup without the signer check, for an
// issuer naming the holder (MsgRevokeUnits): either address of a migrated
// holder may be named. Falls back to named.
func (k Keeper) unitsHolderOf(ctx context.Context, classID, editionID uint64, named string) string {
	for _, a := range k.recentAddressesOf(ctx, named) {
		if !k.canActAs(ctx, named, a) {
			continue
		}
		if has, _ := k.Balances.Has(ctx, balanceKey(classID, editionID, a)); has {
			return a
		}
	}
	return named
}

// settle folds what a balance accrued into unclaimed. Call it before any
// change to the balance's units.
func settle(b *types.Balance, e types.Edition) {
	b.Unclaimed = b.Claimable(e.RewardPerUnit)
	b.RewardDebt = e.RewardPerUnit
}

// writeBalance stores b, or, when it holds no units, deletes the record and
// pays its holder the record deposit and any unclaimed rewards. The caller
// saves the edition (holders count).
func (k Keeper) writeBalance(ctx context.Context, e *types.Edition, b types.Balance) error {
	key := balanceKey(b.ClassId, b.EditionId, b.Owner)
	has, err := k.Balances.Has(ctx, key)
	if err != nil {
		return err
	}
	if b.Units() == 0 {
		if !has {
			return nil
		}
		if err := k.Balances.Remove(ctx, key); err != nil {
			return err
		}
		if err := k.BalancesByOwner.Remove(ctx, collections.Join3(b.Owner, b.ClassId, b.EditionId)); err != nil {
			return err
		}
		e.Holders--
		depositTo := b.Owner
		if b.DepositPayer != "" {
			depositTo = b.DepositPayer // paid by a session key (SES-101)
		}
		if err := k.refund(ctx, depositTo, b.Deposit); err != nil {
			return err
		}
		return k.refund(ctx, b.Owner, b.Unclaimed)
	}
	if !has {
		e.Holders++
		if err := k.BalancesByOwner.Set(ctx, collections.Join3(b.Owner, b.ClassId, b.EditionId)); err != nil {
			return err
		}
	}
	return k.Balances.Set(ctx, key, b)
}

// credit adds free units to to's balance. A new record takes escrow as its
// deposit when escrowed is set (recording escrowPayer, the session-paid
// payer of the escrow, if any), or collects token_deposit from payer (the
// message signer); an escrow that is not needed (the record already
// exists) is refunded to escrowFrom.
func (k Keeper) credit(ctx context.Context, e *types.Edition, to string, amount uint64,
	payer sdk.AccAddress, escrowed bool, escrow math.Int, escrowFrom, escrowPayer string,
) error {
	b, found, err := k.getBalance(ctx, e.ClassId, e.Id, to)
	if err != nil {
		return err
	}
	switch {
	case found:
		settle(&b, *e)
		if escrowed {
			if err := k.refund(ctx, escrowFrom, escrow); err != nil {
				return err
			}
		}
	case escrowed:
		b.Deposit = escrow
		b.DepositPayer = escrowPayer
		b.RewardDebt = e.RewardPerUnit
	default:
		dep := k.GetParams(ctx).TokenDeposit
		if err := k.collect(ctx, payer, dep); err != nil {
			return err
		}
		b.Deposit = dep
		if commontypes.SessionExecGranter(ctx) != "" {
			// payer is the signer, rewritten to the granter (SES-101).
			if b.DepositPayer, err = k.addressCodec.BytesToString(payer); err != nil {
				return err
			}
		}
		b.RewardDebt = e.RewardPerUnit
	}
	if b.Amount, err = types.AddUnits(b.Amount, amount); err != nil {
		return err
	}
	return k.writeBalance(ctx, e, b)
}

// debit removes units from owner's balance, free units or (fromLocked)
// locked ones.
func (k Keeper) debit(ctx context.Context, e *types.Edition, owner string, amount uint64, fromLocked bool) error {
	b, found, err := k.getBalance(ctx, e.ClassId, e.Id, owner)
	if err != nil {
		return err
	}
	have := b.Amount
	if fromLocked {
		have = b.Locked
	}
	if !found || have < amount {
		return errorsmod.Wrapf(types.ErrInsufficientUnits, "%s holds %d of edition %d/%d", owner, have, e.ClassId, e.Id)
	}
	settle(&b, *e)
	if fromLocked {
		b.Locked -= amount
	} else {
		b.Amount -= amount
	}
	return k.writeBalance(ctx, e, b)
}

// lockUnits moves free units to locked (listings, pending transfers);
// unlock reverses it. Rewards keep accruing to the holder either way.
func (k Keeper) lockUnits(ctx context.Context, e *types.Edition, owner string, amount uint64, unlock bool) error {
	b, found, err := k.getBalance(ctx, e.ClassId, e.Id, owner)
	if err != nil {
		return err
	}
	from := b.Amount
	if unlock {
		from = b.Locked
	}
	if !found || from < amount {
		return errorsmod.Wrapf(types.ErrInsufficientUnits, "%s holds %d of edition %d/%d", owner, from, e.ClassId, e.Id)
	}
	if unlock {
		b.Locked -= amount
		if b.Amount, err = types.AddUnits(b.Amount, amount); err != nil {
			return err
		}
	} else {
		b.Amount -= amount
		if b.Locked, err = types.AddUnits(b.Locked, amount); err != nil {
			return err
		}
	}
	return k.writeBalance(ctx, e, b)
}

// ---------------------------------------------------------------------------
// Issuance
// ---------------------------------------------------------------------------

// validateImpactClaim checks a claim and, for an initiative certificate,
// that the initiative is completed, the signer is its assignee and no
// certificate exists for it yet. It returns the claim to store (an
// initiative claim without contributors credits the assignee).
func (k Keeper) validateImpactClaim(ctx context.Context, claim types.ImpactClaim, signer string, p types.Params) (types.ImpactClaim, error) {
	if err := types.ValidateImpactClaim(claim, p); err != nil {
		return claim, err
	}
	for _, a := range claim.Contributors {
		if _, err := k.decodeAddr("contributor", a); err != nil {
			return claim, errorsmod.Wrap(types.ErrInvalidClaim, err.Error())
		}
	}
	if claim.InitiativeId == 0 {
		return claim, nil
	}
	if k.late.rep == nil {
		return claim, errorsmod.Wrap(types.ErrInitiativeNotEligible, "rep keeper not wired")
	}
	init, err := k.late.rep.GetInitiative(ctx, claim.InitiativeId)
	if err != nil {
		return claim, errorsmod.Wrapf(types.ErrInitiativeNotEligible, "initiative %d: %v", claim.InitiativeId, err)
	}
	if init.Status != reptypes.InitiativeStatus_INITIATIVE_STATUS_COMPLETED {
		return claim, errorsmod.Wrapf(types.ErrInitiativeNotEligible, "initiative %d is %s", claim.InitiativeId, init.Status)
	}
	if !k.canActAs(ctx, signer, init.Assignee) {
		return claim, errorsmod.Wrap(types.ErrInitiativeNotEligible, "only the initiative's assignee may issue its certificate")
	}
	if has, err := k.hasInitiativeCertificate(ctx, claim.InitiativeId); err != nil {
		return claim, err
	} else if has {
		return claim, errorsmod.Wrapf(types.ErrCertificateExists, "initiative %d already has a certificate", claim.InitiativeId)
	}
	if len(claim.Contributors) == 0 {
		claim.Contributors = []string{init.Assignee}
	}
	return claim, nil
}

func (k Keeper) hasInitiativeCertificate(ctx context.Context, initiativeID uint64) (bool, error) {
	iter, err := k.CertificatesByInitiative.Iterate(ctx, collections.NewPrefixedTripleRange[uint64, uint64, uint64](initiativeID))
	if err != nil {
		return false, err
	}
	defer iter.Close()
	return iter.Valid(), nil
}

// mintUnitsTo issues amount units of e to recipient: directly, or as a
// pending mint in the recipient's inbox (origin "class/<id>"). The caller
// checked the edition cap and saves class and edition.
func (k Keeper) mintUnitsTo(ctx context.Context, c types.Class, e *types.Edition, minter sdk.AccAddress, minterStr, recipient string,
	amount uint64, p types.Params,
) (types.UnitResult, error) {
	res := types.UnitResult{Recipient: recipient, Amount: amount}
	if amount == 0 {
		return res, errorsmod.Wrap(types.ErrInvalidBatch, "amount must be positive")
	}
	if _, err := k.validateRecipient("recipient", recipient); err != nil {
		return res, err
	}
	// Minting to oneself (any of one's own addresses) is always direct, as
	// on the unique-token path.
	if k.sameMember(ctx, recipient, minterStr) || (!c.Flags.NeedsConsent() && k.deliversDirectly(ctx, minterStr, recipient)) {
		if err := k.credit(ctx, e, recipient, amount, minter, false, math.Int{}, "", ""); err != nil {
			return res, err
		}
		supply, err := types.AddUnits(e.Supply, amount)
		if err != nil {
			return res, err
		}
		e.Supply = supply
		res.Delivered = true
		emit(ctx, types.EventUnitsMinted, classAttr(c.Id), editionAttr(e.Id),
			sdk.NewAttribute("minter", minterStr), sdk.NewAttribute("owner", recipient), sdk.NewAttribute("amount", u64(amount)))
		return res, nil
	}
	id, err := k.parkUnits(ctx, types.InboxKind_INBOX_KIND_UNIT_MINT, c, e, minter, minterStr, recipient, amount, types.MintOrigin(c.Id), p)
	if err != nil {
		return res, err
	}
	if e.Reserved, err = types.AddUnits(e.Reserved, amount); err != nil {
		return res, err
	}
	res.PendingId = id
	return res, nil
}

// parkUnits writes a pending unit item: both inbox caps, the burned inbox
// fee and an escrowed record deposit, all paid by from.
func (k Keeper) parkUnits(ctx context.Context, kind types.InboxKind, c types.Class, e *types.Edition, from sdk.AccAddress, fromStr, to string,
	amount uint64, origin string, p types.Params,
) (uint64, error) {
	if err := k.reserveInboxSlot(ctx, origin, to, p); err != nil {
		return 0, err
	}
	if err := k.burnFrom(ctx, from, p.InboxFee); err != nil {
		return 0, err
	}
	if err := k.collect(ctx, from, p.TokenDeposit); err != nil {
		return 0, err
	}
	seq, err := k.PendingUnitSeq.Next(ctx)
	if err != nil {
		return 0, err
	}
	pu := types.PendingUnits{
		Id:        seq + 1, // ids start at 1
		Kind:      kind,
		ClassId:   c.Id,
		EditionId: e.Id,
		From:      fromStr,
		To:        to,
		Amount:    amount,
		Deposit:   p.TokenDeposit,
		CreatedAt: now(ctx),
		ExpiresAt: now(ctx) + p.PendingTtl,
		// from paid the escrow; recorded when it was a session key.
		DepositPayer: sessionDepositPayer(ctx, fromStr),
	}
	if err := k.PendingUnits.Set(ctx, pu.Id, pu); err != nil {
		return 0, err
	}
	if err := k.indexPending(ctx, kind, unitNamespace, pu.Id, fromStr, to, pu.ExpiresAt); err != nil {
		return 0, err
	}
	emit(ctx, types.EventUnitsPending, classAttr(c.Id), editionAttr(e.Id),
		sdk.NewAttribute("pending_id", u64(pu.Id)), sdk.NewAttribute("kind", kind.String()),
		sdk.NewAttribute("from", fromStr), sdk.NewAttribute("to", to), sdk.NewAttribute("amount", u64(amount)),
		sdk.NewAttribute("expires_at", i64(pu.ExpiresAt)), sdk.NewAttribute("inbox_fee", p.InboxFee.String()))
	return pu.Id, nil
}

// unitOriginOf is the inbox-cap origin a pending unit item was charged to.
func unitOriginOf(pu types.PendingUnits) string {
	if pu.Kind == types.InboxKind_INBOX_KIND_UNIT_MINT {
		return types.MintOrigin(pu.ClassId)
	}
	return pu.From
}

// resolvePendingUnits settles a pending unit item. Accepting credits the
// recipient (the escrowed deposit funds a new record, or goes back to the
// sender); anything else returns the units and the escrow.
func (k Keeper) resolvePendingUnits(ctx context.Context, pu types.PendingUnits, outcome PendingOutcome) error {
	if err := k.PendingUnits.Remove(ctx, pu.Id); err != nil {
		return err
	}
	if err := k.unindexPending(ctx, unitNamespace, pu.Id, pu.From, pu.To, pu.ExpiresAt); err != nil {
		return err
	}
	if err := k.releaseInboxSlot(ctx, unitOriginOf(pu), pu.To); err != nil {
		return err
	}
	e, err := k.getEdition(ctx, pu.ClassId, pu.EditionId)
	if err != nil {
		return err
	}
	mint := pu.Kind == types.InboxKind_INBOX_KIND_UNIT_MINT
	if mint {
		e.Reserved -= pu.Amount
	}
	refund := math.ZeroInt()
	if outcome == OutcomeAccepted {
		if !mint {
			if err := k.debit(ctx, &e, pu.From, pu.Amount, true); err != nil {
				return err
			}
		} else if e.Supply, err = types.AddUnits(e.Supply, pu.Amount); err != nil {
			return err
		}
		if err := k.credit(ctx, &e, pu.To, pu.Amount, nil, true, pu.Deposit, pu.From, pu.DepositPayer); err != nil {
			return err
		}
		emit(ctx, types.EventUnitsTransferred, classAttr(pu.ClassId), editionAttr(pu.EditionId),
			sdk.NewAttribute("from", pu.From), sdk.NewAttribute("to", pu.To), sdk.NewAttribute("amount", u64(pu.Amount)),
			sdk.NewAttribute("via", "accept"))
	} else {
		if !mint {
			if err := k.lockUnits(ctx, &e, pu.From, pu.Amount, true); err != nil {
				return err
			}
		}
		refund = pu.Deposit
		if err := k.refund(ctx, pu.From, pu.Deposit); err != nil {
			return err
		}
	}
	if err := k.saveEdition(ctx, e); err != nil {
		return err
	}
	kind := "unit_transfer"
	if mint {
		kind = "unit_mint"
	}
	emit(ctx, types.EventPendingResolved, classAttr(pu.ClassId), sdk.NewAttribute("pending_id", u64(pu.Id)),
		sdk.NewAttribute("kind", kind), sdk.NewAttribute("outcome", string(outcome)), sdk.NewAttribute("refund", refund.String()))
	return nil
}

// ---------------------------------------------------------------------------
// Unit listings
// ---------------------------------------------------------------------------

// removeUnitListing deletes a unit listing and its indexes and unlocks its
// remaining units.
func (k Keeper) removeUnitListing(ctx context.Context, l types.UnitListing, reason string) error {
	if err := k.UnitListings.Remove(ctx, l.Id); err != nil {
		return err
	}
	if err := k.UnitListingsByEdition.Remove(ctx, collections.Join3(l.ClassId, l.EditionId, l.Id)); err != nil {
		return err
	}
	if err := k.UnitListingsBySeller.Remove(ctx, collections.Join(l.Seller, l.Id)); err != nil {
		return err
	}
	if err := k.UnitListingExpiry.Remove(ctx, collections.Join(l.ExpiresAt, l.Id)); err != nil {
		return err
	}
	if l.Quantity > 0 {
		e, err := k.getEdition(ctx, l.ClassId, l.EditionId)
		if err != nil {
			return err
		}
		if err := k.lockUnits(ctx, &e, l.Seller, l.Quantity, true); err != nil {
			return err
		}
		if err := k.saveEdition(ctx, e); err != nil {
			return err
		}
	}
	emit(ctx, types.EventUnitsDelisted, classAttr(l.ClassId), editionAttr(l.EditionId),
		sdk.NewAttribute("listing_id", u64(l.Id)), sdk.NewAttribute("seller", l.Seller), sdk.NewAttribute("reason", reason))
	return nil
}

func (k Keeper) getUnitListing(ctx context.Context, id uint64) (types.UnitListing, error) {
	l, err := k.UnitListings.Get(ctx, id)
	if errors.Is(err, collections.ErrNotFound) {
		return l, errorsmod.Wrapf(types.ErrListingNotFound, "unit listing %d", id)
	}
	return l, err
}

func editionAttr(id uint64) sdk.Attribute { return sdk.NewAttribute("edition_id", u64(id)) }
