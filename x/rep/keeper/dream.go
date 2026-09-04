package keeper

import (
	"context"
	"fmt"

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

// ApplyPendingDecay calculates and applies decay to a member's UNSTAKED
// balance (0.2%/epoch). Staked decay is NOT applied here: it burns the
// member.StakedDream aggregate while leaving every obligation that backs it
// (stake records, invitation locks, challenge stakes, role bonds) at face
// value, so the aggregate drifts below the sum of its claims and
// UnlockDREAM's cap-to-actual shortfall lands entirely on whoever unlocks
// last. Instead, staked decay is applied to the stake records themselves once
// per epoch by decayStakes (MaybeApplyBulkDecay), which shrinks the record,
// its pool denominators, its reward debt, and the member aggregate in
// lockstep. Obligations that are not stake records — invitations, challenges,
// bonds, bounties — do not decay at all; escrow should not erode.
//
// New members within the grace period are exempt from decay. This updates the
// member struct in-place but does not save to store (caller must save).
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
	// (e.g., 0.998^500 ≈ 0.37 for unstaked, 0.998^500 for unstaked).
	const maxDecayEpochs int64 = 500
	if elapsed > maxDecayEpochs {
		elapsed = maxDecayEpochs
	}

	// Grace period: new members exempt from all decay
	if memberWithinDecayGrace(*member, params, currentEpoch) {
		member.LastDecayEpoch = currentEpoch
		return nil
	}

	one := math.LegacyOneDec()

	// Unstaked decay: balance * (1 - unstaked_rate)^elapsed
	unstakedRate := params.UnstakedDecayRate
	unstaked := member.DreamBalance.Sub(*member.StakedDream)
	if unstaked.IsPositive() && unstakedRate.IsPositive() {
		multiplier := one.Sub(unstakedRate).Power(uint64(elapsed))
		newUnstaked := math.LegacyNewDecFromInt(unstaked).Mul(multiplier).TruncateInt()
		decayAmount := unstaked.Sub(newUnstaked)
		if decayAmount.IsPositive() {
			*member.DreamBalance = member.DreamBalance.Sub(decayAmount)
			*member.LifetimeBurned = member.LifetimeBurned.Add(decayAmount)
			// Decay does not route through BurnDREAM, so it counts its own
			// burn. Tracked here rather than in the bulk walker because
			// ApplyPendingDecay is also called lazily from write paths.
			if err := k.TrackBurn(ctx, decayAmount); err != nil {
				return err
			}
		}
	}

	member.LastDecayEpoch = currentEpoch
	return nil
}

// memberWithinDecayGrace reports whether the member is still inside the
// NewMemberDecayGraceEpochs window in which no decay applies.
//
// The window is measured from JoinedAtHeight — the height-domain twin of the
// JoinedAt timestamp — so the member's age is an honest epoch count. Members
// restored from state written before that field existed carry
// JoinedAtHeight = 0, which reads as "joined at genesis": they exit grace
// after NewMemberDecayGraceEpochs epochs, exactly the behaviour
// genesis-seeded members have always had. (The previous logic divided the
// JoinedAt unix timestamp by EpochBlocks as though it were a block height;
// for invited members that produced an astronomically large join epoch, a
// deeply negative member age, and a grace window that never expired.)
func memberWithinDecayGrace(member types.Member, params types.Params, currentEpoch int64) bool {
	if params.NewMemberDecayGraceEpochs <= 0 || params.EpochBlocks <= 0 {
		return false
	}
	joinEpoch := member.JoinedAtHeight / params.EpochBlocks
	return currentEpoch-joinEpoch < params.NewMemberDecayGraceEpochs
}

