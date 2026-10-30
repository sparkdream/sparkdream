package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ZeroingHook adapts the keeper to x/rep's MemberZeroingHook.
type ZeroingHook struct{ k *Keeper }

// NewZeroingHook returns the hook app.go registers with x/rep.
func NewZeroingHook(k *Keeper) ZeroingHook { return ZeroingHook{k: k} }

// AfterMemberZeroed closes a zeroed member's active post-conviction stakes
// (FRM-27). x/rep burned the DREAM behind them with the rest of the
// member's balance: kept active, they would go on streaming forum reputation
// to the authors they back, and their release would fail against a lock that
// no longer exists. Each one is marked released and moved to the released
// index without an unlock. The reputation it already credited stays, and the
// record stays slashable until GC, exactly as for a stake its staker
// released (a hide that stands before then still claws that reputation back).
func (h ZeroingHook) AfterMemberZeroed(ctx context.Context, member string, addresses []string) error {
	k := h.k
	now := sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
	closed := 0
	for _, addr := range addresses {
		var ids []uint64
		rng := collections.NewPrefixedPairRange[string, uint64](addr)
		if err := k.PostConvictionStakesByStaker.Walk(ctx, rng, func(key collections.Pair[string, uint64]) (bool, error) {
			ids = append(ids, key.K2())
			return false, nil
		}); err != nil {
			return err
		}
		for _, id := range ids {
			if err := k.PostConvictionStakesByStaker.Remove(ctx, collections.Join(addr, id)); err != nil {
				return err
			}
			stake, err := k.PostConvictionStake.Get(ctx, id)
			if err != nil || stake.Released {
				continue
			}
			stake.Released = true
			if err := k.PostConvictionStake.Set(ctx, id, stake); err != nil {
				return err
			}
			if err := k.PostConvictionStakesByPost.Remove(ctx, collections.Join(stake.PostId, id)); err != nil {
				return err
			}
			if err := k.indexReleasedStake(ctx, stake, now); err != nil {
				return err
			}
			closed++
		}
	}
	if closed > 0 {
		sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
			"forum_zeroed_stakes_closed",
			sdk.NewAttribute("member", member),
			sdk.NewAttribute("stakes_closed", fmt.Sprintf("%d", closed)),
		))
	}
	return nil
}
