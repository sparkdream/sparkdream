package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	// How long the "Confidence Vote" lasts
	GovernanceMarketDurationBlocks = 72000 // ~5 Days (assuming 6s blocks)

	// How long until tokens can be redeemed
	GovernanceMarketRedemptionBlocks = 302400 // ~21 Days (assuming 6s blocks)

	// Subsidy Amount (Paid by x/commons Treasury)
	GovernanceMarketSubsidy = "1000000000" // 1000 SPARK

	// GovernanceMarketRetryDelay is how long (seconds) a failed trigger
	// waits before it is retried, so one failure (e.g. an unfunded subsidy)
	// does not end the group's confidence-vote loop.
	GovernanceMarketRetryDelay int64 = 86400

	// MaxMarketTriggersPerBlock bounds the EndBlock work; the rest waits for
	// the next block.
	MaxMarketTriggersPerBlock = 10
)

func (k Keeper) ScheduleNextMarket(ctx sdk.Context, groupName string, termDuration int64) error {
	// 1. Calculate Next Trigger (Now + 50% of Term)
	halfTerm := termDuration / 2
	nextTrigger := ctx.BlockTime().Unix() + halfTerm

	// 2. Add to Queue
	return k.MarketTriggerQueue.Set(ctx, collections.Join(nextTrigger, groupName))
}

func (k Keeper) TriggerGovernanceMarket(ctx sdk.Context, groupName string) error {
	// 1. Get Group
	group, err := k.Groups.Get(ctx, groupName)
	if err != nil {
		return err
	}

	// 2. Prepare Market Params
	question := fmt.Sprintf("Confidence Vote: %s", groupName)
	symbol := fmt.Sprintf("CONF-%s-%d", groupName, ctx.BlockHeight()) // Unique Symbol

	subsidyAmount, ok := math.NewIntFromString(GovernanceMarketSubsidy)
	if !ok {
		return fmt.Errorf("invalid GovernanceMarketSubsidy %q", GovernanceMarketSubsidy)
	}
	subsidyCoin := sdk.NewCoin(k.BondDenom(ctx), subsidyAmount)

	// 3. Call Futarchy (Programmatic Creation)
	// Creator is the x/commons Module Account
	moduleAddr := k.GetModuleAddress()

	marketId, err := k.futarchyKeeper.CreateMarketInternal(
		ctx,
		moduleAddr,
		symbol,
		question,
		GovernanceMarketDurationBlocks,
		GovernanceMarketRedemptionBlocks,
		subsidyCoin,
	)
	if err != nil {
		return err
	}

	// 4. Link Market to Group (So the Elastic Tenure Hook works!)
	if err := k.MarketToGroup.Set(ctx, marketId, groupName); err != nil {
		return err
	}

	// 5. Schedule the NEXT market (The Loop)
	// Note: We use the group's TermDuration to calculate the 50% interval
	return k.ScheduleNextMarket(ctx, groupName, group.TermDuration)
}

// EndBlockMarketTriggers fires every confidence-vote market whose trigger
// time has come (CMN-08). Each trigger runs in a cache context: on success
// its writes (market, link, next trigger) commit; on failure nothing it did
// persists, an error is logged, and the trigger is retried after
// GovernanceMarketRetryDelay. Entries for a deleted group, or a group that
// has turned futarchy off, are dropped.
func (k Keeper) EndBlockMarketTriggers(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentTime := sdkCtx.BlockTime().Unix()

	// Up to CurrentTime (inclusive): EndExclusive at currentTime+1 with an
	// empty name covers every group name at currentTime.
	rng := new(collections.Range[collections.Pair[int64, string]]).
		EndExclusive(collections.Join(currentTime+1, ""))

	var due []collections.Pair[int64, string]
	if err := k.MarketTriggerQueue.Walk(ctx, rng, func(key collections.Pair[int64, string]) (bool, error) {
		due = append(due, key)
		return len(due) >= MaxMarketTriggersPerBlock, nil
	}); err != nil {
		return err
	}

	for _, key := range due {
		groupName := key.K2()
		if err := k.MarketTriggerQueue.Remove(ctx, key); err != nil {
			return err
		}

		group, err := k.Groups.Get(ctx, groupName)
		if err != nil || !group.FutarchyEnabled {
			continue
		}

		cacheCtx, write := sdkCtx.CacheContext()
		if err := k.TriggerGovernanceMarket(cacheCtx, groupName); err != nil {
			sdkCtx.Logger().Error("failed to auto-create confidence market; retrying later",
				"group", groupName, "error", err)
			if err := k.MarketTriggerQueue.Set(ctx, collections.Join(currentTime+GovernanceMarketRetryDelay, groupName)); err != nil {
				return err
			}
			continue
		}
		write()
	}
	return nil
}
