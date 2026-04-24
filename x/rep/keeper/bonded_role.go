package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// bondedRoleKey assembles the collections key for BondedRoles.
func bondedRoleKey(roleType types.RoleType, addr string) collections.Pair[int32, string] {
	return collections.Join(int32(roleType), addr)
}

// validateRoleType rejects ROLE_TYPE_UNSPECIFIED and unknown enum values.
func validateRoleType(roleType types.RoleType) error {
	if roleType == types.RoleType_ROLE_TYPE_UNSPECIFIED {
		return errorsmod.Wrap(types.ErrInvalidRoleType, "role_type must be specified")
	}
	if _, ok := types.RoleType_name[int32(roleType)]; !ok {
		return errorsmod.Wrapf(types.ErrInvalidRoleType, "unknown role_type %d", roleType)
	}
	return nil
}

// IsBondedRole reports whether the address holds a BondedRole record for the
// given role_type.
func (k Keeper) IsBondedRole(ctx context.Context, roleType types.RoleType, addr string) (bool, error) {
	if err := validateRoleType(roleType); err != nil {
		return false, err
	}
	return k.BondedRoles.Has(ctx, bondedRoleKey(roleType, addr))
}

// GetBondedRole returns the BondedRole record for (role_type, addr).
func (k Keeper) GetBondedRole(ctx context.Context, roleType types.RoleType, addr string) (types.BondedRole, error) {
	if err := validateRoleType(roleType); err != nil {
		return types.BondedRole{}, err
	}
	br, err := k.BondedRoles.Get(ctx, bondedRoleKey(roleType, addr))
	if err != nil {
		return types.BondedRole{}, errorsmod.Wrapf(types.ErrBondedRoleNotFound,
			"%s:%s", roleType.String(), addr)
	}
	return br, nil
}

// GetAvailableBond returns current_bond minus total_committed_bond for the
// given role. Returns zero (no error) if the record does not exist.
func (k Keeper) GetAvailableBond(ctx context.Context, roleType types.RoleType, addr string) (math.Int, error) {
	if err := validateRoleType(roleType); err != nil {
		return math.ZeroInt(), err
	}
	br, err := k.BondedRoles.Get(ctx, bondedRoleKey(roleType, addr))
	if err != nil {
		return math.ZeroInt(), nil
	}
	current := parseIntOrZero(br.CurrentBond)
	committed := parseIntOrZero(br.TotalCommittedBond)
	available := current.Sub(committed)
	if available.IsNegative() {
		return math.ZeroInt(), nil
	}
	return available, nil
}

// ReserveBond increments total_committed_bond on the role record by amount.
// Errors if available bond < amount or record not found.
func (k Keeper) ReserveBond(ctx context.Context, roleType types.RoleType, addr string, amount math.Int) error {
	if err := validateRoleType(roleType); err != nil {
		return err
	}
	if amount.IsNegative() || amount.IsZero() {
		return types.ErrInvalidAmount
	}
	key := bondedRoleKey(roleType, addr)
	br, err := k.BondedRoles.Get(ctx, key)
	if err != nil {
		return errorsmod.Wrapf(types.ErrBondedRoleNotFound, "%s:%s", roleType.String(), addr)
	}
	current := parseIntOrZero(br.CurrentBond)
	committed := parseIntOrZero(br.TotalCommittedBond)
	available := current.Sub(committed)
	if available.LT(amount) {
		return errorsmod.Wrapf(types.ErrInsufficientBond,
			"need %s available, have %s", amount.String(), available.String())
	}
	br.TotalCommittedBond = committed.Add(amount).String()
	return k.BondedRoles.Set(ctx, key, br)
}

// ReleaseBond decrements total_committed_bond on the role record by amount
// (saturating at zero).
func (k Keeper) ReleaseBond(ctx context.Context, roleType types.RoleType, addr string, amount math.Int) error {
	if err := validateRoleType(roleType); err != nil {
		return err
	}
	if amount.IsNegative() {
		return types.ErrInvalidAmount
	}
	key := bondedRoleKey(roleType, addr)
	br, err := k.BondedRoles.Get(ctx, key)
	if err != nil {
		return errorsmod.Wrapf(types.ErrBondedRoleNotFound, "%s:%s", roleType.String(), addr)
	}
	committed := parseIntOrZero(br.TotalCommittedBond)
	released := committed.Sub(amount)
	if released.IsNegative() {
		released = math.ZeroInt()
	}
	br.TotalCommittedBond = released.String()
	return k.BondedRoles.Set(ctx, key, br)
}

