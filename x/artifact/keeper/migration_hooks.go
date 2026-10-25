package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
	reptypes "sparkdream/x/rep/types"
)

// MigrationHooks implements x/rep's MemberMigrationHooks for x/artifact
// (member-key-migration plan §8, artifact row). Everything artifact holds
// for a member (classes, tokens, listings, offers, inbox items) is read
// through the alias (CanActAs / RecentAddressesOf), so nothing moves. The
// one veto: a class-owner handover proposed TO the old address, which names
// an address rather than an identity and would otherwise land on a
// tombstone.
type MigrationHooks struct {
	k *Keeper
}

// NewMigrationHooks returns the x/artifact migration hooks.
func NewMigrationHooks(k *Keeper) MigrationHooks {
	return MigrationHooks{k: k}
}

var _ reptypes.MemberMigrationHooks = MigrationHooks{}

// BeforeMemberMigration vetoes while a pending class-owner proposal names
// the old address.
func (h MigrationHooks) BeforeMemberMigration(ctx context.Context, from, to sdk.AccAddress, path reptypes.MigrationPath) error {
	fromStr := from.String()
	var pending uint64
	_ = h.k.PendingClassOwners.Walk(ctx, nil, func(classID uint64, p types.PendingClassOwner) (bool, error) {
		if p.ProposedOwner == fromStr {
			pending = classID
			return true, nil
		}
		return false, nil
	})
	if pending != 0 {
		return fmt.Errorf("x/artifact: class %d has a pending owner handover to %s; accept or let it expire first", pending, fromStr)
	}
	return nil
}

// AfterMemberMigration has nothing to move: artifact state is lazy.
func (h MigrationHooks) AfterMemberMigration(ctx context.Context, from, to sdk.AccAddress, path reptypes.MigrationPath) error {
	return nil
}
