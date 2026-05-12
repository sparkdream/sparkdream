package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// CreateAuthorBond creates an author bond stake on a content item.
// Only callable by content modules (not via MsgStake) since authorship verification
// lives in the content module.
func (k Keeper) CreateAuthorBond(
	ctx context.Context,
	author sdk.AccAddress,
	targetType types.StakeTargetType,
	targetID uint64,
	amount math.Int,
) (uint64, error) {
	if !types.IsAuthorBondType(targetType) {
		return 0, types.ErrNotAuthorBondType
	}

	if amount.IsNegative() || amount.IsZero() {
		return 0, types.ErrInvalidAmount
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0, err
	}

	// Validate bond amount cap
	if amount.GT(params.MaxAuthorBondPerContent) {
		return 0, types.ErrAuthorBondCap
	}

	// Check no existing bond (one bond per content item)
	existingStakes, err := k.GetStakesByTarget(ctx, targetType, targetID)
	if err != nil {
		return 0, fmt.Errorf("failed to check existing bonds: %w", err)
	}
	if len(existingStakes) > 0 {
		return 0, types.ErrAuthorBondExists
	}

	// Lock DREAM from author
	if err := k.LockDREAM(ctx, author, amount); err != nil {
		return 0, fmt.Errorf("failed to lock DREAM for author bond: %w", err)
	}

	// Get next stake ID
	stakeID, err := k.StakeSeq.Next(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get next stake ID: %w", err)
	}

	// Create stake
	stake := types.Stake{
		Id:               stakeID,
		Staker:           author.String(),
		TargetType:       targetType,
		TargetId:         targetID,
		TargetIdentifier: "", // Not used for author bonds
		Amount:           amount,
		CreatedAt:        sdk.UnwrapSDKContext(ctx).BlockTime().Unix(),
		LastClaimedAt:    0,
		RewardDebt:       math.ZeroInt(),
	}

	// Store stake
	if err := k.Stake.Set(ctx, stakeID, stake); err != nil {
		return 0, fmt.Errorf("failed to store author bond: %w", err)
	}

	// Add to target index
	if err := k.AddStakeToTargetIndex(ctx, stake); err != nil {
		return 0, fmt.Errorf("failed to add author bond to target index: %w", err)
	}

	// Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"author_bond_created",
			sdk.NewAttribute("stake_id", fmt.Sprintf("%d", stakeID)),
			sdk.NewAttribute("author", author.String()),
			sdk.NewAttribute("target_type", targetType.String()),
			sdk.NewAttribute("target_id", fmt.Sprintf("%d", targetID)),
			sdk.NewAttribute("amount", amount.String()),
		),
	)

	return stakeID, nil
}

// GetAuthorBond returns the author bond stake for a content item.
// Returns ErrAuthorBondNotFound if no bond exists.
func (k Keeper) GetAuthorBond(ctx context.Context, targetType types.StakeTargetType, targetID uint64) (types.Stake, error) {
	if !types.IsAuthorBondType(targetType) {
		return types.Stake{}, types.ErrNotAuthorBondType
	}

	stakes, err := k.GetStakesByTarget(ctx, targetType, targetID)
	if err != nil {
		return types.Stake{}, err
	}

	if len(stakes) == 0 {
		return types.Stake{}, types.ErrAuthorBondNotFound
	}

	// There should only be one bond per content item
	return stakes[0], nil
}

