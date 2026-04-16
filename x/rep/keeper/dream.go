package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GetCurrentEpoch calculates the current epoch based on block height and params
func (k Keeper) GetCurrentEpoch(ctx context.Context) (int64, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0, err
	}
	if params.EpochBlocks <= 0 {
		return 0, nil // Avoid division by zero
	}
	return sdkCtx.BlockHeight() / params.EpochBlocks, nil
}

// ApplyPendingDecay calculates and applies decay to a member's balance.
// Both unstaked (0.2%/epoch) and staked (0.05%/epoch) DREAM decay.
// New members within the grace period are exempt from all decay.
// It updates the member struct in-place but does not save to store (caller must save).
func (k Keeper) ApplyPendingDecay(ctx context.Context, member *types.Member) error {
	currentEpoch, err := k.GetCurrentEpoch(ctx)
	if err != nil {
		return err
	}

	if member.LastDecayEpoch >= currentEpoch {
		return nil
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	elapsed := currentEpoch - member.LastDecayEpoch
	if elapsed <= 0 {
		return nil
	}

	// Cap elapsed epochs to avoid gas-expensive Power() calls.
	// After 500 epochs of decay the remaining value is negligible
	// (e.g., 0.998^500 ≈ 0.37 for staked, 0.998^500 for unstaked).
	const maxDecayEpochs int64 = 500
	if elapsed > maxDecayEpochs {
		elapsed = maxDecayEpochs
	}

	// Grace period: new members exempt from all decay
	if params.NewMemberDecayGraceEpochs > 0 {
		// Calculate member's join epoch from JoinedAt timestamp
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		memberAge := currentEpoch
		if member.JoinedAt > 0 && params.EpochBlocks > 0 {
			joinedBlock := member.JoinedAt // stored as block height
			joinEpoch := joinedBlock / params.EpochBlocks
			// But JoinedAt is actually a unix timestamp, convert via block time estimate
			// Use a simpler approach: check if member has been around long enough
			blockHeight := sdkCtx.BlockHeight()
			blocksPerEpoch := params.EpochBlocks
			if blocksPerEpoch > 0 {
				currentEpochNum := blockHeight / blocksPerEpoch
				joinEpoch = member.JoinedAt / blocksPerEpoch
				memberAge = currentEpochNum - joinEpoch
			}
		}
		if memberAge < params.NewMemberDecayGraceEpochs {
			member.LastDecayEpoch = currentEpoch
			return nil
		}
	}

	one := math.LegacyOneDec()

	// 1. Unstaked decay: balance * (1 - unstaked_rate)^elapsed
	unstakedRate := params.UnstakedDecayRate
	unstaked := member.DreamBalance.Sub(*member.StakedDream)
	if unstaked.IsPositive() && unstakedRate.IsPositive() {
		multiplier := one.Sub(unstakedRate).Power(uint64(elapsed))
		newUnstaked := math.LegacyNewDecFromInt(unstaked).Mul(multiplier).TruncateInt()
		decayAmount := unstaked.Sub(newUnstaked)
		if decayAmount.IsPositive() {
			*member.DreamBalance = member.DreamBalance.Sub(decayAmount)
			*member.LifetimeBurned = member.LifetimeBurned.Add(decayAmount)
		}
	}

	// 2. Staked decay: staked * (1 - staked_rate)^elapsed
	stakedRate := params.StakedDecayRate
	staked := *member.StakedDream
	if staked.IsPositive() && stakedRate.IsPositive() {
		multiplier := one.Sub(stakedRate).Power(uint64(elapsed))
		newStaked := math.LegacyNewDecFromInt(staked).Mul(multiplier).TruncateInt()
		stakedDecayAmount := staked.Sub(newStaked)
		if stakedDecayAmount.IsPositive() {
			*member.StakedDream = member.StakedDream.Sub(stakedDecayAmount)
			*member.DreamBalance = member.DreamBalance.Sub(stakedDecayAmount)
			*member.LifetimeBurned = member.LifetimeBurned.Add(stakedDecayAmount)
		}
	}

	member.LastDecayEpoch = currentEpoch
	return nil
}

// GetBalance returns the balance of a member, applying any pending decay first.
// It persists the updated member state to the store.
func (k Keeper) GetBalance(ctx context.Context, addr sdk.AccAddress) (math.Int, error) {
	member, err := k.Member.Get(ctx, addr.String())
	if err != nil {
		// Member not found, return 0
		return math.NewInt(0), nil
	}

	// Apply decay
	if err := k.ApplyPendingDecay(ctx, &member); err != nil {
		return math.NewInt(0), err
	}

	// Persist update
	if err := k.Member.Set(ctx, addr.String(), member); err != nil {
		return math.NewInt(0), err
	}

	return *member.DreamBalance, nil
}

// referralMintingKey is a context key used to prevent recursive referral reward minting.
// When MintDREAM is called for a referral reward, this flag is set so that the
// nested MintDREAM call does not trigger another referral reward calculation.
type referralMintingKeyType struct{}

var referralMintingKey = referralMintingKeyType{}

// MintDREAM mints DREAM tokens to a member's balance.
// This updates the member's balance and lifetime earned tracking.
// The member must already exist in the system.
func (k Keeper) MintDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error {
	if amount.IsNegative() || amount.IsZero() {
		return types.ErrInvalidAmount
	}

	member, err := k.Member.Get(ctx, addr.String())
	if err != nil {
		return types.ErrMemberNotFound
	}

	// Apply pending decay before modifying balance
	if err := k.ApplyPendingDecay(ctx, &member); err != nil {
		return err
	}

	// Mint the tokens
	*member.DreamBalance = member.DreamBalance.Add(amount)
	*member.LifetimeEarned = member.LifetimeEarned.Add(amount)

	// Save updated member
	if err := k.Member.Set(ctx, addr.String(), member); err != nil {
		return err
	}

	// Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"mint_dream",
			sdk.NewAttribute("recipient", addr.String()),
			sdk.NewAttribute("amount", amount.String()),
		),
	)

	// Calculate referral reward for inviter (if applicable)
	// Skip if we are already inside a referral reward mint (reentrancy guard)
	if sdkCtx.Value(referralMintingKey) == nil {
		// Set the reentrancy guard before calling CalculateReferralReward
		guardedCtx := sdkCtx.WithValue(referralMintingKey, true)
		if err := k.CalculateReferralReward(guardedCtx, addr, amount); err != nil {
			sdkCtx.Logger().Error("failed to calculate referral reward",
				"error", err,
				"recipient", addr.String(),
				"amount", amount.String())
		}
	}

	return nil
}

