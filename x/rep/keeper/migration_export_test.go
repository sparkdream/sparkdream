package keeper

import (
	"context"

	"sparkdream/x/rep/types"
)

// ResolveMigrationObjectionForTest exposes the jury-verdict handler of the
// COUNCIL path so the three outcomes can be asserted without seating a jury.
func (k Keeper) ResolveMigrationObjectionForTest(ctx context.Context, review types.JuryReview, verdict types.Verdict) error {
	return k.resolveMigrationObjection(ctx, review, verdict)
}

// CheckTargetFreshForTest exposes the freshness check.
func (k Keeper) CheckTargetFreshForTest(ctx context.Context, from, to string) error {
	return k.checkTargetFresh(ctx, from, to)
}
