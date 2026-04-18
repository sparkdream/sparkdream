package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// IsSentinel reports whether the address has a sentinel record.
func (k Keeper) IsSentinel(ctx context.Context, addr string) (bool, error) {
	return k.SentinelActivity.Has(ctx, addr)
}

// GetSentinel returns the sentinel record for addr.
func (k Keeper) GetSentinel(ctx context.Context, addr string) (types.SentinelActivity, error) {
	sa, err := k.SentinelActivity.Get(ctx, addr)
	if err != nil {
		return types.SentinelActivity{}, errorsmod.Wrap(types.ErrSentinelNotFound, addr)
	}
	return sa, nil
}

// GetAvailableBond returns current_bond minus total_committed_bond.
// Returns zero (no error) if the sentinel does not exist.
func (k Keeper) GetAvailableBond(ctx context.Context, addr string) (math.Int, error) {
	sa, err := k.SentinelActivity.Get(ctx, addr)
	if err != nil {
		return math.ZeroInt(), nil
	}
	current := parseIntOrZero(sa.CurrentBond)
	committed := parseIntOrZero(sa.TotalCommittedBond)
	available := current.Sub(committed)
	if available.IsNegative() {
		return math.ZeroInt(), nil
	}
	return available, nil
}

// ReserveBond increments total_committed_bond by amount.
// Errors if available bond < amount or sentinel not found.
func (k Keeper) ReserveBond(ctx context.Context, addr string, amount math.Int) error {
	if amount.IsNegative() || amount.IsZero() {
		return types.ErrInvalidAmount
	}
	sa, err := k.SentinelActivity.Get(ctx, addr)
	if err != nil {
		return errorsmod.Wrap(types.ErrSentinelNotFound, addr)
	}
	current := parseIntOrZero(sa.CurrentBond)
	committed := parseIntOrZero(sa.TotalCommittedBond)
	available := current.Sub(committed)
	if available.LT(amount) {
		return errorsmod.Wrapf(types.ErrInsufficientSentinelBond,
			"need %s available, have %s", amount.String(), available.String())
	}
	sa.TotalCommittedBond = committed.Add(amount).String()
	return k.SentinelActivity.Set(ctx, addr, sa)
}

// ReleaseBond decrements total_committed_bond by amount (saturating at zero).
func (k Keeper) ReleaseBond(ctx context.Context, addr string, amount math.Int) error {
	if amount.IsNegative() {
		return types.ErrInvalidAmount
	}
	sa, err := k.SentinelActivity.Get(ctx, addr)
	if err != nil {
		return errorsmod.Wrap(types.ErrSentinelNotFound, addr)
	}
	committed := parseIntOrZero(sa.TotalCommittedBond)
	released := committed.Sub(amount)
	if released.IsNegative() {
		released = math.ZeroInt()
	}
	sa.TotalCommittedBond = released.String()
	return k.SentinelActivity.Set(ctx, addr, sa)
}

// SlashBond decrements both current_bond and total_committed_bond by amount
// and burns the equivalent DREAM from the sentinel's member balance.
// Mirrors the author bond slash pattern (unlock staked → burn).
func (k Keeper) SlashBond(ctx context.Context, addr string, amount math.Int, reason string) error {
	if amount.IsNegative() || amount.IsZero() {
		return types.ErrInvalidAmount
	}
	sa, err := k.SentinelActivity.Get(ctx, addr)
	if err != nil {
		return errorsmod.Wrap(types.ErrSentinelNotFound, addr)
	}
	current := parseIntOrZero(sa.CurrentBond)
	committed := parseIntOrZero(sa.TotalCommittedBond)

	// Cap the slash at current_bond (can't slash more than exists).
	slash := amount
	if slash.GT(current) {
		slash = current
	}

	sentinelAddr, addrErr := sdk.AccAddressFromBech32(addr)
	if addrErr != nil {
		return fmt.Errorf("invalid sentinel address: %w", addrErr)
	}

	// Sentinel bonds are held as locked DREAM on the member; slash by
	// unlocking then burning, matching SlashAuthorBond semantics.
	if err := k.UnlockDREAM(ctx, sentinelAddr, slash); err != nil {
		return fmt.Errorf("failed to unlock DREAM for sentinel slash: %w", err)
	}
	if err := k.BurnDREAM(ctx, sentinelAddr, slash); err != nil {
		return fmt.Errorf("failed to burn DREAM for sentinel slash: %w", err)
	}

	sa.CurrentBond = current.Sub(slash).String()
	releasedCommit := committed.Sub(slash)
	if releasedCommit.IsNegative() {
		releasedCommit = math.ZeroInt()
	}
	sa.TotalCommittedBond = releasedCommit.String()

	if err := k.SentinelActivity.Set(ctx, addr, sa); err != nil {
		return err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"sentinel_bond_slashed",
			sdk.NewAttribute("sentinel", addr),
			sdk.NewAttribute("amount_slashed", slash.String()),
			sdk.NewAttribute("reason", reason),
		),
	)
	return nil
}

// RecordActivity stamps the current epoch and resets consecutive_inactive_epochs.
// Idempotent when already stamped this epoch. No-op if sentinel missing.
func (k Keeper) RecordActivity(ctx context.Context, addr string) error {
	sa, err := k.SentinelActivity.Get(ctx, addr)
	if err != nil {
		return nil
	}
	currentEpoch, err := k.GetCurrentEpoch(ctx)
	if err != nil {
		return err
	}
	if sa.LastActiveEpoch == currentEpoch {
		return nil
	}
	sa.LastActiveEpoch = currentEpoch
	sa.ConsecutiveInactiveEpochs = 0
	return k.SentinelActivity.Set(ctx, addr, sa)
}

// SetBondStatus updates bond_status and demotion_cooldown_until.
func (k Keeper) SetBondStatus(ctx context.Context, addr string, status types.SentinelBondStatus, cooldownUntil int64) error {
	sa, err := k.SentinelActivity.Get(ctx, addr)
	if err != nil {
		return errorsmod.Wrap(types.ErrSentinelNotFound, addr)
	}
	sa.BondStatus = status
	sa.DemotionCooldownUntil = cooldownUntil
	if err := k.SentinelActivity.Set(ctx, addr, sa); err != nil {
		return err
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"sentinel_bond_status_updated",
			sdk.NewAttribute("sentinel", addr),
			sdk.NewAttribute("bond_status", status.String()),
			sdk.NewAttribute("cooldown_until", fmt.Sprintf("%d", cooldownUntil)),
		),
	)
	return nil
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
