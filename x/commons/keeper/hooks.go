package keeper

import (
	"context"
	"fmt"

	futarchytypes "sparkdream/x/futarchy/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Ensure Keeper implements the interface
var _ futarchytypes.FutarchyHooks = Keeper{}

// AfterMarketResolved is called by x/futarchy when a market ends.
func (k Keeper) AfterMarketResolved(ctx context.Context, marketId uint64, winner string) error {
	// 1. Check if this market is linked to any Commons Group
	// Collections API works natively with context.Context
	groupName, err := k.MarketToGroup.Get(ctx, marketId)
	if err != nil {
		// Market not linked to a group; ignore safely.
		return nil
	}

	// 2. Fetch the Committee Group
	group, err := k.Groups.Get(ctx, groupName)
	if err != nil {
		return fmt.Errorf("hook error: market linked to non-existent group %s", groupName)
	}

	// 3. Apply Elastic Tenure Logic
	bonusTime := group.TermDuration / 5   // +20%
	penaltyTime := group.TermDuration / 2 // -50%

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentTime := sdkCtx.BlockTime().Unix()

	switch winner {
	case "yes":
		// CONFIDENCE: Extend
		group.CurrentTermExpiration += bonusTime

		// Cap logic: Max 2 terms from *now* (or strictly from activation)
		maxExpiration := currentTime + (group.TermDuration * 2)
		if group.CurrentTermExpiration > maxExpiration {
			group.CurrentTermExpiration = maxExpiration
		}

		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent("elastic_tenure",
				sdk.NewAttribute("group", groupName),
				sdk.NewAttribute("action", "extended"),
				sdk.NewAttribute("seconds", fmt.Sprintf("%d", bonusTime)),
			),
		)
	case "no":
		// NO CONFIDENCE: Shorten
		group.CurrentTermExpiration -= penaltyTime

		// Safety: Do not expire in the past; expire "now" to trigger immediate re-election
		if group.CurrentTermExpiration < currentTime {
			group.CurrentTermExpiration = currentTime
		}

		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent("elastic_tenure",
				sdk.NewAttribute("group", groupName),
				sdk.NewAttribute("action", "shortened"),
				sdk.NewAttribute("seconds", fmt.Sprintf("%d", penaltyTime)),
			),
		)
	default:
		// RESOLVED_INVALID (Quorum failed)
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent("market_invalid_no_quorum",
				sdk.NewAttribute("group", groupName),
				sdk.NewAttribute("action", "no quorum"),
			),
		)
		return nil
	}

	// 4. Clean up the link
	if err := k.MarketToGroup.Remove(ctx, marketId); err != nil {
		return err
	}

	// 5. Save the updated Group
	return k.Groups.Set(ctx, groupName, group)
}