// SlashBond decrements both current_bond and total_committed_bond on the role
// record by amount, and burns the equivalent DREAM from the role-holder's
// member balance (unlock staked → burn, mirroring the author-bond slash).
// Auto-transitions bond_status per the role's BondedRoleConfig thresholds.
func (k Keeper) SlashBond(ctx context.Context, roleType types.RoleType, addr string, amount math.Int, reason string) error {
	if err := validateRoleType(roleType); err != nil {
		return err
	}
	if amount.IsNegative() || amount.IsZero() {
		return types.ErrInvalidAmount
	}
	key := bondedRoleKey(roleType, addr)
	br, err := k.BondedRoles.Get(ctx, key)
	if err != nil {
		return errorsmod.Wrapf(types.ErrBondedRoleNotFound, "%s:%s", roleType.String(), addr)
	}
	current := parseIntOrZero(br.CurrentBond)
	committed := parseIntOrZero(br.TotalCommittedBond)

	// Cap the slash at current_bond (can't slash more than exists).
	slash := amount
	if slash.GT(current) {
		slash = current
	}

	roleAddr, addrErr := sdk.AccAddressFromBech32(addr)
	if addrErr != nil {
		return fmt.Errorf("invalid role-holder address: %w", addrErr)
	}

	// Bonds are held as locked DREAM on the member; slash by unlocking
	// then burning, matching SlashAuthorBond / SlashBond (sentinel) semantics.
	if err := k.UnlockDREAM(ctx, roleAddr, slash); err != nil {
		return fmt.Errorf("failed to unlock DREAM for role slash: %w", err)
	}
	if err := k.BurnDREAM(ctx, roleAddr, slash); err != nil {
		return fmt.Errorf("failed to burn DREAM for role slash: %w", err)
	}

	newCurrent := current.Sub(slash)
	br.CurrentBond = newCurrent.String()
	releasedCommit := committed.Sub(slash)
	if releasedCommit.IsNegative() {
		releasedCommit = math.ZeroInt()
	}
	br.TotalCommittedBond = releasedCommit.String()

	// Re-evaluate bond_status against the role's config thresholds.
	br.BondStatus = k.computeBondStatus(ctx, roleType, newCurrent)

	if err := k.BondedRoles.Set(ctx, key, br); err != nil {
		return err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"bonded_role_slashed",
			sdk.NewAttribute("role_type", roleType.String()),
			sdk.NewAttribute("address", addr),
			sdk.NewAttribute("amount_slashed", slash.String()),
			sdk.NewAttribute("reason", reason),
			sdk.NewAttribute("bond_status", br.BondStatus.String()),
		),
	)
	return nil
}

// RecordActivity stamps the current epoch on the role record and resets
// consecutive_inactive_epochs. Idempotent when already stamped this epoch.
// No-op if the role record is missing.
func (k Keeper) RecordActivity(ctx context.Context, roleType types.RoleType, addr string) error {
	if err := validateRoleType(roleType); err != nil {
		return err
	}
	key := bondedRoleKey(roleType, addr)
	br, err := k.BondedRoles.Get(ctx, key)
	if err != nil {
		return nil
	}
	currentEpoch, err := k.GetCurrentEpoch(ctx)
	if err != nil {
		return err
	}
	if br.LastActiveEpoch == currentEpoch {
		return nil
	}
	br.LastActiveEpoch = currentEpoch
	br.ConsecutiveInactiveEpochs = 0
	return k.BondedRoles.Set(ctx, key, br)
}

