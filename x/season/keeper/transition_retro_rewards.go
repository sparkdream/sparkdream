package keeper

import (
	"context"
	"fmt"
	"sort"

	"sparkdream/x/season/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// processRetroRewardsPhase distributes retroactive rewards to top nominations.
// This runs as a transition phase before the existing snapshot phase.
func (k Keeper) processRetroRewardsPhase(ctx context.Context, state *types.SeasonTransitionState, batchSize int) (bool, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	season, err := k.Season.Get(ctx)
	if err != nil {
		return false, err
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return false, err
	}

	// Collect all nominations for this season
	type nominationWithConviction struct {
		nomination types.Nomination
		conviction math.LegacyDec
	}
	var nominations []nominationWithConviction

	err = k.Nomination.Walk(ctx, nil, func(_ uint64, nom types.Nomination) (bool, error) {
		if nom.Season != season.Number {
			return false, nil
		}
		// Recalculate final conviction
		conviction, err := k.CalculateNominationConviction(ctx, nom)
		if err != nil {
			return false, err
		}
		nom.Conviction = conviction
		nominations = append(nominations, nominationWithConviction{nomination: nom, conviction: conviction})
		return false, nil
	})
	if err != nil {
		return false, err
	}

	// Sort by conviction descending
	sort.Slice(nominations, func(i, j int) bool {
		return nominations[i].conviction.GT(nominations[j].conviction)
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
	maxRecipients := int(params.RetroRewardMaxRecipients)
	if len(eligible) > maxRecipients {
		eligible = eligible[:maxRecipients]
	}

	// Calculate total conviction for proportional distribution
	totalConviction := math.LegacyZeroDec()
	for _, n := range eligible {
		totalConviction = totalConviction.Add(n.conviction)
	}

	// Calculate activity-based retro PGF budget:
	// budget = ratio * season_initiative_minting, clamped to [min, max]
	var budget math.LegacyDec
	if k.repKeeper != nil {
		seasonMinted, _ := k.repKeeper.GetSeasonMinted(ctx)
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

			// Mint DREAM to nominator
			mintSuccess := false
			var mintFailReason string
			if k.repKeeper != nil {
				addr, err := k.addressCodec.StringToBytes(n.nomination.Nominator)
				if err != nil {
					mintFailReason = err.Error()
				} else {
					rewardInt := reward.TruncateInt()
					if rewardInt.IsPositive() {
						if mintErr := k.repKeeper.MintDREAM(ctx, addr, rewardInt); mintErr != nil {
							sdkCtx.Logger().Error("failed to mint DREAM for retro reward",
								"nomination_id", n.nomination.Id,
								"nominator", n.nomination.Nominator,
								"amount", rewardInt.String(),
								"error", mintErr)
							mintFailReason = mintErr.Error()
						} else {
							mintSuccess = true
						}
					} else {
						mintFailReason = "reward amount truncated to zero"
					}
				}
			} else {
				mintFailReason = "rep keeper not wired"
			}

			// Update nomination — only mark as Rewarded if mint succeeded
			nom := n.nomination
			nom.RewardAmount = reward
			nom.Rewarded = mintSuccess
			nom.Conviction = n.conviction
			_ = k.Nomination.Set(ctx, nom.Id, nom)

			if mintSuccess {
				// Create RetroRewardRecord only on successful mint.
				recordKey := fmt.Sprintf("%d/%d", season.Number, nom.Id)
				record := types.RetroRewardRecord{
					Season:             season.Number,
					NominationId:       nom.Id,
					Recipient:          nom.Nominator,
					ContentRef:         nom.ContentRef,
					Conviction:         n.conviction,
					RewardAmount:       reward,
					DistributedAtBlock: sdkCtx.BlockHeight(),
				}
				_ = k.RetroRewardRecord.Set(ctx, recordKey, record)

				sdkCtx.EventManager().EmitEvent(
					sdk.NewEvent(
						"retro_reward_distributed",
						sdk.NewAttribute("season", fmt.Sprintf("%d", season.Number)),
						sdk.NewAttribute("nomination_id", fmt.Sprintf("%d", nom.Id)),
						sdk.NewAttribute("recipient", nom.Nominator),
						sdk.NewAttribute("content_ref", nom.ContentRef),
						sdk.NewAttribute("reward_amount", reward.String()),
						sdk.NewAttribute("conviction", n.conviction.String()),
					),
				)
			} else {
				sdkCtx.EventManager().EmitEvent(
					sdk.NewEvent(
						"retro_reward_mint_failed",
						sdk.NewAttribute("season", fmt.Sprintf("%d", season.Number)),
						sdk.NewAttribute("nomination_id", fmt.Sprintf("%d", nom.Id)),
						sdk.NewAttribute("recipient", nom.Nominator),
						sdk.NewAttribute("content_ref", nom.ContentRef),
						sdk.NewAttribute("reward_amount", reward.String()),
						sdk.NewAttribute("reason", mintFailReason),
					),
				)
			}
		}
	}

	// Phase completes in a single batch (nominations are bounded by max_recipients)
	return true, nil
}

// processReturnNominationStakesPhase returns all nomination stakes to stakers.
// Uses a cursor (state.LastProcessed) to stream stakes without loading all into memory.
func (k Keeper) processReturnNominationStakesPhase(ctx context.Context, state *types.SeasonTransitionState, batchSize int) (bool, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	season, err := k.Season.Get(ctx)
	if err != nil {
		return false, err
	}

	// Use state.LastProcessed as the cursor key for iterator resume.
	// Empty string = start from the beginning (pass nil Ranger to walk all).
	var ranger collections.Ranger[string]
	if state.LastProcessed != "" {
		ranger = new(collections.Range[string]).StartExclusive(state.LastProcessed)
	}

	processed := 0
	var lastKey string

	err = k.NominationStake.Walk(ctx, ranger, func(key string, stake types.NominationStake) (bool, error) {
		// Check if this stake belongs to a nomination in the current season
		nom, nomErr := k.Nomination.Get(ctx, stake.NominationId)
		if nomErr != nil {
			// Nomination not found — clean up orphaned stake
			_ = k.NominationStake.Remove(ctx, key)
			lastKey = key
			return false, nil
		}
		if nom.Season != season.Number {
			lastKey = key
			return false, nil
		}

		// Return the stake
		if k.repKeeper != nil {
			addr, addrErr := k.addressCodec.StringToBytes(stake.Staker)
			if addrErr == nil {
				amountInt := stake.Amount.TruncateInt()
				if amountInt.IsPositive() {
					_ = k.repKeeper.UnlockDREAM(ctx, addr, amountInt)
				}
			}
		}
		_ = k.NominationStake.Remove(ctx, key)

		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				"nomination_stake_returned",
				sdk.NewAttribute("nomination_id", fmt.Sprintf("%d", stake.NominationId)),
				sdk.NewAttribute("staker", stake.Staker),
				sdk.NewAttribute("amount", stake.Amount.String()),
			),
		)

		lastKey = key
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
