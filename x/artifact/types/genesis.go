package types

import (
	"fmt"
)

// DefaultGenesis returns the default genesis state
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params: DefaultParams(),
	}
}

type tokenKey struct{ class, token uint64 }

// Validate performs basic genesis state validation returning an error upon any
// failure. It checks the structural invariants of docs/x-artifact-spec.md §13
// except escrow, which depends on bank genesis.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}

	classes := map[uint64]Class{}
	for _, c := range gs.Classes {
		if c.Id == 0 || c.Id > gs.ClassSeq {
			return fmt.Errorf("class id %d outside (0, class_seq=%d]", c.Id, gs.ClassSeq)
		}
		if _, dup := classes[c.Id]; dup {
			return fmt.Errorf("duplicate class %d", c.Id)
		}
		if c.Owner == "" || c.Creator == "" {
			return fmt.Errorf("class %d missing owner or creator", c.Id)
		}
		if err := c.Flags.Validate(); err != nil {
			return fmt.Errorf("class %d: %w", c.Id, err)
		}
		if err := ValidateRoyaltyShares(c.RoyaltyRecipients); err != nil {
			return fmt.Errorf("class %d: %w", c.Id, err)
		}
		if c.NextTokenId == 0 {
			return fmt.Errorf("class %d next_token_id must be >= 1", c.Id)
		}
		if err := c.Expiry.Validate(); err != nil {
			return fmt.Errorf("class %d: %w", c.Id, err)
		}
		if err := ValidatePricing(c.MintPolicy); err != nil {
			return fmt.Errorf("class %d: %w", c.Id, err)
		}
		if _, ok := RevealKind_name[int32(c.Reveal.Kind)]; !ok {
			return fmt.Errorf("class %d: unknown reveal kind %d", c.Id, c.Reveal.Kind)
		}
		if c.Reveal.Reveals() && !hexHashPattern.MatchString(c.Reveal.Commitment) {
			return fmt.Errorf("class %d: reveal commitment is not a SHA-256", c.Id)
		}
		if c.Renderer.SourceHash != "" && c.Renderer.PkgPath == "" {
			return fmt.Errorf("class %d: renderer source_hash without pkg_path", c.Id)
		}
		if _, ok := TokenModel_name[int32(c.TokenModel)]; !ok {
			return fmt.Errorf("class %d: unknown token model %d", c.Id, c.TokenModel)
		}
		if err := c.Harberger.Validate(); err != nil {
			return fmt.Errorf("class %d: %w", c.Id, err)
		}
		classes[c.Id] = c
	}

	live := map[uint64]uint64{}
	tokens := map[tokenKey]Token{}
	for _, t := range gs.Tokens {
		c, ok := classes[t.ClassId]
		if !ok {
			return fmt.Errorf("token %d/%d references unknown class", t.ClassId, t.Id)
		}
		k := tokenKey{t.ClassId, t.Id}
		if _, dup := tokens[k]; dup {
			return fmt.Errorf("duplicate token %d/%d", t.ClassId, t.Id)
		}
		if t.Id == 0 || t.Id >= c.NextTokenId {
			return fmt.Errorf("token %d/%d outside [1, next_token_id=%d)", t.ClassId, t.Id, c.NextTokenId)
		}
		if t.Owner == "" {
			return fmt.Errorf("token %d/%d has no owner", t.ClassId, t.Id)
		}
		if t.Deposit.IsNil() || t.Deposit.IsNegative() {
			return fmt.Errorf("token %d/%d has an invalid deposit", t.ClassId, t.Id)
		}
		if !t.Paid.IsNil() && t.Paid.IsNegative() {
			return fmt.Errorf("token %d/%d has a negative escrowed payment", t.ClassId, t.Id)
		}
		if !t.Paid.IsNil() && t.Paid.IsPositive() && !c.Reveal.Reveals() {
			return fmt.Errorf("token %d/%d escrows a payment on a plain class", t.ClassId, t.Id)
		}
		if t.Unrevealed && c.Reveal.Kind != RevealKind_REVEAL_KIND_BLIND_METADATA {
			return fmt.Errorf("token %d/%d is unrevealed on a non-blind class", t.ClassId, t.Id)
		}
		if c.Expiry.Expires() && t.ExpiresAt == 0 {
			return fmt.Errorf("token %d/%d of an expiring class has no expires_at", t.ClassId, t.Id)
		}
		if int(t.DisplayedVersion) > len(t.MediaVersions) {
			return fmt.Errorf("token %d/%d displayed_version out of range", t.ClassId, t.Id)
		}
		if t.RedeemedAt != 0 && !c.Flags.Redeemable {
			return fmt.Errorf("token %d/%d redeemed on a non-redeemable class", t.ClassId, t.Id)
		}
		tokens[k] = t
		live[t.ClassId]++
	}

	reserved := map[uint64]uint64{}
	for _, pm := range gs.PendingMints {
		c, ok := classes[pm.ClassId]
		if !ok {
			return fmt.Errorf("pending mint %d/%d references unknown class", pm.ClassId, pm.TokenId)
		}
		k := tokenKey{pm.ClassId, pm.TokenId}
		if _, clash := tokens[k]; clash {
			return fmt.Errorf("pending mint %d/%d collides with a token", pm.ClassId, pm.TokenId)
		}
		if pm.TokenId == 0 || pm.TokenId >= c.NextTokenId {
			return fmt.Errorf("pending mint %d/%d outside id range", pm.ClassId, pm.TokenId)
		}
		reserved[pm.ClassId]++
	}

	for _, e := range gs.Editions {
		if c, ok := classes[e.ClassId]; ok && c.IsEditions() {
			live[e.ClassId]++
		}
	}
	for id, c := range classes {
		if live[id] != c.Supply {
			return fmt.Errorf("class %d supply %d != %d tokens", id, c.Supply, live[id])
		}
		if reserved[id] != c.ReservedSupply {
			return fmt.Errorf("class %d reserved_supply %d != %d pending mints", id, c.ReservedSupply, reserved[id])
		}
		if c.MaxSupply > 0 && c.Supply+c.Burned+c.ReservedSupply > c.MaxSupply {
			return fmt.Errorf("class %d exceeds max_supply", id)
		}
	}

	listed := map[tokenKey]bool{}
	for _, l := range gs.Listings {
		k := tokenKey{l.ClassId, l.TokenId}
		t, ok := tokens[k]
		if !ok {
			return fmt.Errorf("listing %d/%d without token", l.ClassId, l.TokenId)
		}
		if t.Lock != TokenLock_TOKEN_LOCK_LISTED || t.Owner != l.Seller || t.ListingSeq != l.Nonce {
			return fmt.Errorf("listing %d/%d inconsistent with token", l.ClassId, l.TokenId)
		}
		if !classes[l.ClassId].Flags.Transferable {
			return fmt.Errorf("listing %d/%d on a soulbound class", l.ClassId, l.TokenId)
		}
		listed[k] = true
	}
	pending := map[tokenKey]bool{}
	for _, pt := range gs.PendingTransfers {
		k := tokenKey{pt.ClassId, pt.TokenId}
		t, ok := tokens[k]
		if !ok {
			return fmt.Errorf("pending transfer %d/%d without token", pt.ClassId, pt.TokenId)
		}
		if t.Lock != TokenLock_TOKEN_LOCK_PENDING_TRANSFER || t.Owner != pt.From {
			return fmt.Errorf("pending transfer %d/%d inconsistent with token", pt.ClassId, pt.TokenId)
		}
		pending[k] = true
	}
	for k, t := range tokens {
		if t.Lock == TokenLock_TOKEN_LOCK_LISTED && !listed[k] {
			return fmt.Errorf("token %d/%d LISTED without listing", k.class, k.token)
		}
		if t.Lock == TokenLock_TOKEN_LOCK_PENDING_TRANSFER && !pending[k] {
			return fmt.Errorf("token %d/%d PENDING without record", k.class, k.token)
		}
	}

	for _, po := range gs.PendingClassOwners {
		if _, ok := classes[po.ClassId]; !ok {
			return fmt.Errorf("pending owner for unknown class %d", po.ClassId)
		}
	}

	openTargets := map[tokenKey]bool{}
	hideIDs := map[uint64]bool{}
	for _, hr := range gs.HideRecords {
		if hr.Id == 0 || hr.Id > gs.HideSeq {
			return fmt.Errorf("hide id %d outside (0, hide_seq=%d]", hr.Id, gs.HideSeq)
		}
		if hideIDs[hr.Id] {
			return fmt.Errorf("duplicate hide record %d", hr.Id)
		}
		hideIDs[hr.Id] = true
		if hr.Outcome == HideOutcome_HIDE_OUTCOME_PENDING {
			k := tokenKey{hr.ClassId, hr.TokenId}
			if openTargets[k] {
				return fmt.Errorf("two open hide records for %d/%d", hr.ClassId, hr.TokenId)
			}
			openTargets[k] = true
		}
	}

	seenProposal := map[[3]uint64]bool{}
	for _, mp := range gs.MediaProposals {
		if _, ok := tokens[tokenKey{mp.ClassId, mp.TokenId}]; !ok {
			return fmt.Errorf("media proposal %d/%d #%d without token", mp.ClassId, mp.TokenId, mp.Id)
		}
		pk := [3]uint64{mp.ClassId, mp.TokenId, mp.Id}
		if mp.Id == 0 || seenProposal[pk] {
			return fmt.Errorf("invalid or duplicate media proposal %d/%d #%d", mp.ClassId, mp.TokenId, mp.Id)
		}
		seenProposal[pk] = true
	}
	for _, q := range gs.ClassReleaseQueue {
		if _, ok := classes[q.ClassId]; !ok {
			return fmt.Errorf("release queue for unknown class %d", q.ClassId)
		}
	}

	seenPolicy := map[string]bool{}
	for _, e := range gs.ReceivePolicies {
		if seenPolicy[e.Address] {
			return fmt.Errorf("duplicate receive policy for %s", e.Address)
		}
		seenPolicy[e.Address] = true
		if e.Policy == ReceivePolicy_RECEIVE_POLICY_UNSPECIFIED {
			return fmt.Errorf("receive policy for %s is UNSPECIFIED", e.Address)
		}
	}
	return gs.validateWave34(classes, tokens)
}

