package types

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
)

// Waves 3 and 4 of docs/x-artifact-spec.md §16.2: editions (semi-fungible
// tokens) and impact certificates, crafting, rentals, nesting and Harberger
// tokens, plus the NEITHER burn mode.

const (
	// MaxImpactContributors bounds ImpactClaim.contributors.
	MaxImpactContributors = 20
	// MaxCraftInputs bounds the ingredients of one recipe.
	MaxCraftInputs = 8
	// HardMaxEditionSupply is the compiled ceiling on an edition's issued
	// units (supply + burned + reserved). It applies to an uncapped edition
	// (max_supply 0) too, and bounds max_supply and a recipe's
	// output_quantity, so no unit counter or balance can come near 2^64.
	HardMaxEditionSupply uint64 = 1_000_000_000_000_000
	// MaxHarbergerTaxBps bounds HarbergerPolicy.tax_bps_per_year (100% a year).
	MaxHarbergerTaxBps uint32 = 10000
	// SecondsPerYear is the year Harberger taxes are prorated over.
	SecondsPerYear int64 = 365 * 24 * 3600
)

// RewardScale is the fixed-point scale of Edition.reward_per_unit.
var RewardScale = math.NewIntWithDecimal(1, 18)

// ---------------------------------------------------------------------------
// Class shape
// ---------------------------------------------------------------------------

// IsEditions reports whether the class issues semi-fungible editions.
func (c Class) IsEditions() bool { return c.TokenModel == TokenModel_TOKEN_MODEL_EDITIONS }

// IsHarberger reports whether the class's tokens are Harberger tokens.
func (c Class) IsHarberger() bool { return c.Harberger.Enabled() }

// NobodyMayBurn reports the NEITHER burn mode: the token can never be
// destroyed, so it carries no refundable deposit (the deposit is burned as
// a fee) and nobody may be given one without accepting it.
func (f ClassFlags) NobodyMayBurn() bool {
	return f.BurnAuthorization == BurnAuthorization_BURN_AUTHORIZATION_NEITHER
}

// NeedsConsent reports whether delivering a token of the class to anyone
// but the signer must wait in the recipient's inbox: the holder could never
// get rid of it (ISSUER, NEITHER), or it is a mutual attestation.
func (f ClassFlags) NeedsConsent() bool {
	return !f.HolderMayBurn() || f.Mutual
}

// Enabled reports whether the policy makes the class a Harberger class.
func (h HarbergerPolicy) Enabled() bool { return h.TaxBpsPerYear > 0 }

// Validate checks a Harberger policy against the compiled bounds.
func (h HarbergerPolicy) Validate() error {
	if !h.Enabled() {
		if h.TaxPeriod != 0 || h.ForeclosureDuration != 0 {
			return errorsmod.Wrap(ErrInvalidHarberger, "tax_period and foreclosure_duration need tax_bps_per_year")
		}
		return nil
	}
	if h.TaxBpsPerYear > MaxHarbergerTaxBps {
		return errorsmod.Wrapf(ErrInvalidHarberger, "tax_bps_per_year must be at most %d", MaxHarbergerTaxBps)
	}
	if h.TaxPeriod < MinHarbergerPeriod || h.TaxPeriod > MaxHarbergerPeriod {
		return errorsmod.Wrapf(ErrInvalidHarberger, "tax_period must be in [%d, %d] seconds", MinHarbergerPeriod, MaxHarbergerPeriod)
	}
	if h.ForeclosureDuration < MinHarbergerPeriod || h.ForeclosureDuration > MaxHarbergerPeriod {
		return errorsmod.Wrapf(ErrInvalidHarberger, "foreclosure_duration must be in [%d, %d] seconds", MinHarbergerPeriod, MaxHarbergerPeriod)
	}
	return nil
}

// TaxPerPeriod is the tax on price for one tax period, rounded up so a
// non-zero price never pays zero tax.
func (h HarbergerPolicy) TaxPerPeriod(price math.Int) math.Int {
	return h.TaxFor(price, h.TaxPeriod)
}

// TaxFor is the tax on price for seconds of holding, rounded up like
// TaxPerPeriod; zero for a non-positive span.
func (h HarbergerPolicy) TaxFor(price math.Int, seconds int64) math.Int {
	if !h.Enabled() || price.IsNil() || !price.IsPositive() || seconds <= 0 {
		return math.ZeroInt()
	}
	num := price.MulRaw(int64(h.TaxBpsPerYear)).MulRaw(seconds)
	den := math.NewInt(10000).MulRaw(SecondsPerYear)
	q := num.Quo(den)
	if !num.Mod(den).IsZero() {
		q = q.AddRaw(1)
	}
	return q
}

// ForeclosureStart returns when a token's price starts falling: the
// recorded foreclosure (a missed payment), or the end of the grace period
// for a priced token without a tax grant; 0 if neither applies.
func (s HarbergerState) ForeclosureStart() int64 {
	if s.ForeclosedAt > 0 {
		return s.ForeclosedAt
	}
	if s.GrantId == 0 && s.GraceUntil > 0 {
		return s.GraceUntil
	}
	return 0
}

