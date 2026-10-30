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
// re-register from the new address instead of migrating.
//
// On the KEY path the owner still holds the old key, so a live operator
// vetoes: retire it first. On the RECOVERY and COUNCIL paths the old key is
// presumed lost or stolen, and a thief holding it could register an operator
// on the old address just to veto the recovery (REP-R-02). There the hook
// does not veto; finalization forces the old address's ACTIVE and
// UNDERFUNDED operators into UNBONDING, and the canonical address collects
// the matured exit bond through MsgSweepTombstone (ClaimForTombstone). The
// target must never be a live operator.
type MigrationHooks struct {
	k *Keeper
}

// NewMigrationHooks returns the x/service migration hooks.
func NewMigrationHooks(k *Keeper) MigrationHooks {
	return MigrationHooks{k: k}
}

var _ reptypes.MemberMigrationHooks = MigrationHooks{}

// isLiveOperatorStatus reports whether an operator record still carries a
// bond or a role: RETIRED and the terminal SLASHED do not.
func isLiveOperatorStatus(s types.OperatorStatus) bool {
	switch s {
	case types.OperatorStatus_OPERATOR_STATUS_ACTIVE,
		types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED,
		types.OperatorStatus_OPERATOR_STATUS_UNBONDING:
		return true
	}
	return false
}

// liveOperators returns addr's operator records in a live status.
func (k Keeper) liveOperators(ctx context.Context, addr sdk.AccAddress) []types.Operator {
	var live []types.Operator
	rng := collections.NewPrefixedPairRange[[]byte, string]([]byte(addr))
	if err := k.Operators.Walk(ctx, rng, func(_ collections.Pair[[]byte, string], op types.Operator) (bool, error) {
		if isLiveOperatorStatus(op.Status) {
			live = append(live, op)
		}
		return false, nil
	}); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("liveOperators: Operators.Walk failed", "err", err)
	}
	return live
}

// liveOperatorTypes lists the service types in which addr is a live
// operator.
func (k Keeper) liveOperatorTypes(ctx context.Context, addr sdk.AccAddress) []string {
	var out []string
	for _, op := range k.liveOperators(ctx, addr) {
		out = append(out, op.ServiceType)
	}
	return out
}

// BeforeMemberMigration vetoes a live operator on the old address (KEY path
// only) and a live operator on the new address (every path).
func (h MigrationHooks) BeforeMemberMigration(ctx context.Context, from, to sdk.AccAddress, path reptypes.MigrationPath) error {
	if path == reptypes.MigrationPath_MIGRATION_PATH_KEY {
		if live := h.k.liveOperatorTypes(ctx, from); len(live) > 0 {
			return fmt.Errorf("x/service: %s is an operator of %v; retire and re-register from the new address", from.String(), live)
		}
	}
	if live := h.k.liveOperatorTypes(ctx, to); len(live) > 0 {
		return fmt.Errorf("x/service: %s is already an operator of %v", to.String(), live)
	}
	return nil
}

// AfterMemberMigration forces the old address's ACTIVE and UNDERFUNDED
// operators into UNBONDING on the RECOVERY and COUNCIL paths (the KEY path
// vetoed them already). Reports, escrows and the unbonding clock run as for
// a voluntary exit; the bond stays slashable until claimed.
func (h MigrationHooks) AfterMemberMigration(ctx context.Context, from, to sdk.AccAddress, path reptypes.MigrationPath) error {
	if path == reptypes.MigrationPath_MIGRATION_PATH_KEY {
		return nil
	}
	for _, op := range h.k.liveOperators(ctx, from) {
		if op.Status != types.OperatorStatus_OPERATOR_STATUS_ACTIVE &&
			op.Status != types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED {
			continue
		}
		if err := h.k.startOperatorUnbond(ctx, op, []byte(from), types.UnbondSourceForcedMigration); err != nil {
			return err
		}
	}
	return nil
}

// ClaimForTombstone claims every matured exit bond of the operators on a
// migrated (tombstoned) address. x/rep calls it from MsgSweepTombstone,
// signed by the canonical address, so a recovered member collects the bond
// the old key can no longer claim; the bank send restriction delivers it to
// the canonical address. Operators whose claim gates are not met (unbonding
// still running, open reports, active Tier-1 escrow) are skipped and can be
// swept again later. Each claim runs in a child cache context, so a failure
// leaves no partial state.
func (k Keeper) ClaimForTombstone(ctx context.Context, tombstone, _ sdk.AccAddress) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	for _, op := range k.liveOperators(ctx, tombstone) {
		if op.Status != types.OperatorStatus_OPERATOR_STATUS_UNBONDING {
			continue
		}
		cacheCtx, write := sdkCtx.CacheContext()
		if err := k.claimUnbondedExit(cacheCtx, op, []byte(tombstone)); err != nil {
			sdkCtx.Logger().Info("x/service: tombstone exit claim skipped",
				"operator", op.Address, "service_type", op.ServiceType, "reason", err)
			continue
		}
		write()
	}
	return nil
}