// validateWave34 checks editions, balances, pending units, unit listings,
// recipes, rentals, nesting and Harberger state (docs/x-artifact-spec.md
// §13 items 13-16), except the escrow they add (bank genesis).
func (gs GenesisState) validateWave34(classes map[uint64]Class, tokens map[tokenKey]Token) error {
	type editionTally struct{ units, holders, reserved uint64 }
	editions := map[tokenKey]*editionTally{}
	for _, e := range gs.Editions {
		c, ok := classes[e.ClassId]
		if !ok || !c.IsEditions() {
			return fmt.Errorf("edition %d/%d outside an editions class", e.ClassId, e.Id)
		}
		k := tokenKey{e.ClassId, e.Id}
		if _, dup := editions[k]; dup {
			return fmt.Errorf("duplicate edition %d/%d", e.ClassId, e.Id)
		}
		if e.Id == 0 || e.Id >= c.NextTokenId {
			return fmt.Errorf("edition %d/%d outside [1, next_token_id=%d)", e.ClassId, e.Id, c.NextTokenId)
		}
		if e.RewardPerUnit.IsNil() || e.RewardPerUnit.IsNegative() {
			return fmt.Errorf("edition %d/%d has an invalid reward accumulator", e.ClassId, e.Id)
		}
		editions[k] = &editionTally{}
	}
	type balKey struct {
		class, edition uint64
		owner          string
	}
	locked := map[balKey]uint64{}
	for _, pu := range gs.PendingUnits {
		t, ok := editions[tokenKey{pu.ClassId, pu.EditionId}]
		if !ok || pu.Id == 0 || pu.Id > gs.PendingUnitSeq || pu.Amount == 0 {
			return fmt.Errorf("invalid pending units %d", pu.Id)
		}
		switch pu.Kind {
		case InboxKind_INBOX_KIND_UNIT_MINT:
			t.reserved += pu.Amount
		case InboxKind_INBOX_KIND_UNIT_TRANSFER:
			locked[balKey{pu.ClassId, pu.EditionId, pu.From}] += pu.Amount
		default:
			return fmt.Errorf("pending units %d has kind %s", pu.Id, pu.Kind)
		}
	}
	for _, l := range gs.UnitListings {
		if _, ok := editions[tokenKey{l.ClassId, l.EditionId}]; !ok || l.Id == 0 || l.Id > gs.UnitListingSeq || l.Quantity == 0 {
			return fmt.Errorf("invalid unit listing %d", l.Id)
		}
		locked[balKey{l.ClassId, l.EditionId, l.Seller}] += l.Quantity
	}
	seenBal := map[balKey]bool{}
	for _, b := range gs.Balances {
		t, ok := editions[tokenKey{b.ClassId, b.EditionId}]
		k := balKey{b.ClassId, b.EditionId, b.Owner}
		if !ok || b.Owner == "" || seenBal[k] {
			return fmt.Errorf("invalid or duplicate balance %d/%d %s", b.ClassId, b.EditionId, b.Owner)
		}
		seenBal[k] = true
		if b.Units() == 0 || b.Locked != locked[k] {
			return fmt.Errorf("balance %d/%d %s: %d units, %d locked (want %d)", b.ClassId, b.EditionId, b.Owner, b.Units(), b.Locked, locked[k])
		}
		delete(locked, k)
		t.units += b.Units()
		t.holders++
	}
	for k, v := range locked {
		if v > 0 {
			return fmt.Errorf("%d units locked for %d/%d %s without a balance", v, k.class, k.edition, k.owner)
		}
	}
	for _, e := range gs.Editions {
		t := editions[tokenKey{e.ClassId, e.Id}]
		if t.units != e.Supply || t.holders != e.Holders || t.reserved != e.Reserved {
			return fmt.Errorf("edition %d/%d counters do not match its balances", e.ClassId, e.Id)
		}
		if e.MaxSupply > 0 && e.Supply+e.Burned+e.Reserved > e.MaxSupply {
			return fmt.Errorf("edition %d/%d exceeds max_supply", e.ClassId, e.Id)
		}
	}
	for _, r := range gs.CraftRecipes {
		if _, ok := classes[r.ClassId]; !ok || r.Id == 0 || r.Id > classes[r.ClassId].NextRecipeId {
			return fmt.Errorf("invalid craft recipe %d/%d", r.ClassId, r.Id)
		}
	}
	rented := map[tokenKey]bool{}
	for _, r := range gs.Rentals {
		t, ok := tokens[tokenKey{r.ClassId, r.TokenId}]
		if !ok || t.Lock != TokenLock_TOKEN_LOCK_RENTED || t.Owner != r.Owner || r.User == "" {
			return fmt.Errorf("rental %d/%d inconsistent with its token", r.ClassId, r.TokenId)
		}
		rented[tokenKey{r.ClassId, r.TokenId}] = true
	}
	for _, o := range gs.RentalOffers {
		if _, ok := tokens[tokenKey{o.ClassId, o.TokenId}]; !ok {
			return fmt.Errorf("rental offer %d/%d without token", o.ClassId, o.TokenId)
		}
	}
	seenOffer := map[uint64]bool{}
	for _, o := range gs.Offers {
		if o.Id == 0 || o.Id > gs.OfferSeq || seenOffer[o.Id] || o.Bidder == "" || o.Price.Amount.IsNil() || !o.Price.Amount.IsPositive() {
			return fmt.Errorf("invalid or duplicate offer %d", o.Id)
		}
		if _, ok := classes[o.ClassId]; !ok {
			return fmt.Errorf("offer %d references unknown class %d", o.Id, o.ClassId)
		}
		seenOffer[o.Id] = true
	}
	auctioned := map[tokenKey]bool{}
	for _, a := range gs.Auctions {
		k := tokenKey{a.ClassId, a.TokenId}
		t, ok := tokens[k]
		if a.Id == 0 || a.Id > gs.AuctionSeq || !ok || auctioned[k] || t.Lock != TokenLock_TOKEN_LOCK_AUCTION || t.Owner != a.Seller {
			return fmt.Errorf("auction %d inconsistent with its token", a.Id)
		}
		if a.HighBid.IsNil() || (a.HighBidder != "" && a.HighBid.LT(a.ReservePrice.Amount)) {
			return fmt.Errorf("auction %d has an invalid high bid", a.Id)
		}
		auctioned[k] = true
	}
	descendants := map[tokenKey]uint32{}
	for k, t := range tokens {
		if t.Lock == TokenLock_TOKEN_LOCK_AUCTION && !auctioned[k] {
			return fmt.Errorf("token %d/%d AUCTION-locked without an auction", k.class, k.token)
		}
		if t.Lock == TokenLock_TOKEN_LOCK_RENTED && !rented[k] {
			return fmt.Errorf("token %d/%d RENTED without a rental", k.class, k.token)
		}
		if t.IsNested() != (t.Lock == TokenLock_TOKEN_LOCK_NESTED) {
			return fmt.Errorf("token %d/%d nested state and lock disagree", k.class, k.token)
		}
		// Walk up: every ancestor counts this token, shares its owner and
		// the chain stays within the hard depth cap.
		cur := t
		for depth := uint32(0); cur.IsNested(); depth++ {
			if depth >= HardMaxNestDepth {
				return fmt.Errorf("token %d/%d sits deeper than %d levels (or in a cycle)", k.class, k.token, HardMaxNestDepth)
			}
			pk := tokenKey{cur.Parent.ClassId, cur.Parent.TokenId}
			parent, ok := tokens[pk]
			if !ok || parent.Owner != t.Owner {
				return fmt.Errorf("token %d/%d has a missing or differently owned ancestor", k.class, k.token)
			}
			descendants[pk]++
			cur = parent
		}
		if classes[t.ClassId].IsHarberger() != (t.Harberger != nil) {
			return fmt.Errorf("token %d/%d harberger state does not match its class", k.class, k.token)
		}
	}
	for k, t := range tokens {
		if descendants[k] != t.Descendants {
			return fmt.Errorf("token %d/%d descendants %d != %d", k.class, k.token, t.Descendants, descendants[k])
		}
	}
	return nil
}
