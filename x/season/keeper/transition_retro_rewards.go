package keeper

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"

	commontypes "sparkdream/x/common/types"
	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/season/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// processRetroRewardsPhase distributes retroactive rewards to top nominations.
// This runs as a transition phase before the existing snapshot phase.
//
// SEA-04: the phase is batched. Each block refreshes the final conviction of
// up to batchSize of this season's nominations (walking the season index and
// each nomination's own stakes by prefix) and stores it on the nomination.
// The block that exhausts the season's nominations pays out from the stored
// convictions, so the payout reads each nomination once and no stakes.
//
// SEA-01: nominations already marked Rewarded are never paid again, and
// MsgAbortSeasonTransition is refused once this phase has paid out.
func (k Keeper) processRetroRewardsPhase(ctx context.Context, state *types.SeasonTransitionState, batchSize int) (bool, error) {
	season, err := k.Season.Get(ctx)
	if err != nil {
		return false, err
	}

	var after uint64
	if state.LastProcessed != "" {
		after, err = strconv.ParseUint(state.LastProcessed, 10, 64)
		if err != nil {
			return false, fmt.Errorf("invalid retro cursor %q: %w", state.LastProcessed, err)
		}
	}

	processed := 0
	var lastID uint64
	err = k.walkSeasonNominations(ctx, season.Number, after, func(nom types.Nomination) (bool, error) {
		if processed >= batchSize {
			return true, nil
		}
		conviction, err := k.CalculateNominationConviction(ctx, nom)
		if err != nil {
			return true, err
		}
		nom.Conviction = conviction
		if err := k.SetNomination(ctx, nom); err != nil {
			return true, err
		}
		lastID = nom.Id
		processed++
		return false, nil
	})
	if err != nil {
		return false, err
	}
	state.ProcessedCount += commontypes.Saturate[uint64](processed)
	if processed >= batchSize {
		// More may remain; resume after lastID next block.
		state.LastProcessed = strconv.FormatUint(lastID, 10)
		return false, nil
	}

	if err := k.payRetroRewards(ctx, season); err != nil {
		return false, err
	}
	return true, nil
}

