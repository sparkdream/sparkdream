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

// EligibleForRole returns the caller's BondedRole if it may take a
// role-gated action right now, or a typed error explaining why not. This is
// the SHARED action-time eligibility check for every module consuming a
// bonded role (forum + collect for the sentinel role today) — the logic
// originated as forum's eligibleSentinel and was hoisted here so consuming
// modules cannot drift (collect previously accepted UNBONDING sentinels
// whose staying bond no longer covered the role minimum).
//
// NORMAL and RECOVERY are eligible outright. UNBONDING is eligible while
// the *staying* bond (current_bond - pending_unbond_amount) stays at or
// above the role's configured min_bond; the portion being withdrawn is
// treated as already gone. The action's own ReserveBond call (pending-aware)
// separately enforces that any reserved slash fits in the staying,
// uncommitted bond. DEMOTED (and any future ineligible status) is never
// eligible.
//
// There is deliberately no time / unbond-completion comparison: eligibility
// is a pure bond-quantity question. Safety comes from earmarked (committed)
// bond surviving the unbond — not from timing.
//
// Missing BondedRoleConfig mirrors computeBondStatus's lenient fallback:
// the min bond is treated as zero, so an UNBONDING role stays eligible
// while any bond stays behind.
//
// Errors: ErrBondedRoleNotFound (no role), ErrRoleDemoted,
// ErrRoleUnbondingBelowMin. Module-local caps (overturn cooldowns,
// per-epoch limits, age gates) remain the owning module's job.
func (k Keeper) EligibleForRole(ctx context.Context, roleType types.RoleType, addr string) (types.BondedRole, error) {
	br, err := k.GetBondedRole(ctx, roleType, addr)
	if err != nil {
		return br, err
	}

	switch br.BondStatus {
	case types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL,
		types.BondedRoleStatus_BONDED_ROLE_STATUS_RECOVERY:
		return br, nil

	case types.BondedRoleStatus_BONDED_ROLE_STATUS_UNBONDING:
		current, cerr := parseIntOrZero(br.CurrentBond)
		if cerr != nil {
			return br, errorsmod.Wrapf(types.ErrInvalidAmount,
				"invalid bonded role current_bond: %q", br.CurrentBond)
		}
		pending, perr := parseIntOrZero(br.PendingUnbondAmount)
		if perr != nil {
			return br, errorsmod.Wrapf(types.ErrInvalidAmount,
				"invalid bonded role pending_unbond_amount: %q", br.PendingUnbondAmount)
		}
		minBond := math.ZeroInt()
		if cfg, cfgErr := k.BondedRoleConfigs.Get(ctx, int32(roleType)); cfgErr == nil {
			minBond = mustParseIntOrZero(cfg.MinBond)
		}
		if current.Sub(pending).LT(minBond) {
			return br, errorsmod.Wrap(types.ErrRoleUnbondingBelowMin,
				"staying bond below role minimum during unbond")
		}
		return br, nil

	default: // DEMOTED and any future ineligible status
		return br, errorsmod.Wrapf(types.ErrRoleDemoted, "status %s", br.BondStatus.String())
	}
}

