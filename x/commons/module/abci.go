package commons

import (
	"context"

	"sparkdream/x/commons/keeper"
)

// EndBlocker fires the confidence-vote markets that are due. Kept as the
// module-level entry point; the logic lives in keeper.EndBlockMarketTriggers.
func EndBlocker(ctx context.Context, k keeper.Keeper) error {
	return k.EndBlockMarketTriggers(ctx)
}
