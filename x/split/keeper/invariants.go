package keeper

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/split/types"
)

// RegisterInvariants registers the split invariants. x/crisis is not wired
// on chain; app's TestInvariants runs every module's RegisterInvariants.
func RegisterInvariants(ir sdk.InvariantRegistry, k Keeper) {
	ir.RegisterRoute(types.ModuleName, "share-weights", ShareWeightsInvariant(k))
}

// ShareWeightsInvariant: every share is a valid address with a weight in
// (0, MaxShareWeight], and the weights sum to at most MaxTotalShareWeight.
func ShareWeightsInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var problems string
		var total uint64
		if err := k.Share.Walk(ctx, nil, func(addr string, s types.Share) (bool, error) {
			if _, err := sdk.AccAddressFromBech32(addr); err != nil {
				problems += fmt.Sprintf("share %s: invalid address\n", addr)
			}
			if s.Weight == 0 || s.Weight > types.MaxShareWeight {
				problems += fmt.Sprintf("share %s: weight %d outside (0, %d]\n", addr, s.Weight, types.MaxShareWeight)
			}
			total += s.Weight
			return false, nil
		}); err != nil {
			return fmt.Sprintf("Share.Walk failed: %v", err), true
		}
		if total > types.MaxTotalShareWeight {
			problems += fmt.Sprintf("total share weight %d > %d\n", total, types.MaxTotalShareWeight)
		}
		return sdk.FormatInvariant(types.ModuleName, "share-weights", problems), problems != ""
	}
}