// GetAvailableBond returns the bond that can actually back a new action:
// current_bond minus total_committed_bond minus pending_unbond_amount. Bond
// that is leaving (pending) must not back new reservations, otherwise an
// action's slash could land on DREAM that has already matured and unlocked.
// Returns zero (no error) if the record does not exist. When no unbond is in
// flight pending is zero, so behavior is unchanged for non-unbonding roles.
func (k Keeper) GetAvailableBond(ctx context.Context, roleType types.RoleType, addr string) (math.Int, error) {
	if err := validateRoleType(roleType); err != nil {
		return math.ZeroInt(), err
	}
	br, err := k.BondedRoles.Get(ctx, bondedRoleKey(roleType, addr))
	if err != nil {
		return math.ZeroInt(), nil
	}
	current, err := parseIntOrZero(br.CurrentBond)
	if err != nil {
		return math.ZeroInt(), err
	}
	committed, err := parseIntOrZero(br.TotalCommittedBond)
	if err != nil {
		return math.ZeroInt(), err
	}
	pending, err := parseIntOrZero(br.PendingUnbondAmount)
	if err != nil {
		return math.ZeroInt(), err
	}
	available := current.Sub(committed).Sub(pending)
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
	current, err := parseIntOrZero(br.CurrentBond)
	if err != nil {
		return err
	}
	committed, err := parseIntOrZero(br.TotalCommittedBond)
	if err != nil {
		return err
	}
	// Reservations draw only on the staying bond: subtract pending_unbond_amount
	// so a slash can never be committed against DREAM that is on its way out and
	// will unlock at unbond maturity. Zero when no unbond is in flight.
	pending, err := parseIntOrZero(br.PendingUnbondAmount)
	if err != nil {
		return err
	}
	available := current.Sub(committed).Sub(pending)
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
	committed, err := parseIntOrZero(br.TotalCommittedBond)
	if err != nil {
		return err
	}
	released := committed.Sub(amount)
	if released.IsNegative() {
		released = math.ZeroInt()
	}
	br.TotalCommittedBond = released.String()
	return k.BondedRoles.Set(ctx, key, br)
}

// IncreaseBond locks `amount` DREAM from the role-holder's available balance
// and credits it to current_bond on the BondedRole record. Used by reward
// flows that auto-bond DREAM payouts (e.g. federation verifier rewards in
// RECOVERY status). Recomputes bond_status against the role's thresholds so
// crossing min_bond auto-promotes RECOVERY → NORMAL.
//
// Rejects when an unbond is in flight (state-machine linearity matches
// MsgBondRole's rule) and when the role record is missing. Does not enforce
// trust-level / rep-tier gates — those apply only to user-initiated bonding.
func (k Keeper) IncreaseBond(ctx context.Context, roleType types.RoleType, addr string, amount math.Int) error {
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
	if br.BondStatus == types.BondedRoleStatus_BONDED_ROLE_STATUS_UNBONDING {
		return errorsmod.Wrap(types.ErrInvalidRequest,
			"cannot increase bond while UNBONDING is in flight")
	}
	roleAddr, addrErr := sdk.AccAddressFromBech32(addr)
	if addrErr != nil {
		return fmt.Errorf("invalid role-holder address: %w", addrErr)
	}
	if err := k.LockDREAM(ctx, roleAddr, amount); err != nil {
		return fmt.Errorf("failed to lock DREAM for bond increase: %w", err)
	}
	current, err := parseIntOrZero(br.CurrentBond)
	if err != nil {
		return err
	}
	newBond := current.Add(amount)
	br.CurrentBond = newBond.String()
	br.BondStatus = k.computeBondStatus(ctx, roleType, newBond)
	if err := k.BondedRoles.Set(ctx, key, br); err != nil {
		return err
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"bonded_role_bond_increased",
			sdk.NewAttribute("role_type", roleType.String()),
			sdk.NewAttribute("address", addr),
			sdk.NewAttribute("amount", amount.String()),
			sdk.NewAttribute("total_bond", br.CurrentBond),
			sdk.NewAttribute("bond_status", br.BondStatus.String()),
		),
	)
	return nil
}

