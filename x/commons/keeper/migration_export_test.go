package keeper

import "context"

// CheckVotingPeriodCapForTest exposes the ZK-delay voting period cap.
func (k Keeper) CheckVotingPeriodCapForTest(ctx context.Context, votingPeriodSeconds int64) error {
	return k.checkVotingPeriodCap(ctx, votingPeriodSeconds)
}
