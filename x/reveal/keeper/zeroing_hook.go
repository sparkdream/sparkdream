package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/reveal/types"
)

// ZeroingHook adapts the keeper to x/rep's MemberZeroingHook.
type ZeroingHook struct{ k *Keeper }

// NewZeroingHook returns the hook app.go registers with x/rep.
func NewZeroingHook(k *Keeper) ZeroingHook { return ZeroingHook{k: k} }

// AfterMemberZeroed drops a zeroed member's stakes on tranches that are still
// collecting (STAKING). x/rep burned the DREAM behind them with the rest of
// the member's balance, so they no longer back anything: kept, they would go
// on counting toward the tranche threshold and could back a tranche with
// DREAM that does not exist. Stakes on tranches already BACKED or later stay:
// they settle through unlockOrGone, which treats the missing lock as settled,
// and pulling them out mid-verification would change a tally in flight.
func (h ZeroingHook) AfterMemberZeroed(ctx context.Context, member string, addresses []string) error {
	k := h.k
	type held struct {
		id    uint64
		stake types.RevealStake
	}
	var stakes []held
	for _, addr := range addresses {
		rng := collections.NewPrefixedPairRange[string, uint64](addr)
		if err := k.StakesByStaker.Walk(ctx, rng, func(key collections.Pair[string, uint64]) (bool, error) {
			stake, err := k.RevealStake.Get(ctx, key.K2())
			if err != nil {
				return false, nil
			}
			stakes = append(stakes, held{id: key.K2(), stake: stake})
			return false, nil
		}); err != nil {
			return err
		}
	}

	dropped := 0
	for _, st := range stakes {
		contrib, err := k.Contribution.Get(ctx, st.stake.ContributionId)
		if err != nil {
			continue
		}
		tranche, err := GetTranche(&contrib, st.stake.TrancheId)
		if err != nil || tranche.Status != types.TrancheStatus_TRANCHE_STATUS_STAKING {
			continue
		}
		tranche.DreamStaked = tranche.DreamStaked.Sub(st.stake.Amount)
		if tranche.DreamStaked.IsNegative() {
			tranche.DreamStaked = math.ZeroInt()
		}
		if err := k.RevealStake.Remove(ctx, st.id); err != nil {
			return err
		}
		if err := k.StakesByTranche.Remove(ctx, collections.Join(TrancheKey(st.stake.ContributionId, st.stake.TrancheId), st.id)); err != nil {
			return err
		}
		if err := k.StakesByStaker.Remove(ctx, collections.Join(st.stake.Staker, st.id)); err != nil {
			return err
		}
		if err := k.saveContribution(ctx, &contrib); err != nil {
			return err
		}
		dropped++
	}

	if dropped > 0 {
		sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
			"reveal_zeroed_stakes_dropped",
			sdk.NewAttribute("member", member),
			sdk.NewAttribute("stakes_dropped", fmt.Sprintf("%d", dropped)),
		))
	}
	return nil
}
