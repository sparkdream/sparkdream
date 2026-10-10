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
	return nil
}