// BurnDREAM burns DREAM tokens from a member's balance.
// This updates the member's balance and lifetime burned tracking.
func (k Keeper) BurnDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error {
	if amount.IsNegative() || amount.IsZero() {
		return types.ErrInvalidAmount
	}

	member, err := k.Member.Get(ctx, addr.String())
	if err != nil {
		return types.ErrMemberNotFound
	}

	// Apply pending decay before checking balance
	if err := k.ApplyPendingDecay(ctx, &member); err != nil {
		return err
	}

	// Check sufficient balance
	if member.DreamBalance.LT(amount) {
		return types.ErrInsufficientBalance
	}

	// Burn the tokens
	*member.DreamBalance = member.DreamBalance.Sub(amount)
	*member.LifetimeBurned = member.LifetimeBurned.Add(amount)

	// Save updated member
	if err := k.Member.Set(ctx, addr.String(), member); err != nil {
		return err
	}

	// Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"burn_dream",
			sdk.NewAttribute("from", addr.String()),
			sdk.NewAttribute("amount", amount.String()),
		),
	)

	return nil
}

// LockDREAM locks DREAM tokens (moves from available balance to staked).
// Locked tokens do not decay and earn staking rewards.
func (k Keeper) LockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error {
	if amount.IsNegative() || amount.IsZero() {
		return types.ErrInvalidAmount
	}

	member, err := k.Member.Get(ctx, addr.String())
	if err != nil {
		return types.ErrMemberNotFound
	}

	// Apply pending decay before checking balance
	if err := k.ApplyPendingDecay(ctx, &member); err != nil {
		return err
	}

	// Check sufficient unlocked balance
	unlockedBalance := member.DreamBalance.Sub(*member.StakedDream)
	if unlockedBalance.LT(amount) {
		return types.ErrInsufficientBalance
	}

	// Lock the tokens
	*member.StakedDream = member.StakedDream.Add(amount)

	// Save updated member
	if err := k.Member.Set(ctx, addr.String(), member); err != nil {
		return err
	}

	// Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"lock_dream",
			sdk.NewAttribute("address", addr.String()),
			sdk.NewAttribute("amount", amount.String()),
		),
	)

	return nil
}

