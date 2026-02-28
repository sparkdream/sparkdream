package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/season/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// CalculateNominationConviction calculates the conviction score for a nomination
// based on all its stakes. Formula: conviction = sum(stake.amount * min(1.0, elapsed / (2 * halfLife)))
// where elapsed and halfLife are in blocks.
func (k Keeper) CalculateNominationConviction(ctx context.Context, nomination types.Nomination) (math.LegacyDec, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentBlock := sdkCtx.BlockHeight()

	params, err := k.Params.Get(ctx)
	if err != nil {
		return math.LegacyZeroDec(), err
	}

	halfLifeBlocks := int64(params.NominationConvictionHalfLifeEpochs) * params.EpochBlocks
	if halfLifeBlocks <= 0 {
		halfLifeBlocks = 3 * 17280 // fallback: 3 epochs * default blocks
	}

	totalConviction := math.LegacyZeroDec()
	twoHalfLife := math.LegacyNewDec(2 * halfLifeBlocks)

	// Iterate all stakes for this nomination
	prefix := fmt.Sprintf("%d/", nomination.Id)
	err = k.NominationStake.Walk(ctx, nil, func(key string, stake types.NominationStake) (bool, error) {
		if len(key) < len(prefix) || key[:len(prefix)] != prefix {
			return false, nil
		}

		elapsed := currentBlock - stake.StakedAtBlock
		if elapsed < 0 {
			elapsed = 0
		}

		// timeFactor = min(1.0, elapsed / (2 * halfLifeBlocks))
		timeFactor := math.LegacyMinDec(
			math.LegacyOneDec(),
			math.LegacyNewDec(elapsed).Quo(twoHalfLife),
		)

		stakeConviction := stake.Amount.Mul(timeFactor)
		totalConviction = totalConviction.Add(stakeConviction)

		return false, nil
	})
	if err != nil {
		return math.LegacyZeroDec(), err
	}

	return totalConviction, nil
}
