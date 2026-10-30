package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// maxSubmittedChecksPerBlock caps how many SUBMITTED initiatives the
// EndBlocker tests against the completion threshold per block (REP-E-10).
const maxSubmittedChecksPerBlock = 100

// CheckSubmittedInitiatives tests at most maxSubmittedChecksPerBlock
// SUBMITTED initiatives against the completion threshold and moves the ones
// that pass into the challenge period. It resumes after the id it stopped at
// in the previous block and starts over from the lowest id once it reaches the
// end, so every SUBMITTED initiative is checked at least once every
// ceil(n / maxSubmittedChecksPerBlock) blocks. Conviction only grows with
// time, so a later check never misses a completion; it may land a few blocks
// later than an every-block walk would.
func (k Keeper) CheckSubmittedInitiatives(ctx context.Context) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	status := int32(types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED)

	cursor, err := k.SubmittedCheckCursor.Get(ctx)
	if err != nil {
		cursor = 0
	}

	rng := collections.NewPrefixedPairRange[int32, uint64](status)
	if cursor > 0 {
		rng = rng.StartExclusive(cursor)
	}

	var ids []uint64
	reachedEnd := true
	if err := k.InitiativesByStatus.Walk(ctx, rng, func(key collections.Pair[int32, uint64]) (bool, error) {
		if len(ids) >= maxSubmittedChecksPerBlock {
			reachedEnd = false
			return true, nil
		}
		ids = append(ids, key.K2())
		return false, nil
	}); err != nil {
		sdkCtx.Logger().Error("failed to walk submitted initiatives", "error", err)
		return
	}

	for _, id := range ids {
		canComplete, err := k.CanCompleteInitiative(ctx, id)
		if err != nil {
			sdkCtx.Logger().Error("failed to check initiative completion", "initiative_id", id, "error", err)
			continue
		}
		if canComplete {
			if err := k.TransitionToChallengePeriod(ctx, id); err != nil {
				sdkCtx.Logger().Error("failed to transition initiative to challenge period", "initiative_id", id, "error", err)
			}
		}
	}

	next := uint64(0)
	if !reachedEnd && len(ids) > 0 {
		next = ids[len(ids)-1]
	}
	if err := k.SubmittedCheckCursor.Set(ctx, next); err != nil {
		sdkCtx.Logger().Error("failed to store submitted check cursor", "error", err)
	}
}