// UnlockDREAM unlocks DREAM tokens (moves from staked to available balance).
// Unlocked tokens will begin decaying if not re-staked.
func (k Keeper) UnlockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error {
	if amount.IsNegative() || amount.IsZero() {
		return types.ErrInvalidAmount
	}

	member, err := k.Member.Get(ctx, addr.String())
	if err != nil {
		return types.ErrMemberNotFound
	}

	// Apply pending decay
	if err := k.ApplyPendingDecay(ctx, &member); err != nil {
		return err
	}

	// Check sufficient staked balance.
	// Staked DREAM may have decayed slightly below the originally locked amount.
	// Cap the unlock to the actual staked balance to avoid failures from rounding.
	unlockAmount := amount
	if member.StakedDream.LT(amount) {
		if member.StakedDream.IsZero() {
			return types.ErrInsufficientStake
		}
		unlockAmount = *member.StakedDream
	}

	// Unlock the tokens
	*member.StakedDream = member.StakedDream.Sub(unlockAmount)

	// Save updated member
	if err := k.Member.Set(ctx, addr.String(), member); err != nil {
		return err
	}

	// Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"unlock_dream",
			sdk.NewAttribute("address", addr.String()),
			sdk.NewAttribute("amount", unlockAmount.String()),
		),
	)

	return nil
}