// payRetroRewards pays the season's retro-PGF budget across the top
// nominations by the convictions stored in the batched pass.
func (k Keeper) payRetroRewards(ctx context.Context, season types.Season) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	// Collect this season's unpaid nominations
	type nominationWithConviction struct {
		nomination types.Nomination
		conviction math.LegacyDec
	}
	var nominations []nominationWithConviction

	err = k.walkSeasonNominations(ctx, season.Number, 0, func(nom types.Nomination) (bool, error) {
		if nom.Rewarded {
			return false, nil // SEA-01: paid in an earlier (aborted) run
		}
		nominations = append(nominations, nominationWithConviction{nomination: nom, conviction: nom.Conviction})
		return false, nil
	})
	if err != nil {
		return err
	}

	// Sort by conviction descending, ties by id for a stable order
	sort.Slice(nominations, func(i, j int) bool {
		if !nominations[i].conviction.Equal(nominations[j].conviction) {
			return nominations[i].conviction.GT(nominations[j].conviction)
		}
		return nominations[i].nomination.Id < nominations[j].nomination.Id
	})

	// Filter: conviction >= min_conviction
	minConviction := params.RetroRewardMinConviction
	var eligible []nominationWithConviction
	for _, n := range nominations {
		if n.conviction.GTE(minConviction) {
			eligible = append(eligible, n)
		}
	}

	// Take top N
	maxRecipients := commontypes.Saturate[int](params.RetroRewardMaxRecipients)
	if len(eligible) > maxRecipients {
		eligible = eligible[:maxRecipients]
	}

	// Calculate total conviction for proportional distribution
	totalConviction := math.LegacyZeroDec()
	for _, n := range eligible {
		totalConviction = totalConviction.Add(n.conviction)
	}

	// Calculate activity-based retro PGF budget:
	// budget = ratio * season_initiative_minting, clamped to [min, max].
	// SEA-12: the base is DREAM minted for completed initiatives only, not
	// every season mint (staking, referral and jury rewards, or an earlier
	// retro payout).
	var budget math.LegacyDec
	if k.late.repKeeper != nil {
		seasonMinted, getErr := k.late.repKeeper.GetSeasonInitiativeRewardsMinted(ctx)
		if getErr != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("GetSeasonInitiativeRewardsMinted failed", "err", getErr)
		}
		rawBudget := params.RetroRewardBudgetRatio.MulInt(seasonMinted)
		budgetInt := rawBudget.TruncateInt()
		// Clamp to [min, max]
		if budgetInt.LT(params.RetroRewardBudgetMin) {
			budgetInt = params.RetroRewardBudgetMin
		}
		if budgetInt.GT(params.RetroRewardBudgetMax) {
			budgetInt = params.RetroRewardBudgetMax
		}
		budget = math.LegacyNewDecFromInt(budgetInt)
	} else {
		budget = math.LegacyNewDecFromInt(params.RetroRewardBudgetMin) // fallback
	}

	// Distribute rewards
	if totalConviction.IsPositive() && len(eligible) > 0 {
		for _, n := range eligible {
			// reward_i = budget * (conviction_i / total_conviction)
			share := n.conviction.Quo(totalConviction)
			reward := budget.Mul(share)

			nom := n.nomination
			nom.RewardAmount = reward
			nom.Conviction = n.conviction
			if !reward.TruncateInt().IsPositive() {
				// A deliberate zero (the share truncates to nothing) is not
				// a failure and is never retried (SEA-21).
				nom.Rewarded = false
				if err := k.SetNomination(ctx, nom); err != nil {
					return err
				}
				sdkCtx.EventManager().EmitEvent(sdk.NewEvent("retro_reward_zero",
					sdk.NewAttribute("season", fmt.Sprintf("%d", season.Number)),
					sdk.NewAttribute("nomination_id", fmt.Sprintf("%d", nom.Id)),
					sdk.NewAttribute("reward_amount", reward.String()),
				))
				continue
			}
			if err := k.attemptRetroPayout(ctx, nom); err != nil {
				return err
			}
		}
	}

	return nil
}

// MaxRetroPayoutAttempts bounds how often a failed retro-PGF payout is
// tried: once at the payout block, then once per epoch from the
// BeginBlocker until it is paid or abandoned (SEA-21).
const MaxRetroPayoutAttempts = 10

// maxRetroRetriesPerBlock bounds the retries processed in one block.
const maxRetroRetriesPerBlock = 10

