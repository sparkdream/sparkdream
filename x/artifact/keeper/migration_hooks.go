package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	reptypes "sparkdream/x/rep/types"
)

// MigrationHooks implements x/rep's MemberMigrationHooks for x/artifact
// (member-key-migration plan §8, artifact row). Everything artifact holds
// for a member (classes, tokens, listings, offers, inbox items, pending
// class-owner handovers) is read through the alias (CanActAs /
// RecentAddressesOf), so nothing moves and nothing vetoes: a handover
// proposed to the old address is accepted by the successor
// (MsgAcceptClassOwner checks CanActAs), so no third party can block a
// migration by proposing a handover to the migrating address.
type MigrationHooks struct {
	k *Keeper
}

// NewMigrationHooks returns the x/artifact migration hooks.
func NewMigrationHooks(k *Keeper) MigrationHooks {
	return MigrationHooks{k: k}
}

var _ reptypes.MemberMigrationHooks = MigrationHooks{}

// BeforeMemberMigration never vetoes (see MigrationHooks).
func (h MigrationHooks) BeforeMemberMigration(ctx context.Context, from, to sdk.AccAddress, path reptypes.MigrationPath) error {
	return nil
}

// AfterMemberMigration has nothing to move: artifact state is lazy.
func (h MigrationHooks) AfterMemberMigration(ctx context.Context, from, to sdk.AccAddress, path reptypes.MigrationPath) error {
	return nil
}
