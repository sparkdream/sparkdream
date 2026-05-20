package types

import (
	"fmt"

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

		if gs.GrantSeq != 0 && g.Id >= gs.GrantSeq {
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
			if sk.SpendLimit.Denom != "uspark" {
				return fmt.Errorf("grant %d: spend_limit denom must be uspark, got %q", i, sk.SpendLimit.Denom)
			}
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
			if rp.AmountPerPeriod.Denom == "dream" {
				return fmt.Errorf("grant %d: amount_per_period denom must not be dream", i)
			}
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
			if sa.MaxPerPeriod.Denom == "dream" {
				return fmt.Errorf("grant %d: max_per_period denom must not be dream", i)
			}
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
		default:
			return fmt.Errorf("grant %d: unknown type %s", i, g.Type)
		}
	}

	for i, c := range gs.ActiveGrantCounts {
		if _, err := sdk.AccAddressFromBech32(c.Granter); err != nil {
			return fmt.Errorf("active_grant_counts[%d]: invalid granter address %q: %w", i, c.Granter, err)
		}
		if c.Type == GrantType_GRANT_TYPE_UNSPECIFIED {
			return fmt.Errorf("active_grant_counts[%d]: type must be specified", i)
		}
	}

	return nil
}
