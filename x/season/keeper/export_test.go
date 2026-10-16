package keeper

import (
	"context"

	"sparkdream/x/season/types"
)

// ProcessTitlesPhaseForTest exposes the TITLES transition phase to tests.
func (k Keeper) ProcessTitlesPhaseForTest(ctx context.Context, state *types.SeasonTransitionState, batchSize int) (bool, error) {
	return k.processTitlesPhase(ctx, state, batchSize)
}
