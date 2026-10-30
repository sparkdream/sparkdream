package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/season/types"
)

// ZeroingHook adapts the keeper to x/rep's MemberZeroingHook.
type ZeroingHook struct{ k *Keeper }

// NewZeroingHook returns the hook app.go registers with x/rep.
func NewZeroingHook(k *Keeper) ZeroingHook { return ZeroingHook{k: k} }

// AfterMemberZeroed drops a zeroed member's nomination stakes. x/rep burned
// the DREAM behind them with the rest of the member's balance; kept, they
// would go on adding conviction to the nominations they back, and the RETURN
// phase would try to unlock DREAM that is gone. Each backed nomination's
// total and conviction are recomputed without the stake.
func (h ZeroingHook) AfterMemberZeroed(ctx context.Context, member string, addresses []string) error {
	k := h.k
	owned := make(map[string]struct{}, len(addresses))
	for _, a := range addresses {
		owned[a] = struct{}{}
	}

	var keys []string
	var stakes []types.NominationStake
	if err := k.NominationStake.Walk(ctx, nil, func(key string, s types.NominationStake) (bool, error) {
		if _, ok := owned[s.Staker]; ok {
			keys = append(keys, key)
			stakes = append(stakes, s)
		}
		return false, nil
	}); err != nil {
		return err
	}

	for i, s := range stakes {
		if err := k.NominationStake.Remove(ctx, keys[i]); err != nil {
			return err
		}
		nom, err := k.Nomination.Get(ctx, s.NominationId)
		if err != nil {
			continue
		}
		nom.TotalStaked = nom.TotalStaked.Sub(s.Amount)
		if nom.TotalStaked.IsNegative() {
			nom.TotalStaked = math.LegacyZeroDec()
		}
		conviction, err := k.CalculateNominationConviction(ctx, nom)
		if err != nil {
			return err
		}
		nom.Conviction = conviction
		if err := k.SetNomination(ctx, nom); err != nil {
			return err
		}
	}

	if len(stakes) > 0 {
		sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
			"season_zeroed_stakes_dropped",
			sdk.NewAttribute("member", member),
			sdk.NewAttribute("stakes_dropped", fmt.Sprintf("%d", len(stakes))),
		))
	}
	return nil
}