// attemptRetroPayout pays nom.RewardAmount (truncated) to the nominator.
//
// SEA-21: the payment runs in its own cache context and commits only on
// success, so a mint that fails after a treasury draw never leaves a
// partial, unrecorded credit. On failure the nomination records the
// attempt and stays in RetroPayoutRetry (SetNomination keeps the queue);
// after MaxRetroPayoutAttempts it is abandoned with an event. Only a
// store error writing season state is returned.
func (k Keeper) attemptRetroPayout(ctx context.Context, nom types.Nomination) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	rewardInt := nom.RewardAmount.TruncateInt()

	var treasuryPaid, minted math.Int
	var payErr error
	if k.late.repKeeper == nil {
		payErr = errors.New("rep keeper not wired")
	} else if addr, err := k.addressCodec.StringToBytes(nom.Nominator); err != nil {
		payErr = err
	} else {
		cacheCtx, write := sdkCtx.CacheContext()
		treasuryPaid, minted, payErr = k.late.repKeeper.PayRetroPgfReward(cacheCtx, addr, rewardInt)
		if payErr == nil {
			write()
		}
	}

	if payErr != nil {
		nom.PayoutAttempts++
		abandoned := nom.PayoutAttempts >= MaxRetroPayoutAttempts
		nom.PayoutAbandoned = abandoned
		if err := k.SetNomination(ctx, nom); err != nil {
			return err
		}
		sdkCtx.Logger().Error("failed to pay DREAM for retro reward",
			"nomination_id", nom.Id, "nominator", nom.Nominator,
			"amount", rewardInt.String(), "attempt", nom.PayoutAttempts, "error", payErr)
		evType := "retro_reward_mint_failed"
		if abandoned {
			evType = "retro_reward_abandoned"
		}
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(evType,
			sdk.NewAttribute("season", fmt.Sprintf("%d", nom.Season)),
			sdk.NewAttribute("nomination_id", fmt.Sprintf("%d", nom.Id)),
			sdk.NewAttribute("recipient", nom.Nominator),
			sdk.NewAttribute("content_ref", nom.ContentRef),
			sdk.NewAttribute("reward_amount", nom.RewardAmount.String()),
			sdk.NewAttribute("attempt", fmt.Sprintf("%d", nom.PayoutAttempts)),
			sdk.NewAttribute("reason", payErr.Error()),
		))
		return nil
	}

	nom.Rewarded = true
	if err := k.SetNomination(ctx, nom); err != nil {
		return err
	}
	record := types.RetroRewardRecord{
		Season:             nom.Season,
		NominationId:       nom.Id,
		Recipient:          nom.Nominator,
		ContentRef:         nom.ContentRef,
		Conviction:         nom.Conviction,
		RewardAmount:       nom.RewardAmount,
		DistributedAtBlock: sdkCtx.BlockHeight(),
	}
	if err := k.RetroRewardRecord.Set(ctx, fmt.Sprintf("%d/%d", nom.Season, nom.Id), record); err != nil {
		return err
	}
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"retro_reward_distributed",
			sdk.NewAttribute("season", fmt.Sprintf("%d", nom.Season)),
			sdk.NewAttribute("nomination_id", fmt.Sprintf("%d", nom.Id)),
			sdk.NewAttribute("recipient", nom.Nominator),
			sdk.NewAttribute("content_ref", nom.ContentRef),
			sdk.NewAttribute("reward_amount", nom.RewardAmount.String()),
			sdk.NewAttribute("treasury_paid", treasuryPaid.String()),
			sdk.NewAttribute("minted", minted.String()),
			sdk.NewAttribute("conviction", nom.Conviction.String()),
			sdk.NewAttribute("attempt", fmt.Sprintf("%d", nom.PayoutAttempts+1)),
		),
	)
	return nil
}

// retryRetroPayouts retries queued failed payouts (SEA-21): only on epoch
// boundaries (the x/rep mint ceiling is per epoch), never while a season
// transition runs, at most maxRetroRetriesPerBlock per block, each in its
// own cache context.
func (k Keeper) retryRetroPayouts(ctx context.Context) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.Params.Get(ctx)
	if err != nil || params.EpochBlocks <= 0 || sdkCtx.BlockHeight()%params.EpochBlocks != 0 {
		return
	}
	if st, err := k.SeasonTransitionState.Get(ctx); err == nil && st.Phase != types.TransitionPhase_TRANSITION_PHASE_UNSPECIFIED {
		return
	}
	var due []collections.Pair[uint64, uint64]
	if err := k.RetroPayoutRetry.Walk(ctx, nil, func(key collections.Pair[uint64, uint64]) (bool, error) {
		due = append(due, key)
		return len(due) >= maxRetroRetriesPerBlock, nil
	}); err != nil {
		sdkCtx.Logger().Error("failed to walk retro payout retries", "error", err)
		return
	}
	for _, key := range due {
		nom, err := k.Nomination.Get(ctx, key.K2())
		if err != nil || !payoutOwed(nom) {
			if err := k.RetroPayoutRetry.Remove(ctx, key); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("retryRetroPayouts: RetroPayoutRetry.Remove failed", "err", err)
			}
			continue
		}
		cacheCtx, write := sdkCtx.CacheContext()
		if err := k.attemptRetroPayout(cacheCtx, nom); err != nil {
			sdkCtx.Logger().Error("retro payout retry failed", "nomination_id", nom.Id, "error", err)
			continue
		}
		write()
	}
}

