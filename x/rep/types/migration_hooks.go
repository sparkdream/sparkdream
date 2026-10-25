package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// MemberMigrationHooks is implemented by modules that hold member identity
// state keyed by address and must either veto a migration or re-key their
// bounded core state when one finalizes (member-key-migration plan §8).
//
// Both hooks run inside the migration's own transaction or inside the
// EndBlocker finalization cache context: a returned error from
// BeforeMemberMigration refuses the request / accept / finalization, and a
// returned error from AfterMemberMigration aborts the finalization (the
// migration is marked FAILED, nothing moves, the chain does not halt).
//
// Hooks are wired in app.go through Keeper.SetMigrationHooks, never through
// depinject optional inputs (cycle rule, docs/development-conventions.md).
type MemberMigrationHooks interface {
	// BeforeMemberMigration is a veto. It is called at request, accept and
	// finalization, with the old address (from), the new one (to) and the
	// path. A module refuses when `to` is not fresh in its own state (an
	// x/name owner record, an x/season profile, x/session grants as granter,
	// an x/service operator record, x/federation identity links) or when
	// `from` carries state that cannot travel (an x/service operator bond, an
	// x/name dispute as owner, a pending x/artifact class-owner proposal).
	BeforeMemberMigration(ctx context.Context, from, to sdk.AccAddress, path MigrationPath) error
	// AfterMemberMigration re-keys the module's bounded, authority-bearing
	// state from `from` to `to`. Everything unbounded (authorship, votes,
	// stakes, history) stays stored under `from` and is read through the
	// alias (Canonical / CanActAs / RecentAddressesOf / AllAddressesOf).
	AfterMemberMigration(ctx context.Context, from, to sdk.AccAddress, path MigrationPath) error
}

// MultiMemberMigrationHooks dispatches to every child hook in order. Unlike
// RepHooks, errors are tx-halting: the first error stops the dispatch and is
// returned, because a half-applied re-key is worse than a refused migration.
type MultiMemberMigrationHooks []MemberMigrationHooks

// NewMultiMemberMigrationHooks wraps hooks into one MemberMigrationHooks.
func NewMultiMemberMigrationHooks(hooks ...MemberMigrationHooks) MultiMemberMigrationHooks {
	return MultiMemberMigrationHooks(hooks)
}

func (m MultiMemberMigrationHooks) BeforeMemberMigration(ctx context.Context, from, to sdk.AccAddress, path MigrationPath) error {
	for _, h := range m {
		if h == nil {
			continue
		}
		if err := h.BeforeMemberMigration(ctx, from, to, path); err != nil {
			return err
		}
	}
	return nil
}

func (m MultiMemberMigrationHooks) AfterMemberMigration(ctx context.Context, from, to sdk.AccAddress, path MigrationPath) error {
	for _, h := range m {
		if h == nil {
			continue
		}
		if err := h.AfterMemberMigration(ctx, from, to, path); err != nil {
			return err
		}
	}
	return nil
}

var _ MemberMigrationHooks = MultiMemberMigrationHooks{}
