package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GetCurrentEpoch calculates the current epoch based on block height and params
func (k Keeper) GetCurrentEpoch(ctx context.Context) (int64, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0, err
	}
	if params.EpochBlocks <= 0 {
		return 0, nil // Avoid division by zero
	}
	return sdkCtx.BlockHeight() / params.EpochBlocks, nil
}

// ApplyPendingDecay calculates and applies decay to a member's balance
// It updates the member struct in-place but does not save to store (caller must save)
func (k Keeper) ApplyPendingDecay(ctx context.Context, member *types.Member) error {
	currentEpoch, err := k.GetCurrentEpoch(ctx)
	if err != nil {
		return err
	}

	if member.LastDecayEpoch >= currentEpoch {
		return nil
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	elapsed := currentEpoch - member.LastDecayEpoch
	if elapsed <= 0 {
		return nil
	}

	// Calculate decay: balance * (1 - rate)^elapsed
	decayRate := params.UnstakedDecayRate
	if decayRate.IsZero() {
		member.LastDecayEpoch = currentEpoch
		return nil
	}

	one := math.LegacyOneDec()
	factor := one.Sub(decayRate)

	// factor^elapsed
	multiplier := factor.Power(uint64(elapsed))

	unstaked := member.DreamBalance.Sub(*member.StakedDream)
	if unstaked.IsPositive() {
		current := math.LegacyNewDecFromInt(unstaked)
		newUnstakedDec := current.Mul(multiplier)
		newUnstaked := newUnstakedDec.TruncateInt()

		decayAmount := unstaked.Sub(newUnstaked)

		if decayAmount.IsPositive() {
			if decayAmount.GT(*member.DreamBalance) {
				*member.DreamBalance = math.NewInt(0) // Should not happen given logic, but safe guard
			} else {
				*member.DreamBalance = member.DreamBalance.Sub(decayAmount)
			}
			*member.LifetimeBurned = member.LifetimeBurned.Add(decayAmount)
		}
	}

	member.LastDecayEpoch = currentEpoch
	return nil
}

// GetBalance returns the balance of a member, applying any pending decay first.
// It persists the updated member state to the store.
func (k Keeper) GetBalance(ctx context.Context, addr sdk.AccAddress) (math.Int, error) {
	member, err := k.Member.Get(ctx, addr.String())
	if err != nil {
		// Member not found, return 0
		return math.NewInt(0), nil
	}

	// Apply decay
	if err := k.ApplyPendingDecay(ctx, &member); err != nil {
		return math.NewInt(0), err
	}

	// Persist update
	if err := k.Member.Set(ctx, addr.String(), member); err != nil {
		return math.NewInt(0), err
	}

	return *member.DreamBalance, nil
}