// TransferDREAM transfers DREAM tokens between members with purpose validation and tax
func (k Keeper) TransferDREAM(ctx context.Context, sender, recipient sdk.AccAddress, amount math.Int, purpose types.TransferPurpose) error {
	if amount.IsNegative() || amount.IsZero() {
		return types.ErrInvalidAmount
	}

	if sender.Equals(recipient) {
		return types.ErrCannotTransferToSelf
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	// Get sender member
	senderMember, err := k.Member.Get(ctx, sender.String())
	if err != nil {
		return types.ErrMemberNotFound
	}

	// Get recipient member
	recipientMember, err := k.Member.Get(ctx, recipient.String())
	if err != nil {
		return types.ErrMemberNotFound
	}

	// Apply decay to both members
	if err := k.ApplyPendingDecay(ctx, &senderMember); err != nil {
		return err
	}
	if err := k.ApplyPendingDecay(ctx, &recipientMember); err != nil {
		return err
	}

	// Check purpose limits
	currentEpoch, err := k.GetCurrentEpoch(ctx)
	if err != nil {
		return err
	}

	switch purpose {
	case types.TransferPurpose_TRANSFER_PURPOSE_TIP:
		if amount.GT(params.MaxTipAmount) {
			return types.ErrExceedsMaxTipAmount
		}

		// Reset tip counter if new epoch
		if senderMember.LastTipEpoch < currentEpoch {
			senderMember.TipsGivenThisEpoch = 0
			senderMember.LastTipEpoch = currentEpoch
		}

		if senderMember.TipsGivenThisEpoch >= params.MaxTipsPerEpoch {
			return types.ErrExceedsMaxTipsPerEpoch
		}

		senderMember.TipsGivenThisEpoch++

	case types.TransferPurpose_TRANSFER_PURPOSE_GIFT:
		if amount.GT(params.MaxGiftAmount) {
			return types.ErrExceedsMaxGiftAmount
		}

		if params.GiftOnlyToInvitees {
			if recipientMember.InvitedBy != sender.String() {
				return types.ErrGiftOnlyToInvitees
			}
		}

		// Check per-recipient cooldown
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		currentBlock := sdkCtx.BlockHeight()
		giftKey := collections.Join(sender.String(), recipient.String())

		existingRecord, err := k.GiftRecord.Get(ctx, giftKey)
		if err == nil {
			// Record exists, check cooldown
			blocksSinceLastGift := currentBlock - existingRecord.LastGiftBlock
			if blocksSinceLastGift < params.GiftCooldownBlocks {
				return types.ErrGiftCooldownNotMet
			}
		}
		// If no record exists (err != nil), this is the first gift to this recipient

		// Initialize GiftsSentThisEpoch if nil (for members created before this field was added)
		if senderMember.GiftsSentThisEpoch == nil {
			senderMember.GiftsSentThisEpoch = new(math.Int)
			*senderMember.GiftsSentThisEpoch = math.NewInt(0)
		}

		// Check and update per-sender epoch limit
		if senderMember.LastGiftEpoch < currentEpoch {
			// New epoch, reset counter
			*senderMember.GiftsSentThisEpoch = math.NewInt(0)
			senderMember.LastGiftEpoch = currentEpoch
		}

		newTotal := senderMember.GiftsSentThisEpoch.Add(amount)
		if newTotal.GT(params.MaxGiftsPerSenderEpoch) {
			return types.ErrExceedsEpochGiftLimit
		}

		// Update sender's epoch gift counter (will be saved later with member)
		*senderMember.GiftsSentThisEpoch = newTotal

		// Update gift record for cooldown tracking
		giftRecord := types.GiftRecord{
			Sender:        sender.String(),
			Recipient:     recipient.String(),
			LastGiftBlock: currentBlock,
		}
		if err := k.GiftRecord.Set(ctx, giftKey, giftRecord); err != nil {
			return err
		}
	}

	// Check sender has sufficient unlocked balance (total balance minus staked)
	unlockedBalance := senderMember.DreamBalance.Sub(*senderMember.StakedDream)
	if unlockedBalance.LT(amount) {
		return types.ErrInsufficientBalance
	}

	// Calculate tax
	tax := math.NewInt(0)
	if !params.TransferTaxRate.IsZero() {
		taxDec := math.LegacyNewDecFromInt(amount).Mul(params.TransferTaxRate)
		tax = taxDec.TruncateInt()
	}

	netAmount := amount.Sub(tax)

	// Execute transfer
	*senderMember.DreamBalance = senderMember.DreamBalance.Sub(amount)
	*recipientMember.DreamBalance = recipientMember.DreamBalance.Add(netAmount)

	// Track burned tax
	if tax.IsPositive() {
		*senderMember.LifetimeBurned = senderMember.LifetimeBurned.Add(tax)
	}

	// Save both members
	if err := k.Member.Set(ctx, sender.String(), senderMember); err != nil {
		return err
	}
	if err := k.Member.Set(ctx, recipient.String(), recipientMember); err != nil {
		return err
	}

	// Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"transfer_dream",
			sdk.NewAttribute("sender", sender.String()),
			sdk.NewAttribute("recipient", recipient.String()),
			sdk.NewAttribute("amount", amount.String()),
			sdk.NewAttribute("tax", tax.String()),
			sdk.NewAttribute("purpose", purpose.String()),
		),
	)

	return nil
}