// decayStakes applies one epoch of staked decay to every reward-bearing stake
// record (initiative, project, member, tag), shrinking each stake's Amount by
// StakedDecayRate and moving every dependent ledger in lockstep:
//
//   - the stake's reward_debt is scaled with the principal, so a decaying
//     stake's pending claim shrinks proportionally instead of clamping to zero;
//   - the pool denominators (seasonal pool, per-project info, member/tag
//     pools) drop by the same amount via updateStakePoolTotals, keeping the
//     reward math's share weights equal to the economic stake;
//   - the staker's member aggregates (StakedDream, DreamBalance,
//     LifetimeBurned) absorb the burn, so the aggregate always equals the sum
//     of the obligations backing it and any unlock can be paid in full.
//
// Author bonds keep their face value: slashable escrow cannot erode. Content
// conviction stakes do NOT — they were exempt on the stated grounds that their
// conviction "already time-decays through the conviction half-life", which is
// not true of either conviction formula (both ramp time_factor to a cap of 1.0
// and hold it), and the exemption made them a costless shelter. Non-stake
// obligations (invitation locks, challenge stakes, bonded roles, bounties)
// never had their own decay and are unaffected.
//
// Runs once per epoch from MaybeApplyBulkDecay, after the member unstaked
// decay pass. Cost: O(all stake records) per epoch — every record is walked
// even where skipped (author bonds, PROPOSED-project stakes) — the same order
// the member walk already accepts, and amortized across the epoch's blocks.
//
// Terminal-project stakes decay with everything else: staked decay is the cost
// of holding a staked position, not merely a brake on reward compounding, and
// exempting them would make them a decay-free store of value dominating both
// unstaked DREAM (0.2%/epoch) and live stakes as a place to park DREAM. Their
// principal is freely withdrawable, so decay is simply the nudge to withdraw.
// PROPOSED-project stakes are the one exemption, and are bounded rather than
// permanent — see the case body below.
//
// Conviction is deliberately NOT refreshed per stake here: the shrink is
// 0.05%/epoch of the principal, and the conviction queue recomputes on its own
// schedule, so the drift self-corrects without an O(stakes) conviction pass.
func (k Keeper) decayStakes(ctx context.Context) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	stakedRate := params.StakedDecayRate
	if stakedRate.IsNil() || !stakedRate.IsPositive() {
		return nil
	}
	multiplier := math.LegacyOneDec().Sub(stakedRate)

	currentEpoch, err := k.GetCurrentEpoch(ctx)
	if err != nil {
		return err
	}

	graceCache := make(map[string]bool)
	projectExempt := make(map[uint64]bool) // project id -> exempt because PROPOSED
	memberDecay := make(map[string]math.Int)
	decayedStakes := 0

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	err = k.Stake.Walk(ctx, nil, func(id uint64, stake types.Stake) (bool, error) {
		switch stake.TargetType {
		case types.StakeTargetType_STAKE_TARGET_INITIATIVE,
			types.StakeTargetType_STAKE_TARGET_MEMBER,
			types.StakeTargetType_STAKE_TARGET_TAG:

		case types.StakeTargetType_STAKE_TARGET_PROJECT:
			// A stake on a PROPOSED project is frozen out of the seasonal pool
			// by stakeAccruing, so decaying it is a pure charge on backing work
			// before it is approved — the earliest and least certain moment to
			// commit, and precisely the behaviour conviction exists to buy.
			// The window is bounded: approval starts accrual (and rebases the
			// debt), rejection ends the stake. Terminal projects keep decaying;
			// their principal is freely withdrawable and decay is the nudge to
			// withdraw it.
			exempt, cached := projectExempt[stake.TargetId]
			if !cached {
				project, pErr := k.GetProject(ctx, stake.TargetId)
				exempt = pErr == nil && project.Status == types.ProjectStatus_PROJECT_STATUS_PROPOSED
				projectExempt[stake.TargetId] = exempt
			}
			if exempt {
				return false, nil
			}

		case types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT,
			types.StakeTargetType_STAKE_TARGET_FORUM_CONTENT,
			types.StakeTargetType_STAKE_TARGET_COLLECTION_CONTENT:
			// Content conviction decays with everything else. It was exempt on
			// the stated grounds that "content conviction already time-decays
			// via the conviction half-life" — it does not: both
			// CalculateContentConviction and CalculateRawStakeConviction ramp
			// time_factor linearly to a cap of 1.0 and hold it there, so
			// neither is a half-life despite the parameter names. Content
			// stakes are locked (so exempt from unstaked decay), earn no DREAM,
			// and propagate conviction into initiative conviction, which made
			// them a costless shelter strictly better than holding: 0%/epoch
			// against 0.2% for unstaked DREAM, with a governance benefit
			// attached. Decaying the principal also makes content conviction
			// genuinely erode over time, since conviction is amount *
			// time_factor and only the amount can carry the decay.
			//
			// updateStakePoolTotals routes these to adjustMemberContentStaked,
			// so the per-member content aggregate moves in lockstep.

		default:
			// Author bonds keep face value: slashable escrow cannot erode.
			return false, nil
		}
		if stake.Amount.IsNil() || !stake.Amount.IsPositive() {
			return false, nil
		}

		inGrace, cached := graceCache[stake.Staker]
		if !cached {
			member, mErr := k.Member.Get(ctx, stake.Staker)
			if mErr == nil {
				inGrace = memberWithinDecayGrace(member, params, currentEpoch)
			}
			// A missing member record cannot be looked up for grace; decay the
			// stake anyway — its DREAM is locked regardless.
			graceCache[stake.Staker] = inGrace
		}
		if inGrace {
			return false, nil
		}

		newAmount := math.LegacyNewDecFromInt(stake.Amount).Mul(multiplier).TruncateInt()
		if !newAmount.IsPositive() {
			// Sub-dust stake: one epoch of decay would round away the whole
			// principal. Leave it — the amount is worth less than the write.
			return false, nil
		}
		burned := stake.Amount.Sub(newAmount)
		if !burned.IsPositive() {
			return false, nil
		}

		stake.RewardDebt = scaleRewardDebt(stakeRewardDebt(stake), stake.Amount, newAmount)
		stake.Amount = newAmount
		if err := k.Stake.Set(ctx, id, stake); err != nil {
			return true, err
		}
		if err := k.updateStakePoolTotals(ctx, stake, burned.Neg()); err != nil {
			return true, err
		}
		if cur, ok := memberDecay[stake.Staker]; ok {
			memberDecay[stake.Staker] = cur.Add(burned)
		} else {
			memberDecay[stake.Staker] = burned
		}
		decayedStakes++
		return false, nil
	})
	if err != nil {
		return err
	}

	totalBurned := math.ZeroInt()
	for staker, burned := range memberDecay {
		member, mErr := k.Member.Get(ctx, staker)
		if mErr != nil {
			// The stake walk already decayed the stake and its pools; skipping
			// the member write only leaves a tiny aggregate surplus, which
			// UnlockDREAM tolerates in the staker's favour.
			sdkCtx.Logger().Error("failed to load member for stake decay burn", "staker", staker, "error", mErr)
			continue
		}
		*member.StakedDream = member.StakedDream.Sub(burned)
		*member.DreamBalance = member.DreamBalance.Sub(burned)
		*member.LifetimeBurned = member.LifetimeBurned.Add(burned)
		if err := k.Member.Set(ctx, staker, member); err != nil {
			return err
		}
		totalBurned = totalBurned.Add(burned)
	}

	// One counter write for the whole pass rather than one per staker: the
	// burn total is already accumulated above for the event.
	if totalBurned.IsPositive() {
		if err := k.TrackBurn(ctx, totalBurned); err != nil {
			return err
		}
	}

	if decayedStakes > 0 {
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				"stake_decay_applied",
				sdk.NewAttribute("epoch", fmt.Sprintf("%d", currentEpoch)),
				sdk.NewAttribute("stakes_decayed", fmt.Sprintf("%d", decayedStakes)),
				sdk.NewAttribute("total_burned", totalBurned.String()),
			),
		)
	}
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

	// Enforce the global per-epoch DREAM mint ceiling. Referral cascades count
	// against the same budget because the guard commits on the outer mint too.
	if err := k.CheckAndTrackEpochMint(ctx, amount); err != nil {
		return err
	}

	// Advance the season mint counter. Tracking used to live only in
	// MintToTreasury, so SeasonMinted counted the protocol's 10% treasury share
	// and nothing else — not completer payouts, referral rewards, jury or
	// review fees. Every consumer of that counter was reading a tenth of one
	// revenue line as if it were the chain's whole monetary output;
	// InitSeasonalPool now sizes a season's staking budget from it, so the
	// figure has to be the real one. MintToTreasury credits the treasury ledger
	// without routing through here, so it keeps its own TrackMint call and
	// nothing is counted twice.
	if err := k.TrackMint(ctx, amount); err != nil {
		return err
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

// CreditDREAM credits previously-minted DREAM to a member's balance without
// passing through the per-epoch mint cap or the referral-reward cascade. It
// is used to move DREAM out of the module treasury (which was counted
// against the mint cap at MintToTreasury time) into a recipient account —
// e.g. when TreasuryFundsInterims drains the treasury for an interim
// payout. The lifetime-earned counter is incremented so members still see
// the income in their history.
func (k Keeper) CreditDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error {
	if amount.IsNegative() || amount.IsZero() {
		return types.ErrInvalidAmount
	}

	member, err := k.Member.Get(ctx, addr.String())
	if err != nil {
		return types.ErrMemberNotFound
	}

	if err := k.ApplyPendingDecay(ctx, &member); err != nil {
		return err
	}

	*member.DreamBalance = member.DreamBalance.Add(amount)
	*member.LifetimeEarned = member.LifetimeEarned.Add(amount)

	if err := k.Member.Set(ctx, addr.String(), member); err != nil {
		return err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"credit_dream",
			sdk.NewAttribute("recipient", addr.String()),
			sdk.NewAttribute("amount", amount.String()),
			sdk.NewAttribute("source", "treasury"),
		),
	)

	return nil
}

