package keeper

import (
	"context"
	"fmt"
	"sort"

	"sparkdream/x/season/types"

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
			if k.repKeeper != nil {
				addr, err := k.addressCodec.StringToBytes(n.nomination.Nominator)
				if err == nil {
					rewardInt := reward.TruncateInt()
					if rewardInt.IsPositive() {
						_ = k.repKeeper.MintDREAM(ctx, addr, rewardInt)
					}
				}
			}

			// Update nomination
			nom := n.nomination
			nom.RewardAmount = reward
			nom.Rewarded = true
			nom.Conviction = n.conviction
			_ = k.Nomination.Set(ctx, nom.Id, nom)

			// Create RetroRewardRecord
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
		}
	}

	// Phase completes in a single batch (nominations are bounded by max_recipients)
	return true, nil
}

// processReturnNominationStakesPhase returns all nomination stakes to stakers.
func (k Keeper) processReturnNominationStakesPhase(ctx context.Context, state *types.SeasonTransitionState, batchSize int) (bool, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	season, err := k.Season.Get(ctx)
	if err != nil {
		return false, err
	}

	// Collect all stakes for nominations in this season
	var stakesToReturn []struct {
		key   string
		stake types.NominationStake
	}

	err = k.NominationStake.Walk(ctx, nil, func(key string, stake types.NominationStake) (bool, error) {
		// Check if this stake belongs to a nomination in the current season
		nom, err := k.Nomination.Get(ctx, stake.NominationId)
		if err != nil {
			return false, nil // Skip if nomination not found
		}
		if nom.Season != season.Number {
			return false, nil
		}
		stakesToReturn = append(stakesToReturn, struct {
			key   string
			stake types.NominationStake
		}{key: key, stake: stake})
		return false, nil
	})
	if err != nil {
		return false, err
	}

	// Process in batches
	start := int(state.ProcessedCount)
	end := start + batchSize
	if end > len(stakesToReturn) {
		end = len(stakesToReturn)
	}

	for i := start; i < end; i++ {
		s := stakesToReturn[i]
		if k.repKeeper != nil {
			addr, err := k.addressCodec.StringToBytes(s.stake.Staker)
			if err == nil {
				amountInt := s.stake.Amount.TruncateInt()
				if amountInt.IsPositive() {
					_ = k.repKeeper.UnlockDREAM(ctx, addr, amountInt)
				}
			}
		}
		_ = k.NominationStake.Remove(ctx, s.key)

		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				"nomination_stake_returned",
				sdk.NewAttribute("nomination_id", fmt.Sprintf("%d", s.stake.NominationId)),
				sdk.NewAttribute("staker", s.stake.Staker),
				sdk.NewAttribute("amount", s.stake.Amount.String()),
			),
		)
	}

	state.ProcessedCount = uint64(end)
	return end >= len(stakesToReturn), nil
}
