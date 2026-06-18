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

	// Both nomination_conviction_half_life_epochs and epoch_blocks are validated
	// > 0 by Params.Validate, so halfLifeBlocks is normally always positive. Guard
	// defensively against a corrupt/zero param read rather than substituting a magic
	// default: a zero or negative half-life makes the time-decay factor meaningless,
	// so return zero conviction instead of fabricating a window.
	halfLifeBlocks := int64(params.NominationConvictionHalfLifeEpochs) * params.EpochBlocks
	if halfLifeBlocks <= 0 {
		return math.LegacyZeroDec(), fmt.Errorf("non-positive conviction half-life (%d blocks): half_life_epochs=%d epoch_blocks=%d", halfLifeBlocks, params.NominationConvictionHalfLifeEpochs, params.EpochBlocks)
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
