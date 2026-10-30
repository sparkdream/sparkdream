package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/rep/types"
)

// undelegateTombstone begins unbonding every delegation held by a tombstone
// left by a RECOVERY or COUNCIL migration (at most MaxTombstoneUndelegations
// per call). The old key is lost or stolen there, so nobody can be relied on
// to undelegate; the normal unbonding period and slashing still apply.
// Rewards withdrawn by the unbond go to the withdraw address finalization
// pointed at the canonical address. The matured SPARK is paid to the
// tombstone by x/staking (UndelegateCoins credits the account directly, so
// the bank send restriction that redirects transfers to a tombstone does not
// apply) and MsgSweepTombstone moves it to the canonical address.
//
// Each delegation unbonds in its own cache context: one failure (for example
// the unbonding-entry cap of a validator) is logged with an event and the
// rest proceed. Returns how many unbondings started.
func (k Keeper) undelegateTombstone(ctx context.Context, tombstone sdk.AccAddress, cause string) int {
	sk := k.late.stakingKeeper
	if sk == nil {
		return 0
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	delegations, err := sk.GetDelegatorDelegations(ctx, tombstone, types.MaxTombstoneUndelegations)
	if err != nil {
		sdkCtx.Logger().Error("could not list tombstone delegations", "tombstone", tombstone.String(), "error", err)
		return 0
	}
	started := 0
	for _, del := range delegations {
		valAddr, err := sdk.ValAddressFromBech32(del.ValidatorAddress)
		if err != nil {
			sdkCtx.Logger().Error("invalid validator address on tombstone delegation", "validator", del.ValidatorAddress, "error", err)
			continue
		}
		cacheCtx, write := sdkCtx.CacheContext()
		completion, amount, err := sk.Undelegate(cacheCtx, tombstone, valAddr, del.Shares)
		if err != nil {
			sdkCtx.EventManager().EmitEvent(sdk.NewEvent("tombstone_undelegation_failed",
				sdk.NewAttribute("tombstone", tombstone.String()),
				sdk.NewAttribute("validator", del.ValidatorAddress),
				sdk.NewAttribute("cause", cause),
				sdk.NewAttribute("error", err.Error()),
			))
			continue
		}
		write()
		started++
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("tombstone_undelegated",
			sdk.NewAttribute("tombstone", tombstone.String()),
			sdk.NewAttribute("validator", del.ValidatorAddress),
			sdk.NewAttribute("amount", amount.String()),
			sdk.NewAttribute("completion_time", completion.UTC().String()),
			sdk.NewAttribute("cause", cause),
		))
	}
	if started > 0 {
		sdkCtx.Logger().Info("unbonding tombstone delegations", "tombstone", tombstone.String(), "count", fmt.Sprintf("%d", started), "cause", cause)
	}
	return started
}