// BurnDREAM burns DREAM tokens from a member's balance.
// This updates the member's balance, lifetime burned tracking, and the
// per-season burn counter.
//
// Season tracking lives here for the same reason TrackMint lives in MintDREAM:
// TrackBurn previously had exactly one call site, the treasury-overflow burn,
// so SeasonBurned reported one minor line as the chain's entire destruction —
// no slashing, no failed challenges or invitations, no creation fees, no bonds.
// This is the choke point every one of those goes through, including the
// cross-module burns from x/forum, x/collect, x/reveal, x/name and x/season.
//
// The paths that do NOT route through here track their own burns: decay
// (ApplyPendingDecay and decayStakes), the transfer tax, zeroing, and the
// treasury overflow. SPARK burns — the bonded-role reward-pool overflows — are
// deliberately excluded; SeasonBurned counts DREAM.
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

	if err := k.TrackBurn(ctx, amount); err != nil {
		return err
	}

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
// Staked DREAM stops decaying at the unstaked rate; reward-bearing stakes
// instead decay at the lower StakedDecayRate via decayStakes, which shrinks
// the stake records and this aggregate together.
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
// Unlocked tokens begin decaying at the unstaked rate if not re-staked.
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

	// Check sufficient staked balance. With per-stake decay the aggregate
	// tracks the sum of the underlying obligations exactly, so a full unlock
	// of any one obligation succeeds; the cap below is defense against
	// pre-fix aggregate drift, where staked DREAM had decayed below the
	// originally locked amount.
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
		if err := k.TrackBurn(ctx, tax); err != nil {
			return err
		}
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