// SetBondStatus updates bond_status and demotion_cooldown_until on the role
// record. Used by role-owning modules to demote after consecutive overturns
// or similar policy triggers.
func (k Keeper) SetBondStatus(ctx context.Context, roleType types.RoleType, addr string, statusValue types.BondedRoleStatus, cooldownUntil int64) error {
	if err := validateRoleType(roleType); err != nil {
		return err
	}
	key := bondedRoleKey(roleType, addr)
	br, err := k.BondedRoles.Get(ctx, key)
	if err != nil {
		return errorsmod.Wrapf(types.ErrBondedRoleNotFound, "%s:%s", roleType.String(), addr)
	}
	br.BondStatus = statusValue
	br.DemotionCooldownUntil = cooldownUntil
	if err := k.BondedRoles.Set(ctx, key, br); err != nil {
		return err
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"bonded_role_status_updated",
			sdk.NewAttribute("role_type", roleType.String()),
			sdk.NewAttribute("address", addr),
			sdk.NewAttribute("bond_status", statusValue.String()),
			sdk.NewAttribute("cooldown_until", fmt.Sprintf("%d", cooldownUntil)),
		),
	)
	return nil
}

// GetBondedRoleConfig returns the policy config for the given role_type.
func (k Keeper) GetBondedRoleConfig(ctx context.Context, roleType types.RoleType) (types.BondedRoleConfig, error) {
	if err := validateRoleType(roleType); err != nil {
		return types.BondedRoleConfig{}, err
	}
	cfg, err := k.BondedRoleConfigs.Get(ctx, int32(roleType))
	if err != nil {
		return types.BondedRoleConfig{}, errorsmod.Wrapf(types.ErrBondedRoleConfigMissing,
			"role_type=%s", roleType.String())
	}
	return cfg, nil
}

// SetBondedRoleConfig write-throughs a policy config from the role's owning
// module. Called by forum/collect/federation operational-params handlers on
// change and from their InitGenesis. Inter-module keeper calls are trusted —
// caller authority is not enforced here.
func (k Keeper) SetBondedRoleConfig(ctx context.Context, cfg types.BondedRoleConfig) error {
	if err := validateRoleType(cfg.RoleType); err != nil {
		return err
	}
	// Normalize stringly-typed numeric fields so downstream reads can rely on
	// parseable values.
	if cfg.MinBond == "" {
		cfg.MinBond = "0"
	}
	if cfg.DemotionThreshold == "" {
		cfg.DemotionThreshold = "0"
	}
	if _, ok := math.NewIntFromString(cfg.MinBond); !ok {
		return errorsmod.Wrapf(types.ErrInvalidAmount, "min_bond %q", cfg.MinBond)
	}
	if _, ok := math.NewIntFromString(cfg.DemotionThreshold); !ok {
		return errorsmod.Wrapf(types.ErrInvalidAmount, "demotion_threshold %q", cfg.DemotionThreshold)
	}
	return k.BondedRoleConfigs.Set(ctx, int32(cfg.RoleType), cfg)
}

// computeBondStatus maps a role's current_bond against its config thresholds
// to NORMAL / RECOVERY / DEMOTED. If no config exists, defaults to NORMAL
// when current_bond > 0, else DEMOTED.
func (k Keeper) computeBondStatus(ctx context.Context, roleType types.RoleType, currentBond math.Int) types.BondedRoleStatus {
	cfg, err := k.BondedRoleConfigs.Get(ctx, int32(roleType))
	if err != nil {
		if currentBond.IsPositive() {
			return types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL
		}
		return types.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED
	}
	minBond := parseIntOrZero(cfg.MinBond)
	demotionThreshold := parseIntOrZero(cfg.DemotionThreshold)
	switch {
	case currentBond.GTE(minBond):
		return types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL
	case currentBond.GTE(demotionThreshold):
		return types.BondedRoleStatus_BONDED_ROLE_STATUS_RECOVERY
	default:
		return types.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED
	}
}

// parseIntOrZero parses a math.Int-string, returning zero on empty or parse failure.
func parseIntOrZero(s string) math.Int {
	if s == "" {
		return math.ZeroInt()
	}
	v, ok := math.NewIntFromString(s)
	if !ok {
		return math.ZeroInt()
	}
	return v
}
