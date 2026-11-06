package types

import (
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DefaultGenesis returns the default genesis state.
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params: DefaultParams(),
		Grants: []Grant{},
	}
}

// Validate performs basic genesis state validation returning an error upon any failure.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}

	// SESSION-7 fix: validate each imported grant.
	seenIDs := make(map[uint64]bool)
	for i, g := range gs.Grants {
		if g.Id == 0 {
			return fmt.Errorf("grant %d: id must be non-zero", i)
		}
		if seenIDs[g.Id] {
			return fmt.Errorf("grant %d: duplicate id %d", i, g.Id)
		}
		seenIDs[g.Id] = true

		// Ids are allocated as sequence+1 after the sequence advanced, so
		// the exported sequence value equals the highest allocated id.
		if gs.GrantSeq != 0 && g.Id > gs.GrantSeq {
			return fmt.Errorf("grant %d: id %d exceeds GrantSeq %d", i, g.Id, gs.GrantSeq)
		}
		if _, err := sdk.AccAddressFromBech32(g.Granter); err != nil {
			return fmt.Errorf("grant %d: invalid granter address %q: %w", i, g.Granter, err)
		}
		if _, err := sdk.AccAddressFromBech32(g.Grantee); err != nil {
			return fmt.Errorf("grant %d: invalid grantee address %q: %w", i, g.Grantee, err)
		}
		if g.Granter == g.Grantee {
			return fmt.Errorf("grant %d: granter cannot equal grantee", i)
		}
		if g.Type == GrantType_GRANT_TYPE_UNSPECIFIED {
			return fmt.Errorf("grant %d: type must be specified", i)
		}
		if g.Status == GrantStatus_GRANT_STATUS_UNSPECIFIED {
			return fmt.Errorf("grant %d: status must be specified", i)
		}
		if g.ExpiresAt.IsZero() {
			return fmt.Errorf("grant %d: expires_at must be set", i)
		}

		switch g.Type {
		case GrantType_GRANT_TYPE_SESSION_KEY:
			sk := g.GetSessionKey()
			if sk == nil {
				return fmt.Errorf("grant %d: SESSION_KEY type requires session_key payload", i)
			}
			if !sk.SpendLimit.IsValid() || !sk.SpendLimit.IsPositive() {
				return fmt.Errorf("grant %d: spend_limit must be a valid positive coin", i)
			}
			// Per-chain bond-denom check happens at runtime (keeper.denomAllowed)
			// — Validate() has no ctx and can't read x/identity here.
			if !sk.Spent.IsValid() {
				return fmt.Errorf("grant %d: spent coin is invalid", i)
			}
			if sk.Spent.Denom != sk.SpendLimit.Denom {
				return fmt.Errorf("grant %d: spent denom %q does not match spend_limit denom %q", i, sk.Spent.Denom, sk.SpendLimit.Denom)
			}
			if sk.Spent.Amount.GT(sk.SpendLimit.Amount) {
				return fmt.Errorf("grant %d: spent (%s) exceeds spend_limit (%s)", i, sk.Spent.Amount, sk.SpendLimit.Amount)
			}
		case GrantType_GRANT_TYPE_RECURRING_PULL:
			rp := g.GetRecurringPull()
			if rp == nil {
				return fmt.Errorf("grant %d: RECURRING_PULL type requires recurring_pull payload", i)
			}
			if !rp.AmountPerPeriod.IsValid() || !rp.AmountPerPeriod.IsPositive() {
				return fmt.Errorf("grant %d: amount_per_period must be a valid positive coin", i)
			}
			// DREAM-denom rejection happens at runtime via k.DreamDenom(ctx);
			// Validate() has no ctx and can't read x/identity here.
			if rp.PeriodSeconds <= 0 {
				return fmt.Errorf("grant %d: period_seconds must be > 0", i)
			}
		case GrantType_GRANT_TYPE_SPENDING_ALLOWANCE:
			sa := g.GetSpendingAllowance()
			if sa == nil {
				return fmt.Errorf("grant %d: SPENDING_ALLOWANCE type requires spending_allowance payload", i)
			}
			if !sa.MaxPerPeriod.IsValid() || !sa.MaxPerPeriod.IsPositive() {
				return fmt.Errorf("grant %d: max_per_period must be a valid positive coin", i)
			}
			// DREAM-denom rejection happens at runtime via k.DreamDenom(ctx);
			// Validate() has no ctx and can't read x/identity here.
			if sa.PeriodSeconds <= 0 {
				return fmt.Errorf("grant %d: period_seconds must be > 0", i)
			}
			if sa.Denom == "" || sa.Denom != sa.MaxPerPeriod.Denom {
				return fmt.Errorf("grant %d: denom field must match max_per_period.Denom", i)
			}
		case GrantType_GRANT_TYPE_SCHEDULED_ONESHOT:
			so := g.GetScheduledOneshot()
			if so == nil {
				return fmt.Errorf("grant %d: SCHEDULED_ONESHOT type requires scheduled_oneshot payload", i)
			}
			if so.Action == nil {
				return fmt.Errorf("grant %d: scheduled_oneshot.action must be set", i)
			}
			if so.FireAt <= 0 {
				return fmt.Errorf("grant %d: fire_at must be > 0", i)
			}
		case GrantType_GRANT_TYPE_SIGN_IN:
			si := g.GetSignIn()
			if si == nil {
				return fmt.Errorf("grant %d: SIGN_IN type requires sign_in payload", i)
			}
			if !ValidSignInService(si.Service) {
				return fmt.Errorf("grant %d: sign_in.service %q is not a lowercase DNS hostname", i, si.Service)
			}
		default:
			return fmt.Errorf("grant %d: unknown type %s", i, g.Type)
		}
	}

	grantType := make(map[uint64]GrantType, len(gs.Grants))
	grantStatus := make(map[uint64]GrantStatus, len(gs.Grants))
	pendingPerGranter := make(map[string]int)
	for _, g := range gs.Grants {
		grantType[g.Id] = g.Type
		grantStatus[g.Id] = g.Status
		if g.Status == GrantStatus_GRANT_STATUS_PENDING {
			// Only access-class grants wait for consent (SES-102), and a
			// granter holds at most MaxPendingGrantsPerGranter of them.
			if !IsAccessGrantType(g.Type) {
				return fmt.Errorf("grant %d: %s grants cannot be PENDING", g.Id, g.Type)
			}
			pendingPerGranter[g.Granter]++
			if pendingPerGranter[g.Granter] > MaxPendingGrantsPerGranter {
				return fmt.Errorf("granter %s has more than %d pending grants", g.Granter, MaxPendingGrantsPerGranter)
			}
		}
	}
	seenPendingDeposit := make(map[uint64]bool)
	for i, d := range gs.PendingGrantDeposits {
		if grantStatus[d.GrantId] != GrantStatus_GRANT_STATUS_PENDING {
			return fmt.Errorf("pending_grant_deposits[%d]: grant %d is not a pending grant in genesis", i, d.GrantId)
		}
		if seenPendingDeposit[d.GrantId] {
			return fmt.Errorf("pending_grant_deposits[%d]: duplicate grant %d", i, d.GrantId)
		}
		seenPendingDeposit[d.GrantId] = true
		if !d.Amount.IsValid() || !d.Amount.IsPositive() {
			return fmt.Errorf("pending_grant_deposits[%d]: invalid amount %s", i, d.Amount)
		}
	}
	type pairKey struct{ grantee, granter string }
	seenAllowed := make(map[pairKey]bool, len(gs.AllowedGranters))
	for i, a := range gs.AllowedGranters {
		if _, err := sdk.AccAddressFromBech32(a.Grantee); err != nil {
			return fmt.Errorf("allowed_granters[%d]: invalid grantee address %q: %w", i, a.Grantee, err)
		}
		if _, err := sdk.AccAddressFromBech32(a.Granter); err != nil {
			return fmt.Errorf("allowed_granters[%d]: invalid granter address %q: %w", i, a.Granter, err)
		}
		if a.Grantee == a.Granter {
			return fmt.Errorf("allowed_granters[%d]: grantee cannot equal granter", i)
		}
		k := pairKey{a.Grantee, a.Granter}
		if seenAllowed[k] {
			return fmt.Errorf("allowed_granters[%d]: duplicate entry (%s, %s)", i, a.Grantee, a.Granter)
		}
		seenAllowed[k] = true
	}
	seenDeposit := make(map[uint64]bool)
	for i, d := range gs.OneshotGasDeposits {
		if grantType[d.GrantId] != GrantType_GRANT_TYPE_SCHEDULED_ONESHOT {
			return fmt.Errorf("oneshot_gas_deposits[%d]: grant %d is not a scheduled oneshot in genesis", i, d.GrantId)
		}
		if seenDeposit[d.GrantId] {
			return fmt.Errorf("oneshot_gas_deposits[%d]: duplicate grant %d", i, d.GrantId)
		}
		seenDeposit[d.GrantId] = true
		if !d.Amount.IsValid() {
			return fmt.Errorf("oneshot_gas_deposits[%d]: invalid amount %s", i, d.Amount)
		}
	}
	type spendKey struct {
		grant uint64
		day   int64
	}
	seenSpend := make(map[spendKey]bool)
	for i, e := range gs.EpochSpends {
		if grantType[e.GrantId] != GrantType_GRANT_TYPE_RECURRING_PULL {
			return fmt.Errorf("epoch_spends[%d]: grant %d is not a recurring pull in genesis", i, e.GrantId)
		}
		k := spendKey{e.GrantId, e.Day}
		if seenSpend[k] {
			return fmt.Errorf("epoch_spends[%d]: duplicate (grant %d, day %d)", i, e.GrantId, e.Day)
		}
		seenSpend[k] = true
		if v, ok := math.NewIntFromString(e.Amount); !ok || v.IsNegative() {
			return fmt.Errorf("epoch_spends[%d]: invalid amount %q", i, e.Amount)
		}
	}
	seenCreator := make(map[uint64]bool)
	for i, c := range gs.GrantCreators {
		if _, ok := grantType[c.GrantId]; !ok {
			return fmt.Errorf("grant_creators[%d]: grant %d not in genesis", i, c.GrantId)
		}
		if seenCreator[c.GrantId] {
			return fmt.Errorf("grant_creators[%d]: duplicate grant %d", i, c.GrantId)
		}
		seenCreator[c.GrantId] = true
		if _, err := sdk.AccAddressFromBech32(c.ModuleAddress); err != nil {
			return fmt.Errorf("grant_creators[%d]: invalid module address %q: %w", i, c.ModuleAddress, err)
		}
	}

	// active_grant_counts must match the grants list exactly: one entry per
	// (granter, type), and every count equal to the number of active or
	// paused grants (InitGenesis recomputes them; a mismatch means the file
	// was edited or exported inconsistently).
	type countKey struct {
		granter string
		typ     GrantType
	}
	actual := make(map[countKey]uint32)
	for _, g := range gs.Grants {
		if g.Status == GrantStatus_GRANT_STATUS_ACTIVE || g.Status == GrantStatus_GRANT_STATUS_PAUSED_INSUFFICIENT_FUNDS {
			actual[countKey{g.Granter, g.Type}]++
		}
	}
	supplied := make(map[countKey]bool, len(gs.ActiveGrantCounts))
	for i, c := range gs.ActiveGrantCounts {
		if _, err := sdk.AccAddressFromBech32(c.Granter); err != nil {
			return fmt.Errorf("active_grant_counts[%d]: invalid granter address %q: %w", i, c.Granter, err)
		}
		if c.Type == GrantType_GRANT_TYPE_UNSPECIFIED {
			return fmt.Errorf("active_grant_counts[%d]: type must be specified", i)
		}
		key := countKey{c.Granter, c.Type}
		if supplied[key] {
			return fmt.Errorf("active_grant_counts[%d]: duplicate entry for %s / %s", i, c.Granter, c.Type)
		}
		supplied[key] = true
		if c.Count != actual[key] {
			return fmt.Errorf("active_grant_counts[%d]: %s / %s count %d, but the grants list has %d active",
				i, c.Granter, c.Type, c.Count, actual[key])
		}
	}
	// An exported file lists every non-zero count; one missing is understated.
	// (An empty list is accepted: InitGenesis recomputes.)
	if len(gs.ActiveGrantCounts) > 0 {
		for key, n := range actual {
			if !supplied[key] {
				return fmt.Errorf("active_grant_counts: missing %s / %s (the grants list has %d active)", key.granter, key.typ, n)
			}
		}
	}

	return nil
}