// RecordRewardPayout updates the LastRewardEpoch and CumulativeRewards
// counters on the BondedRole record. Called by role-owning modules from
// their per-role reward distribution flow (e.g. federation verifier
// epoch rewards). No-op when the record is missing — callers iterating
// activity counters may pass addresses without a bond record.
func (k Keeper) RecordRewardPayout(ctx context.Context, roleType types.RoleType, addr string, epoch int64, amount math.Int) error {
	if err := validateRoleType(roleType); err != nil {
		return err
	}
	if amount.IsNegative() {
		return types.ErrInvalidAmount
	}
	key := bondedRoleKey(roleType, addr)
	br, err := k.BondedRoles.Get(ctx, key)
	if err != nil {
		return nil
	}
	prev, err := parseIntOrZero(br.CumulativeRewards)
	if err != nil {
		return fmt.Errorf("invalid cumulative_rewards on bonded role: %w", err)
	}
	br.CumulativeRewards = prev.Add(amount).String()
	br.LastRewardEpoch = epoch
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
	current, err := parseIntOrZero(br.CurrentBond)
	if err != nil {
		return err
	}
	committed, err := parseIntOrZero(br.TotalCommittedBond)
	if err != nil {
		return err
	}

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

	// If an unbond is in flight, cap pending_unbond_amount at the new
	// current_bond. The pending withdrawal cannot exceed what's still locked;
	// slashes during the cooldown reduce the eventual payout.
	if br.BondStatus == types.BondedRoleStatus_BONDED_ROLE_STATUS_UNBONDING {
		pending, perr := parseIntOrZero(br.PendingUnbondAmount)
		if perr == nil && pending.GT(newCurrent) {
			br.PendingUnbondAmount = newCurrent.String()
		}
		// Status stays UNBONDING — only MatureUnbonds flips it. A mid-flight
		// slash that drains current_bond to zero still waits for completion
		// time so the holder can observe the outcome before re-bonding.
	} else {
		// Re-evaluate bond_status against the role's config thresholds, but
		// never make it LESS severe than it already is. A slash only ever
		// lowers current_bond, so the recomputed status can't legitimately be
		// milder than the pre-slash one on bond grounds alone -- the only way
		// that happens is when the existing status came from somewhere other
		// than the bond amount, i.e. a RecordRoleOutcome overturn-streak
		// demotion of a holder whose bond is still healthy. Clobbering that
		// back to NORMAL/RECOVERY silently undoes the demotion, and whether it
		// happened depended on whether the owning module called SlashBond
		// before or after RecordRoleOutcome. It no longer does.
		br.BondStatus = moreSevereBondStatus(br.BondStatus,
			k.computeBondStatus(ctx, roleType, newCurrent))
	}

	if err := k.BondedRoles.Set(ctx, key, br); err != nil {
		return err
	}

	// Stamp the slash onto the shared accountability record so a reward
	// distribution can refuse to pay a role in the same epoch it was slashed
	// for. Best-effort: the bond is already written, and losing the stamp must
	// not roll back a slash.
	if err := k.stampRoleSlashEpoch(ctx, roleType, addr); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Warn("failed to stamp role slash epoch",
			"role_type", roleType.String(), "address", addr, "error", err)
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

// MatureUnbonds walks BondedRole records and finalizes any whose
// UnbondCompletionTime has elapsed: unlocks the pending DREAM, clears the
// pending fields, transitions bond_status to DEMOTED, and starts the role's
// demotion_cooldown gating re-bonding. Called from the rep EndBlocker.
//
// A mid-flight slash that emptied current_bond to zero still reaches maturity
// here — the holder gets back whatever's left of the pending amount (capped
// by current_bond invariant maintained in SlashBond).
func (k Keeper) MatureUnbonds(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// Collect maturity candidates first; mutate after the iterator closes.
	type matureCandidate struct {
		roleType types.RoleType
		addr     string
	}
	var due []matureCandidate

	if err := k.BondedRoles.Walk(ctx, nil, func(key collections.Pair[int32, string], br types.BondedRole) (bool, error) {
		if br.BondStatus != types.BondedRoleStatus_BONDED_ROLE_STATUS_UNBONDING {
			return false, nil
		}
		if br.UnbondCompletionTime == 0 || br.UnbondCompletionTime > now {
			return false, nil
		}
		due = append(due, matureCandidate{
			roleType: types.RoleType(key.K1()),
			addr:     key.K2(),
		})
		return false, nil
	}); err != nil {
		return err
	}

	for _, c := range due {
		if err := k.matureSingleUnbond(ctx, c.roleType, c.addr, now); err != nil {
			sdkCtx.Logger().Error("failed to mature unbond",
				"role_type", c.roleType.String(),
				"address", c.addr,
				"error", err)
		}
	}
	return nil
}

func (k Keeper) matureSingleUnbond(ctx context.Context, roleType types.RoleType, addr string, now int64) error {
	key := bondedRoleKey(roleType, addr)
	br, err := k.BondedRoles.Get(ctx, key)
	if err != nil {
		return err
	}

	pending, err := parseIntOrZero(br.PendingUnbondAmount)
	if err != nil {
		return err
	}
	current, err := parseIntOrZero(br.CurrentBond)
	if err != nil {
		return err
	}
	committed, err := parseIntOrZero(br.TotalCommittedBond)
	if err != nil {
		return err
	}

	// Cap pending at current_bond — invariant maintained by SlashBond, but
	// double-check here so a corrupt record can't unlock more DREAM than
	// is actually locked.
	if pending.GT(current) {
		pending = current
	}

	// Never release earmarked (committed) bond: maturity may only unlock bond
	// that is staying-but-leaving, never bond reserved to back an outstanding
	// action whose appeal window is still open. With pending-aware GetAvailableBond
	// /ReserveBond (above) the invariant current >= committed + pending holds by
	// construction, so release == pending in practice; this cap is the regression
	// guard that keeps rep and forum decoupled regardless of EndBlocker ordering.
	release := pending
	if maxReleasable := current.Sub(committed); release.GT(maxReleasable) {
		release = maxReleasable
	}
	if release.IsNegative() {
		release = math.ZeroInt()
	}

	if release.IsPositive() {
		roleAddr, addrErr := sdk.AccAddressFromBech32(addr)
		if addrErr != nil {
			return fmt.Errorf("invalid role-holder address: %w", addrErr)
		}
		if err := k.UnlockDREAM(ctx, roleAddr, release); err != nil {
			return fmt.Errorf("failed to unlock pending DREAM: %w", err)
		}
	}

	newCurrent := current.Sub(release)

	// If committed bond blocked part of the withdrawal, leave the remainder
	// pending and keep the role UNBONDING so a later block retries once the
	// reservation is released or slashed. Otherwise finalize the unbond.
	remainingPending := pending.Sub(release)
	if remainingPending.IsPositive() {
		br.CurrentBond = newCurrent.String()
		br.PendingUnbondAmount = remainingPending.String()
		// Status stays UNBONDING; UnbondCompletionTime stays set so MatureUnbonds
		// revisits this record on the next block.
		if err := k.BondedRoles.Set(ctx, key, br); err != nil {
			return err
		}
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				"bonded_role_unbond_deferred",
				sdk.NewAttribute("role_type", roleType.String()),
				sdk.NewAttribute("address", addr),
				sdk.NewAttribute("unlocked_amount", release.String()),
				sdk.NewAttribute("remaining_pending", remainingPending.String()),
			),
		)
		return nil
	}

	br.CurrentBond = newCurrent.String()
	br.PendingUnbondAmount = "0"
	br.UnbondCompletionTime = 0
	// Recompute status from the final bond against the role's thresholds.
	// A partial unbond that leaves the bond ≥ min_bond stays NORMAL; a drop
	// into the recovery band lands at RECOVERY; only crossing below
	// demotion_threshold lands at DEMOTED. This matches the legacy
	// immediate-unlock path so partial-unbond intent is preserved through
	// the cooldown.
	br.BondStatus = k.computeBondStatus(ctx, roleType, newCurrent)

	// Start demotion_cooldown only when the matured unbond actually drops
	// the holder into DEMOTED — partial unbonds that keep the role active
	// don't need a re-bond gate.
	if br.BondStatus == types.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED {
		cfg, cfgErr := k.GetBondedRoleConfig(ctx, roleType)
		if cfgErr == nil {
			br.DemotionCooldownUntil = now + cfg.DemotionCooldown
		}
	}

	if err := k.BondedRoles.Set(ctx, key, br); err != nil {
		return err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"bonded_role_unbond_matured",
			sdk.NewAttribute("role_type", roleType.String()),
			sdk.NewAttribute("address", addr),
			sdk.NewAttribute("unlocked_amount", pending.String()),
			sdk.NewAttribute("remaining_bond", br.CurrentBond),
			sdk.NewAttribute("demotion_cooldown_until", fmt.Sprintf("%d", br.DemotionCooldownUntil)),
		),
	)
	return nil
}