// PaidThrough is the time up to which the holder's tax is settled: the
// next due pull of an active grant (each pull pays the period that starts
// at it), else the start of the foreclosure (the end of the grace period
// or a missed pull). A replacement grant whose first pull falls later owes
// the gap up front, and a gift hands this time on rather than a fresh
// grace period (ART-101).
func (s HarbergerState) PaidThrough() int64 {
	if s.GrantId != 0 && s.NextDue != 0 {
		return s.NextDue
	}
	return s.ForeclosureStart()
}

// AssessedPrice is the price the paid-through window was assessed at
// (PaidPrice, zero when unset). Holding the token at a higher price for
// the rest of that window costs the difference up front (ART-201).
func (s HarbergerState) AssessedPrice() math.Int {
	if s.PaidPrice.IsNil() {
		return math.ZeroInt()
	}
	return s.PaidPrice
}

// ReassessmentCharge is what a holder owes up front, to the community
// pool, to hold the token at price from now until nextDue (the first pull
// of the grant being named; 0 without one), given the current state:
//
//   - the time already paid for, now to PaidThrough, was assessed at
//     AssessedPrice; raising the price above it costs the difference for
//     the rest of that window;
//   - the time the new grant skips, PaidThrough (or now, once foreclosed)
//     to nextDue, is charged at the higher of the price held so far and the
//     new one, so swapping through a cheap intermediate price never moves
//     the due date for less than the tax on the price actually held.
//
// paidPrice is the price the whole window now to max(PaidThrough,
// nextDue) is then settled at: the lowest price any part of it was
// assessed at, which a later raise charges from again.
func (s HarbergerState) ReassessmentCharge(h HarbergerPolicy, price math.Int, nextDue, now int64) (charge, paidPrice math.Int) {
	if price.IsNil() {
		price = math.ZeroInt()
	}
	held := s.Price
	if held.IsNil() {
		held = math.ZeroInt()
	}
	charge = math.ZeroInt()
	assessed := s.AssessedPrice()
	from := max(s.PaidThrough(), now)
	paidPrice = math.Int{}
	if from > now {
		// Already paid for, at assessed: top up to the new price.
		if price.GT(assessed) {
			charge = charge.Add(h.TaxFor(price.Sub(assessed), from-now))
			assessed = price
		}
		paidPrice = assessed
	}
	if nextDue > from {
		gapPrice := math.MaxInt(held, price)
		charge = charge.Add(h.TaxFor(gapPrice, nextDue-from))
		if paidPrice.IsNil() || gapPrice.LT(paidPrice) {
			paidPrice = gapPrice
		}
	}
	if paidPrice.IsNil() {
		// Nothing is paid ahead; the next pull sets the window's price.
		paidPrice = price
	}
	return charge, paidPrice
}

// CurrentHarbergerPrice is what a forced buy pays at now: the assessed price, or
// during foreclosure the price falling linearly to zero over the policy's
// foreclosure_duration. foreclosed reports whether the fall has started.
func CurrentHarbergerPrice(h HarbergerPolicy, s HarbergerState, now int64) (price math.Int, foreclosed bool) {
	p := s.Price
	if p.IsNil() {
		p = math.ZeroInt()
	}
	start := s.ForeclosureStart()
	if start == 0 || now < start {
		return p, false
	}
	elapsed := now - start
	if elapsed >= h.ForeclosureDuration {
		return math.ZeroInt(), true
	}
	return p.MulRaw(h.ForeclosureDuration - elapsed).QuoRaw(h.ForeclosureDuration), true
}

// HarbergerForSale reports whether a forced buy is possible at now: a
// priced token, or any token once its foreclosure has started (an
// unpriced token inside its grace period is not for sale).
func HarbergerForSale(s HarbergerState, now int64) bool {
	if !s.Price.IsNil() && s.Price.IsPositive() {
		return true
	}
	start := s.ForeclosureStart()
	return start > 0 && now >= start
}

// NewHarbergerState is the state of a token arriving with a holder at now:
// priced at price, with one tax period of grace to name a tax grant.
func NewHarbergerState(h HarbergerPolicy, price math.Int, now int64) *HarbergerState {
	if price.IsNil() {
		price = math.ZeroInt()
	}
	return &HarbergerState{Price: price, GraceUntil: now + h.TaxPeriod, TaxPaid: math.ZeroInt(), PaidPrice: price}
}

// GiftedHarbergerState is the state of a token arriving by gift or
// accepted transfer at now. Nothing was paid for it, so the obligation
// carries over: the assessed price and any foreclosure stay, and the grace
// period ends when the previous holder's tax was paid through (never later
// than one tax period from now), at the price it was paid at. Passing a
// token back and forth therefore never restarts the grace period; only a
// paid sale does.
func GiftedHarbergerState(h HarbergerPolicy, s HarbergerState, now int64) *HarbergerState {
	price := s.Price
	if price.IsNil() {
		price = math.ZeroInt()
	}
	grace := s.PaidThrough()
	if grace == 0 || grace > now+h.TaxPeriod {
		grace = now + h.TaxPeriod
	}
	return &HarbergerState{Price: price, GraceUntil: grace, ForeclosedAt: s.ForeclosedAt, TaxPaid: math.ZeroInt(), PaidPrice: s.AssessedPrice()}
}

