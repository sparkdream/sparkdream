package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/service/types"
)

// MigrationHooks implements x/rep's MemberMigrationHooks for x/service
// (member-key-migration plan §8, service row). The operator address is the
// service identity: it carries the SPARK bond, Tier-1 escrow and checkpoint
// deadlines, and bridge bindings hang off it. Operators retire and
// re-register from the new address instead of migrating, so the hook is a
// pure veto while the old address is a non-retired operator in any service
// type, and the target must not be an operator either.
type MigrationHooks struct {
	k *Keeper
}

// NewMigrationHooks returns the x/service migration hooks.
func NewMigrationHooks(k *Keeper) MigrationHooks {
	return MigrationHooks{k: k}
}

var _ reptypes.MemberMigrationHooks = MigrationHooks{}

// liveOperatorTypes lists the service types in which addr is a non-retired
// operator.
func (k Keeper) liveOperatorTypes(ctx context.Context, addr sdk.AccAddress) []string {
	var live []string
	rng := collections.NewPrefixedPairRange[[]byte, string]([]byte(addr))
	_ = k.Operators.Walk(ctx, rng, func(key collections.Pair[[]byte, string], op types.Operator) (bool, error) {
		if op.Status != types.OperatorStatus_OPERATOR_STATUS_RETIRED {
			live = append(live, key.K2())
		}
		return false, nil
	})
	return live
}

// BeforeMemberMigration vetoes while either address is a live operator.
func (h MigrationHooks) BeforeMemberMigration(ctx context.Context, from, to sdk.AccAddress, path reptypes.MigrationPath) error {
	if live := h.k.liveOperatorTypes(ctx, from); len(live) > 0 {
		return fmt.Errorf("x/service: %s is an operator of %v; retire and re-register from the new address", from.String(), live)
	}
	if live := h.k.liveOperatorTypes(ctx, to); len(live) > 0 {
		return fmt.Errorf("x/service: %s is already an operator of %v", to.String(), live)
	}
	return nil
}

// AfterMemberMigration has nothing to move.
func (h MigrationHooks) AfterMemberMigration(ctx context.Context, from, to sdk.AccAddress, path reptypes.MigrationPath) error {
	return nil
}
