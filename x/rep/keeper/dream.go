package keeper

import (
	"context"
	"fmt"
	"sort"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
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
// per epoch by decayStakesBatch (MaybeApplyBulkDecay), which shrinks the record,
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
		multiplier := one.Sub(unstakedRate).Power(commontypes.Saturate[uint64](elapsed))
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

// decayStakesBatch decays stake records with id >= from, reading at most
// budget records (0 = no limit), by `epochs` epochs of staked decay. It
// returns the id to resume at and whether the walk reached the last record.
// The per-member burn of the batch is settled before it returns, so a pass
// split across blocks leaves every member consistent between batches.
func (k Keeper) decayStakesBatch(ctx context.Context, from uint64, budget int, epochs uint64) (uint64, bool, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return from, false, err
	}
	stakedRate := params.StakedDecayRate
	if stakedRate.IsNil() || !stakedRate.IsPositive() {
		return from, true, nil
	}
	if epochs == 0 {
		epochs = 1
	}
	multiplier := math.LegacyOneDec().Sub(stakedRate).Power(epochs)

	currentEpoch, err := k.GetCurrentEpoch(ctx)
	if err != nil {
		return from, false, err
	}

	graceCache := make(map[string]bool)
	projectExempt := make(map[uint64]bool) // project id -> exempt because PROPOSED
	memberDecay := make(map[string]math.Int)
	decayedStakes := 0
	visited := 0
	next := from
	done := true

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	err = k.Stake.Walk(ctx, new(collections.Range[uint64]).StartInclusive(from), func(id uint64, stake types.Stake) (bool, error) {
		if budget > 0 && visited >= budget {
			done = false
			return true, nil
		}
		visited++
		next = id + 1
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
			types.StakeTargetType_STAKE_TARGET_BLOG_REPLY_CONTENT,
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

		// Stake records keep the address they were opened under; the member
		// that now carries it is found through the alias (REP-E-07), so a
		// migrated staker's decay is charged to the member that holds the lock.
		owner := k.Canonical(ctx, stake.Staker)
		inGrace, cached := graceCache[owner]
		if !cached {
			member, mErr := k.Member.Get(ctx, owner)
			if mErr == nil {
				inGrace = memberWithinDecayGrace(member, params, currentEpoch)
			}
			// A missing member record cannot be looked up for grace; decay the
			// stake anyway — its DREAM is locked regardless.
			graceCache[owner] = inGrace
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
		if cur, ok := memberDecay[owner]; ok {
			memberDecay[owner] = cur.Add(burned)
		} else {
			memberDecay[owner] = burned
		}
		decayedStakes++
		return false, nil
	})
	if err != nil {
		return from, false, err
	}

	// Sorted keys: map iteration order is random, and the writes below must be
	// the same on every node.
	stakers := make([]string, 0, len(memberDecay))
	for staker := range memberDecay {
		stakers = append(stakers, staker)
	}
	sort.Strings(stakers)

	totalBurned := math.ZeroInt()
	for _, staker := range stakers {
		burned := memberDecay[staker]
		member, mErr := k.Member.Get(ctx, staker)
		if mErr != nil {
			// The stake walk already decayed the stake and its pools; skipping
			// the member write only leaves a tiny aggregate surplus, which
			// UnlockDREAM tolerates in the staker's favour.
			sdkCtx.Logger().Error("failed to load member for stake decay burn", "staker", staker, "error", mErr)
			continue
		}
		// Floored at zero: an aggregate that is already short of its records
		// (a member whose DREAM was taken some other way) must never go
		// negative, which would corrupt `unlocked = balance - staked`
		// everywhere it is read. The burn charged is what was actually there.
		if burned.GT(*member.StakedDream) {
			burned = math.MaxInt(*member.StakedDream, math.ZeroInt())
		}
		if burned.GT(*member.DreamBalance) {
			burned = math.MaxInt(*member.DreamBalance, math.ZeroInt())
		}
		if !burned.IsPositive() {
			continue
		}
		*member.StakedDream = member.StakedDream.Sub(burned)
		*member.DreamBalance = member.DreamBalance.Sub(burned)
		*member.LifetimeBurned = member.LifetimeBurned.Add(burned)
		if err := k.Member.Set(ctx, staker, member); err != nil {
			return from, false, err
		}
		totalBurned = totalBurned.Add(burned)
	}

	// One counter write for the whole pass rather than one per staker: the
	// burn total is already accumulated above for the event.
	if totalBurned.IsPositive() {
		if err := k.TrackBurn(ctx, totalBurned); err != nil {
			return from, false, err
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
	return next, done, nil
}

// GetBalance returns the balance of a member, applying any pending decay first.
// It persists the updated member state to the store.
func (k Keeper) GetBalance(ctx context.Context, addr sdk.AccAddress) (math.Int, error) {
	// Value lookup: a stored (possibly tombstoned) address resolves to the
	// member that now carries it (alias.go).
	addr = k.resolveMember(ctx, addr)
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
	// Value lookup: a stored (possibly tombstoned) address resolves to the
	// member that now carries it (alias.go).
	addr = k.resolveMember(ctx, addr)
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
	// Value lookup: a stored (possibly tombstoned) address resolves to the
	// member that now carries it (alias.go).
	addr = k.resolveMember(ctx, addr)
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

// BurnDREAM burns DREAM tokens from a member's UNLOCKED balance.
// This updates the member's balance, lifetime burned tracking, and the
// per-season burn counter.
//
// Only the unlocked part of the balance can be burned (REP-E-03). Locked
// DREAM backs an escrow somewhere (a stake record, a challenge stake, an
// invitation, a bond), and a fee paid out of it left DreamBalance below
// StakedDream: the escrow's later slash then failed and wedged whatever was
// waiting on it. Slashing a specific escrow goes through BurnLockedDREAM; the
// cross-module "unlock, then burn" pairs keep working because the unlock
// makes the amount unlocked first.
//
// Season tracking lives here for the same reason TrackMint lives in MintDREAM:
// TrackBurn previously had exactly one call site, the treasury-overflow burn,
// so SeasonBurned reported one minor line as the chain's entire destruction —
// no slashing, no failed challenges or invitations, no creation fees, no bonds.
// This is the choke point every one of those goes through, including the
// cross-module burns from x/forum, x/collect, x/reveal, x/name and x/season.
//
// The paths that do NOT route through here track their own burns: decay
// (ApplyPendingDecay and decayStakesBatch), the transfer tax, zeroing, and the
// treasury overflow. SPARK burns — the bonded-role reward-pool overflows — are
// deliberately excluded; SeasonBurned counts DREAM.
func (k Keeper) BurnDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error {
	// Value lookup: a stored (possibly tombstoned) address resolves to the
	// member that now carries it (alias.go).
	addr = k.resolveMember(ctx, addr)
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
	if member.DreamBalance.Sub(DerefInt(member.StakedDream)).LT(amount) {
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

// BurnLockedDREAM slashes an escrow: it releases amount from the member's
// locked DREAM and burns it in one step. Every slash of a specific escrow
// (a rejected challenge stake, a slashed bond, a forfeited invitation stake)
// goes through here instead of an UnlockDREAM + BurnDREAM pair, so the burn
// can never be paid out of some other escrow's lock and the amount locked
// never exceeds the balance behind it.
//
// Fails with ErrInsufficientStake when less than amount is locked: the escrow
// is not there (the member was zeroed, which already burned it), and the
// caller decides whether that is fatal.
func (k Keeper) BurnLockedDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error {
	addr = k.resolveMember(ctx, addr)
	if amount.IsNil() || !amount.IsPositive() {
		return types.ErrInvalidAmount
	}

	member, err := k.Member.Get(ctx, addr.String())
	if err != nil {
		return types.ErrMemberNotFound
	}
	if err := k.ApplyPendingDecay(ctx, &member); err != nil {
		return err
	}
	if DerefInt(member.StakedDream).LT(amount) {
		return errorsmod.Wrapf(types.ErrInsufficientStake, "%s locked, %s to burn", DerefInt(member.StakedDream), amount)
	}
	if member.DreamBalance.LT(amount) {
		// Locked DREAM is a subset of the balance; this only trips on a
		// corrupted record, which must not be burned into negative.
		return errorsmod.Wrapf(types.ErrInsufficientBalance, "%s balance, %s to burn", member.DreamBalance, amount)
	}

	*member.StakedDream = member.StakedDream.Sub(amount)
	*member.DreamBalance = member.DreamBalance.Sub(amount)
	*member.LifetimeBurned = member.LifetimeBurned.Add(amount)
	if err := k.TrackBurn(ctx, amount); err != nil {
		return err
	}
	if err := k.Member.Set(ctx, addr.String(), member); err != nil {
		return err
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
		sdk.NewEvent(
			"burn_dream",
			sdk.NewAttribute("from", addr.String()),
			sdk.NewAttribute("amount", amount.String()),
			sdk.NewAttribute("locked", "true"),
		),
	)
	return nil
}

// LockDREAM locks DREAM tokens (moves from available balance to staked).
// Staked DREAM stops decaying at the unstaked rate; reward-bearing stakes
// instead decay at the lower StakedDecayRate via decayStakesBatch, which shrinks
// the stake records and this aggregate together.
func (k Keeper) LockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error {
	// Value lookup: a stored (possibly tombstoned) address resolves to the
	// member that now carries it (alias.go).
	addr = k.resolveMember(ctx, addr)
	if amount.IsNegative() || amount.IsZero() {
		return types.ErrInvalidAmount
	}

	member, err := k.Member.Get(ctx, addr.String())
	if err != nil {
		return types.ErrMemberNotFound
	}

	// A zeroed member never locks again. Zeroing burns every lock along with
	// the balance, and an escrow record another module still holds against the
	// member then unlocks against nothing (ErrInsufficientStake) instead of
	// against a fresh lock that backs something else (REP-E-06).
	if member.Status != types.MemberStatus_MEMBER_STATUS_ACTIVE {
		return types.ErrMemberNotActive
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
//
// Strict: when less than amount is locked it fails with ErrInsufficientStake
// and releases nothing. It used to cap the release at whatever was locked,
// which on a shortfall released other escrows' locks to pay this one. With
// per-stake decay the aggregate equals the sum of the escrows behind it, so a
// shortfall only means the escrow is gone (the member was zeroed).
func (k Keeper) UnlockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error {
	// Value lookup: a stored (possibly tombstoned) address resolves to the
	// member that now carries it (alias.go).
	addr = k.resolveMember(ctx, addr)
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

	if member.StakedDream.LT(amount) {
		return errorsmod.Wrapf(types.ErrInsufficientStake, "%s locked, %s to unlock", member.StakedDream, amount)
	}
	unlockAmount := amount

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

// TransferDREAM transfers DREAM between members, held to the limits of its
// purpose and taxed at transfer_tax_rate.
//
// Only TIP and GIFT exist. Every other value, including an undefined enum
// number on a hand-built transaction, is rejected: the old switch had no
// default, so any other purpose was an uncapped send. Bounties are escrowed
// against an initiative with MsgFundInitiativeBounty instead.
//
// All limit checks run before any state is written, so a refused transfer
// leaves no trace even when called from another module outside a tx.
func (k Keeper) TransferDREAM(ctx context.Context, sender, recipient sdk.AccAddress, amount math.Int, purpose types.TransferPurpose) error {
	// A transfer to a tombstone reaches the member that now carries it; a
	// tombstoned sender is refused by the membership gate below.
	recipient = k.resolveMember(ctx, recipient)
	if amount.IsNil() || !amount.IsPositive() {
		return types.ErrInvalidAmount
	}
	if purpose != types.TransferPurpose_TRANSFER_PURPOSE_TIP &&
		purpose != types.TransferPurpose_TRANSFER_PURPOSE_GIFT {
		return errorsmod.Wrapf(types.ErrInvalidTransferPurpose, "%d", int32(purpose))
	}

	if sender.Equals(recipient) {
		return types.ErrCannotTransferToSelf
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	senderMember, err := k.Member.Get(ctx, sender.String())
	if err != nil {
		return types.ErrMemberNotFound
	}
	recipientMember, err := k.Member.Get(ctx, recipient.String())
	if err != nil {
		return types.ErrMemberNotFound
	}

	if err := k.ApplyPendingDecay(ctx, &senderMember); err != nil {
		return err
	}
	if err := k.ApplyPendingDecay(ctx, &recipientMember); err != nil {
		return err
	}

	currentEpoch, err := k.GetCurrentEpoch(ctx)
	if err != nil {
		return err
	}

	tax := math.ZeroInt()
	if !params.TransferTaxRate.IsZero() {
		tax = math.LegacyNewDecFromInt(amount).Mul(params.TransferTaxRate).TruncateInt()
	}
	netAmount := amount.Sub(tax)

	giftKey := collections.Join(sender.String(), recipient.String())
	var giftTotal, giftKeyTotal math.Int

	switch purpose {
	case types.TransferPurpose_TRANSFER_PURPOSE_TIP:
		if amount.GT(params.MaxTipAmount) {
			return types.ErrExceedsMaxTipAmount
		}
		if senderMember.LastTipEpoch < currentEpoch || senderMember.TipsSentThisEpoch == nil {
			senderMember.TipsSentThisEpoch = PtrInt(math.ZeroInt())
			senderMember.LastTipEpoch = currentEpoch
		}
		sent := senderMember.TipsSentThisEpoch.Add(amount)
		if sent.GT(params.MaxTipsSentPerEpoch) {
			return errorsmod.Wrapf(types.ErrExceedsMaxTipsPerEpoch, "%s of %s already tipped this epoch",
				senderMember.TipsSentThisEpoch, params.MaxTipsSentPerEpoch)
		}
		// Tips count against the recipient's limits; gifts do not (each
		// member has one inviter, so the per-invitee lifetime cap bounds them).
		if err := k.chargeTransferReceived(ctx, params, &recipientMember, netAmount); err != nil {
			return err
		}
		senderMember.TipsSentThisEpoch = PtrInt(sent)

	case types.TransferPurpose_TRANSFER_PURPOSE_GIFT:
		// InvitedBy is never re-keyed, so a migrated inviter is matched
		// through the alias (REP-R-11); a tombstone never passes CanActAs.
		if params.GiftOnlyToInvitees && !k.CanActAs(ctx, sender.String(), recipientMember.InvitedBy) {
			return types.ErrGiftOnlyToInvitees
		}
		// The lifetime cap counts what the sender gave under every address
		// it ever had: records stay keyed by the address that sent them, so
		// reading only (sender, recipient) would reset the cap on migration.
		giftTotal, giftKeyTotal = math.ZeroInt(), math.ZeroInt()
		for _, from := range k.AllAddressesOf(ctx, sender.String()) {
			if rec, gErr := k.GiftRecord.Get(ctx, collections.Join(from, recipient.String())); gErr == nil {
				v := DerefInt(rec.TotalGifted)
				giftTotal = giftTotal.Add(v)
				if from == sender.String() {
					giftKeyTotal = v
				}
			}
		}
		giftTotal = giftTotal.Add(amount)
		giftKeyTotal = giftKeyTotal.Add(amount)
		if giftTotal.GT(params.MaxGiftPerInvitee) {
			return errorsmod.Wrapf(types.ErrExceedsGiftAllowance, "lifetime allowance is %s", params.MaxGiftPerInvitee)
		}
	}

	// Check sender has sufficient unlocked balance (total balance minus staked)
	unlockedBalance := senderMember.DreamBalance.Sub(*senderMember.StakedDream)
	if unlockedBalance.LT(amount) {
		return types.ErrInsufficientBalance
	}

	if purpose == types.TransferPurpose_TRANSFER_PURPOSE_GIFT {
		if err := k.GiftRecord.Set(ctx, giftKey, types.GiftRecord{
			Sender:      sender.String(),
			Recipient:   recipient.String(),
			TotalGifted: PtrInt(giftKeyTotal),
		}); err != nil {
			return err
		}
	}

	*senderMember.DreamBalance = senderMember.DreamBalance.Sub(amount)
	*recipientMember.DreamBalance = recipientMember.DreamBalance.Add(netAmount)

	if tax.IsPositive() {
		*senderMember.LifetimeBurned = senderMember.LifetimeBurned.Add(tax)
		if err := k.TrackBurn(ctx, tax); err != nil {
			return err
		}
	}

	if err := k.Member.Set(ctx, sender.String(), senderMember); err != nil {
		return err
	}
	if err := k.Member.Set(ctx, recipient.String(), recipientMember); err != nil {
		return err
	}

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

// TransferReceiveHeadroom is how much more DREAM a member may take in through
// tips and initiative bounty payouts before hitting either the epoch or the
// season limit.
//
// The limit sits on the recipient because that is the side a buyer cannot
// multiply: a seller can only send their own DREAM, but a buyer can collect
// from any number of sellers.
func (k Keeper) TransferReceiveHeadroom(ctx context.Context, params types.Params, m *types.Member) (math.Int, error) {
	if err := k.rollTransferReceivedWindows(ctx, m); err != nil {
		return math.ZeroInt(), err
	}
	epochLeft := params.MaxTransferReceivedPerEpoch.Sub(DerefInt(m.TransferReceivedThisEpoch))
	seasonLeft := params.MaxTransferReceivedPerSeason.Sub(DerefInt(m.TransferReceivedThisSeason))
	left := math.MinInt(epochLeft, seasonLeft)
	if left.IsNegative() {
		return math.ZeroInt(), nil
	}
	return left, nil
}

// chargeTransferReceived counts amount against the member's recipient-side
// limits, failing whole rather than admitting part of it. The caller persists
// the member.
func (k Keeper) chargeTransferReceived(ctx context.Context, params types.Params, m *types.Member, amount math.Int) error {
	left, err := k.TransferReceiveHeadroom(ctx, params, m)
	if err != nil {
		return err
	}
	if amount.GT(left) {
		return errorsmod.Wrapf(types.ErrExceedsTransferReceiveLimit,
			"%s can receive %s more this epoch and season, not %s", m.Address, left, amount)
	}
	m.TransferReceivedThisEpoch = PtrInt(DerefInt(m.TransferReceivedThisEpoch).Add(amount))
	m.TransferReceivedThisSeason = PtrInt(DerefInt(m.TransferReceivedThisSeason).Add(amount))
	return nil
}

// rollTransferReceivedWindows resets the epoch and season receive counters
// when their window has moved on.
func (k Keeper) rollTransferReceivedWindows(ctx context.Context, m *types.Member) error {
	epoch, err := k.GetCurrentEpoch(ctx)
	if err != nil {
		return err
	}
	season, err := k.GetCurrentSeason(ctx)
	if err != nil {
		return err
	}
	if m.LastTransferReceivedEpoch != epoch || m.TransferReceivedThisEpoch == nil {
		m.TransferReceivedThisEpoch = PtrInt(math.ZeroInt())
		m.LastTransferReceivedEpoch = epoch
	}
	if m.LastTransferReceivedSeason != season || m.TransferReceivedThisSeason == nil {
		m.TransferReceivedThisSeason = PtrInt(math.ZeroInt())
		m.LastTransferReceivedSeason = season
	}
	return nil
}
