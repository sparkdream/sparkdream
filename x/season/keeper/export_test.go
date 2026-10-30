package keeper

import (
	"context"

	"sparkdream/x/season/types"
)

// ProcessTitlesPhaseForTest exposes the TITLES transition phase to tests.
func (k Keeper) ProcessTitlesPhaseForTest(ctx context.Context, state *types.SeasonTransitionState, batchSize int) (bool, error) {
	return k.processTitlesPhase(ctx, state, batchSize)
}

// IndexNewNominationForTest records a seeded nomination in the Nominate
// indexes (duplicate-ref set and per-nominator count).
func (k Keeper) IndexNewNominationForTest(ctx context.Context, nom types.Nomination) error {
	return k.indexNewNomination(ctx, nom)
}