// SlashAuthorBond slashes (burns) the author bond for a content item.
// Called by content modules when content is moderated (e.g., sentinel hide).
// Returns nil silently if no bond exists or slashing is disabled.
func (k Keeper) SlashAuthorBond(ctx context.Context, targetType types.StakeTargetType, targetID uint64) error {
	if !types.IsAuthorBondType(targetType) {
		return types.ErrNotAuthorBondType
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	// Check if slashing is enabled
	if !params.AuthorBondSlashOnModeration {
		return nil
	}

	// Get the bond; if not found, nothing to slash
	bond, err := k.GetAuthorBond(ctx, targetType, targetID)
	if err != nil {
		return nil // No bond to slash — not an error
	}

	authorAddr, err := sdk.AccAddressFromBech32(bond.Staker)
	if err != nil {
		return fmt.Errorf("invalid author address in bond: %w", err)
	}

	// Two-step slash: unlock from staked balance, then burn from free balance
	// Both operations happen in the same Cosmos SDK tx, so they're atomic
	if err := k.UnlockDREAM(ctx, authorAddr, bond.Amount); err != nil {
		return fmt.Errorf("failed to unlock DREAM for slashing: %w", err)
	}
	if err := k.BurnDREAM(ctx, authorAddr, bond.Amount); err != nil {
		return fmt.Errorf("failed to burn DREAM for slashing: %w", err)
	}

	// Remove the stake
	if err := k.RemoveStakeFromTargetIndex(ctx, bond); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Debug("failed to remove bond from target index", "error", err)
	}
	if err := k.Stake.Remove(ctx, bond.Id); err != nil {
		return fmt.Errorf("failed to remove slashed bond: %w", err)
	}

	// Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"author_bond_slashed",
			sdk.NewAttribute("stake_id", fmt.Sprintf("%d", bond.Id)),
			sdk.NewAttribute("author", bond.Staker),
			sdk.NewAttribute("target_type", targetType.String()),
			sdk.NewAttribute("target_id", fmt.Sprintf("%d", targetID)),
			sdk.NewAttribute("amount_slashed", bond.Amount.String()),
		),
	)

	return nil
}

// RestoreAuthorBond is the inverse of SlashAuthorBond. Called by content
// modules when a moderation action is reversed (sentinel self-correct,
// council unhide, or appeal-OVERTURNED). Mints `amount` DREAM to `author`
// and re-locks it as a fresh author bond on the target, so the slash →
// restore round-trip is net-zero on supply.
//
// Idempotent against accidental double-restore: returns nil silently when
// an author bond already exists for the target. Validates the target type
// the same way CreateAuthorBond does. Mints first, then locks, so a lock
// failure leaves the freshly-minted DREAM in the author's free balance
// rather than vanishing.
func (k Keeper) RestoreAuthorBond(
	ctx context.Context,
	author sdk.AccAddress,
	targetType types.StakeTargetType,
	targetID uint64,
	amount math.Int,
) error {
	if !types.IsAuthorBondType(targetType) {
		return types.ErrNotAuthorBondType
	}
	if amount.IsNil() || amount.IsNegative() || amount.IsZero() {
		// Empty / zero amount means "nothing was slashed, nothing to restore".
		return nil
	}

	// Idempotency: if an author bond already exists for this target, skip.
	// This can happen if the author created a new bond manually after the
	// slash, or if a reversal callback fires twice for the same record.
	if existing, _ := k.GetStakesByTarget(ctx, targetType, targetID); len(existing) > 0 {
		return nil
	}

	// Mint fresh DREAM to the author's free balance to replace what was burned
	// by SlashAuthorBond. This is the supply-restoration step.
	if err := k.MintDREAM(ctx, author, amount); err != nil {
		return fmt.Errorf("restore author bond: mint DREAM: %w", err)
	}

	// Re-create the bond. Skips the MaxAuthorBondPerContent cap check since
	// the original bond by definition passed it; if the cap has since been
	// lowered the operator is choosing to honor pre-existing commitments.
	if err := k.LockDREAM(ctx, author, amount); err != nil {
		return fmt.Errorf("restore author bond: lock DREAM: %w", err)
	}

	stakeID, err := k.StakeSeq.Next(ctx)
	if err != nil {
		return fmt.Errorf("restore author bond: next stake id: %w", err)
	}

	stake := types.Stake{
		Id:               stakeID,
		Staker:           author.String(),
		TargetType:       targetType,
		TargetId:         targetID,
		TargetIdentifier: "",
		Amount:           amount,
		CreatedAt:        sdk.UnwrapSDKContext(ctx).BlockTime().Unix(),
		LastClaimedAt:    0,
		RewardDebt:       math.ZeroInt(),
	}

	if err := k.Stake.Set(ctx, stakeID, stake); err != nil {
		return fmt.Errorf("restore author bond: persist stake: %w", err)
	}
	if err := k.AddStakeToTargetIndex(ctx, stake); err != nil {
		return fmt.Errorf("restore author bond: index stake: %w", err)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"author_bond_restored",
			sdk.NewAttribute("stake_id", fmt.Sprintf("%d", stakeID)),
			sdk.NewAttribute("author", author.String()),
			sdk.NewAttribute("target_type", targetType.String()),
			sdk.NewAttribute("target_id", fmt.Sprintf("%d", targetID)),
			sdk.NewAttribute("amount", amount.String()),
		),
	)
	return nil
}