// computeBondStatus maps a role's current_bond against its config thresholds
// to NORMAL / RECOVERY / DEMOTED. If no config exists, defaults to NORMAL
// when current_bond > 0, else DEMOTED.
// clampStatusToDemotionCooldown holds a role at DEMOTED for as long as its
// demotion cooldown is still running, whatever the bond size says.
//
// computeBondStatus derives status from bond size alone, which made the
// demotion punishment escapable in one round trip: unbond a token amount
// (status -> UNBONDING), then cancel the unbond in full. The cancel path
// recomputed status from the bond, saw it above min_bond, and wrote NORMAL —
// voiding a cooldown that had not elapsed. MsgBondRole checks the cooldown
// before re-bonding; nothing checked it on the way back from UNBONDING.
func clampStatusToDemotionCooldown(br types.BondedRole, now int64, status types.BondedRoleStatus) types.BondedRoleStatus {
	if br.DemotionCooldownUntil > now {
		return types.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED
	}
	return status
}

func (k Keeper) computeBondStatus(ctx context.Context, roleType types.RoleType, currentBond math.Int) types.BondedRoleStatus {
	cfg, err := k.BondedRoleConfigs.Get(ctx, int32(roleType))
	if err != nil {
		if currentBond.IsPositive() {
			return types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL
		}
		return types.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED
	}
	// SetBondedRoleConfig validates these fields on write, so a parse error here
	// signals corrupt state and must not be silently coerced to zero.
	minBond := mustParseIntOrZero(cfg.MinBond)
	demotionThreshold := mustParseIntOrZero(cfg.DemotionThreshold)
	switch {
	case currentBond.GTE(minBond):
		return types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL
	case currentBond.GTE(demotionThreshold):
		return types.BondedRoleStatus_BONDED_ROLE_STATUS_RECOVERY
	default:
		return types.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED
	}
}