// processReturnNominationStakesPhase returns all nomination stakes to stakers.
// Uses a cursor (state.LastProcessed) to stream stakes without loading all into memory.
//
// Stakes on nominations from earlier seasons (strays left by an unlock that
// failed in an earlier transition) are returned too, so nothing stays
// locked past one extra season (SEA-13).
//
// SEA-17: an unlock failure is never reported as a return. When x/rep says
// the lock is already gone (the staker was zeroed or no longer exists, so
// the DREAM was burned with them) the record is deleted; any other error
// keeps the record for the next transition and emits
// nomination_stake_return_failed.
func (k Keeper) processReturnNominationStakesPhase(ctx context.Context, state *types.SeasonTransitionState, batchSize int) (bool, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	season, err := k.Season.Get(ctx)
	if err != nil {
		return false, err
	}

	processed := 0
	var lastKey string

	err = k.NominationStake.Walk(ctx, cursorAfter(state.LastProcessed), func(key string, stake types.NominationStake) (bool, error) {
		lastKey = key
		// Check if this stake belongs to a nomination in this or an earlier season
		nom, nomErr := k.Nomination.Get(ctx, stake.NominationId)
		if nomErr != nil {
			// Nomination not found — clean up orphaned stake
			if err := k.NominationStake.Remove(ctx, key); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("processReturnNominationStakesPhase: NominationStake.Remove failed", "err", err)
			}
			return false, nil
		}
		if nom.Season > season.Number {
			return false, nil
		}

		// Return the stake
		returned := true
		var failReason string
		if k.late.repKeeper != nil {
			addr, addrErr := k.addressCodec.StringToBytes(stake.Staker)
			if addrErr == nil {
				amountInt := stake.Amount.TruncateInt()
				if amountInt.IsPositive() {
					if uErr := k.late.repKeeper.UnlockDREAM(ctx, addr, amountInt); uErr != nil {
						returned = false
						failReason = uErr.Error()
						if lockGone(uErr) {
							// Nothing left to return; drop the stale record.
							if err := k.NominationStake.Remove(ctx, key); err != nil {
								sdk.UnwrapSDKContext(ctx).Logger().Error("processReturnNominationStakesPhase: NominationStake.Remove failed", "err", err)
							}
						}
					}
				}
			}
		}

		if returned {
			if err := k.NominationStake.Remove(ctx, key); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("processReturnNominationStakesPhase: NominationStake.Remove failed", "err", err)
			}
			sdkCtx.EventManager().EmitEvent(
				sdk.NewEvent(
					"nomination_stake_returned",
					sdk.NewAttribute("nomination_id", fmt.Sprintf("%d", stake.NominationId)),
					sdk.NewAttribute("staker", stake.Staker),
					sdk.NewAttribute("amount", stake.Amount.String()),
				),
			)
		} else {
			sdkCtx.Logger().Error("failed to return nomination stake",
				"nomination_id", stake.NominationId, "staker", stake.Staker, "error", failReason)
			sdkCtx.EventManager().EmitEvent(
				sdk.NewEvent(
					"nomination_stake_return_failed",
					sdk.NewAttribute("nomination_id", fmt.Sprintf("%d", stake.NominationId)),
					sdk.NewAttribute("staker", stake.Staker),
					sdk.NewAttribute("amount", stake.Amount.String()),
					sdk.NewAttribute("reason", failReason),
				),
			)
		}

		processed++
		state.ProcessedCount++

		// Stop after processing batchSize stakes
		if processed >= batchSize {
			return true, nil // stop iteration
		}
		return false, nil
	})
	if err != nil {
		return false, err
	}

	// Update cursor for next batch
	if lastKey != "" {
		state.LastProcessed = lastKey
	}

	// If we processed fewer than batchSize, we've exhausted the iterator
	return processed < batchSize, nil
}

// lockGone reports whether an UnlockDREAM error means the locked DREAM no
// longer exists (the member was zeroed, which burns staked DREAM, or the
// member record is gone), as opposed to a transient or store failure.
func lockGone(err error) bool {
	return errors.Is(err, reptypes.ErrInsufficientStake) || errors.Is(err, reptypes.ErrMemberNotFound)
}
