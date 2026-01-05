package keeper

import (
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
	group, err := k.ExtendedGroup.Get(ctx, groupName)
	if err != nil {
		return err
	}

	// 2. Prepare Market Params
	question := fmt.Sprintf("Confidence Vote: %s", groupName)
	symbol := fmt.Sprintf("CONF-%s-%d", groupName, ctx.BlockHeight()) // Unique Symbol

	subsidyCoin := sdk.NewCoin("uspark", math.NewIntFromUint64(1000000000)) // 1000 SPARK

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