// bondStatusSeverity ranks bond statuses so a slash can pick the harsher of
// two candidates. UNBONDING is deliberately absent: SlashBond returns before
// reaching here for an in-flight unbond, and MatureUnbonds owns that
// transition.
func bondStatusSeverity(s types.BondedRoleStatus) int {
	switch s {
	case types.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED:
		return 2
	case types.BondedRoleStatus_BONDED_ROLE_STATUS_RECOVERY:
		return 1
	default:
		return 0
	}
}

// moreSevereBondStatus returns whichever of the two statuses is harsher.
func moreSevereBondStatus(a, b types.BondedRoleStatus) types.BondedRoleStatus {
	if bondStatusSeverity(a) >= bondStatusSeverity(b) {
		return a
	}
	return b
}

// parseIntOrZero parses a math.Int-string; empty strings map to zero. A non-empty
// string that fails to parse is a data-corruption error — callers decide whether
// to propagate or, in genesis paths where the chain cannot boot, to panic.
func parseIntOrZero(s string) (math.Int, error) {
	if s == "" {
		return math.ZeroInt(), nil
	}
	v, ok := math.NewIntFromString(s)
	if !ok {
		return math.ZeroInt(), fmt.Errorf("invalid math.Int string: %q", s)
	}
	return v, nil
}

// mustParseIntOrZero is a thin wrapper for genesis-loading paths where a parse
// failure indicates corrupt state and the chain cannot safely boot.
func mustParseIntOrZero(s string) math.Int {
	v, err := parseIntOrZero(s)
	if err != nil {
		panic(fmt.Errorf("corrupt bonded role field: %w", err))
	}
	return v
}