// ---------------------------------------------------------------------------
// Impact claims
// ---------------------------------------------------------------------------

// IsEmpty reports whether no claim field is set.
func (c ImpactClaim) IsEmpty() bool {
	return c.Scope == "" && len(c.Contributors) == 0 && c.WorkStart == 0 && c.WorkEnd == 0 && c.InitiativeId == 0
}

// ValidateImpactClaim checks a claim's shape; contributor addresses are
// decoded by the keeper.
func ValidateImpactClaim(c ImpactClaim, p Params) error {
	if c.IsEmpty() {
		return nil
	}
	if c.Scope == "" {
		return errorsmod.Wrap(ErrInvalidClaim, "scope is required")
	}
	if err := ValidateText("scope", c.Scope, p.MaxDescriptionLength, true); err != nil {
		return errorsmod.Wrap(ErrInvalidClaim, err.Error())
	}
	if len(c.Contributors) > MaxImpactContributors {
		return errorsmod.Wrapf(ErrInvalidClaim, "at most %d contributors", MaxImpactContributors)
	}
	seen := map[string]bool{}
	for _, a := range c.Contributors {
		if a == "" || seen[a] {
			return errorsmod.Wrapf(ErrInvalidClaim, "empty or duplicate contributor %q", a)
		}
		seen[a] = true
	}
	if c.WorkStart < 0 || c.WorkEnd < 0 || (c.WorkStart > 0 && c.WorkEnd > 0 && c.WorkEnd < c.WorkStart) {
		return errorsmod.Wrap(ErrInvalidClaim, "work period must be non-negative with work_start <= work_end")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Retro-funding accumulator
// ---------------------------------------------------------------------------

// Units is a balance's units that accrue rewards: free plus locked.
func (b Balance) Units() uint64 { return b.Amount + b.Locked }

// Accrued is what a balance earned since it was last settled.
func (b Balance) Accrued(rewardPerUnit math.Int) math.Int {
	debt := b.RewardDebt
	if debt.IsNil() {
		debt = math.ZeroInt()
	}
	if rewardPerUnit.IsNil() || !rewardPerUnit.GT(debt) || b.Units() == 0 {
		return math.ZeroInt()
	}
	return rewardPerUnit.Sub(debt).Mul(math.NewIntFromUint64(b.Units())).Quo(RewardScale)
}

// Claimable is the balance's unclaimed rewards plus what it accrued since
// its last settlement.
func (b Balance) Claimable(rewardPerUnit math.Int) math.Int {
	u := b.Unclaimed
	if u.IsNil() {
		u = math.ZeroInt()
	}
	return u.Add(b.Accrued(rewardPerUnit))
}

// AddUnits adds n units to a, failing instead of wrapping and when the
// result exceeds HardMaxEditionSupply. Every unit counter and balance is
// added to through it.
func AddUnits(a, n uint64) (uint64, error) {
	if n > HardMaxEditionSupply || a > HardMaxEditionSupply-n {
		return 0, errorsmod.Wrapf(ErrSupplyExceeded, "%d + %d units exceeds the hard ceiling %d", a, n, HardMaxEditionSupply)
	}
	return a + n, nil
}

// RewardPerUnitDelta is the accumulator increase for amount paid over
// units circulating units (floor; the remainder stays in the module).
func RewardPerUnitDelta(amount math.Int, units uint64) math.Int {
	if units == 0 {
		return math.ZeroInt()
	}
	return amount.Mul(RewardScale).Quo(math.NewIntFromUint64(units))
}

// ---------------------------------------------------------------------------
// Nesting
// ---------------------------------------------------------------------------

// IsNested reports whether the token sits inside another token.
func (t Token) IsNested() bool { return t.Parent != nil && t.Parent.ClassId != 0 }

// ContentsEntry is one token of a nested tree, as ContentsHash commits it.
type ContentsEntry struct {
	ClassID, TokenID, ParentClassID, ParentTokenID uint64
	MetadataHash                                   string
}

// ContentsHash commits to the tree nested under a token: every descendant,
// its parent and its stored metadata hash, sorted by (class, token). It is
// "" for a token that holds nothing, so an empty pin matches an empty tree.
func ContentsHash(entries []ContentsEntry) string {
	if len(entries) == 0 {
		return ""
	}
	sorted := append([]ContentsEntry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].ClassID != sorted[j].ClassID {
			return sorted[i].ClassID < sorted[j].ClassID
		}
		return sorted[i].TokenID < sorted[j].TokenID
	})
	h := sha256.New()
	for _, e := range sorted {
		fmt.Fprintf(h, "%d/%d<%d/%d:%s\n", e.ClassID, e.TokenID, e.ParentClassID, e.ParentTokenID, e.MetadataHash)
	}
	return hex.EncodeToString(h.Sum(nil))
}
