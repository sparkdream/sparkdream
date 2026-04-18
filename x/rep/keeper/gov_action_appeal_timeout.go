package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// maxAppealTimeoutsPerBlock bounds the EndBlocker work to a safe amount of
// gas-equivalent state writes per block.
const maxAppealTimeoutsPerBlock = 50

// TimeoutExpiredAppeals walks pending GovActionAppeal records and transitions
// any whose Deadline < now to GOV_APPEAL_STATUS_TIMEOUT.
//
// On timeout: half of the appellant bond is refunded to the appellant and
// the other half is burned. No forum counter update — neither party is
// penalized for the jury failing to act.
func (k Keeper) TimeoutExpiredAppeals(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	processed := 0
	iter, err := k.GovActionAppeal.Iterate(ctx, nil)
	if err != nil {
		return fmt.Errorf("iterate gov action appeals: %w", err)
	}
	defer iter.Close()

	type pending struct {
		id     uint64
		appeal types.GovActionAppeal
	}
	var toProcess []pending

	for ; iter.Valid(); iter.Next() {
		if processed >= maxAppealTimeoutsPerBlock {
			break
		}
		kv, err := iter.KeyValue()
		if err != nil {
			continue
		}
		appeal := kv.Value
		if appeal.Status != types.GovAppealStatus_GOV_APPEAL_STATUS_PENDING {
			continue
		}
		if appeal.Deadline == 0 || appeal.Deadline >= now {
			continue
		}
		toProcess = append(toProcess, pending{id: kv.Key, appeal: appeal})
		processed++
	}

	// Close the iterator before mutating.
	iter.Close()

	for _, p := range toProcess {
		bond := parseIntOrZero(p.appeal.AppealBond)
		if bond.IsPositive() {
			half := bond.QuoRaw(2)
			refund := bond.Sub(half)
			if refund.IsPositive() {
				refundCoins := sdk.NewCoins(sdk.NewCoin(types.RewardDenom, refund))
				appellantAddr, addrErr := sdk.AccAddressFromBech32(p.appeal.Appellant)
				if addrErr != nil {
					sdkCtx.Logger().Error("invalid appellant on appeal",
						"appeal_id", p.id, "error", addrErr)
				} else if err := k.bankKeeper.SendCoinsFromModuleToAccount(
					ctx, types.ModuleName, appellantAddr, refundCoins,
				); err != nil {
					sdkCtx.Logger().Error("failed to refund appeal bond on timeout",
						"appeal_id", p.id, "error", err)
				}
			}
			if half.IsPositive() {
				burnCoins := sdk.NewCoins(sdk.NewCoin(types.RewardDenom, half))
				if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, burnCoins); err != nil {
					sdkCtx.Logger().Error("failed to burn appeal bond on timeout",
						"appeal_id", p.id, "error", err)
				}
			}
		}

		p.appeal.Status = types.GovAppealStatus_GOV_APPEAL_STATUS_TIMEOUT
		if err := k.GovActionAppeal.Set(ctx, p.id, p.appeal); err != nil {
			sdkCtx.Logger().Error("failed to update appeal on timeout",
				"appeal_id", p.id, "error", err)
			continue
		}

		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			"gov_action_appeal_timeout",
			sdk.NewAttribute("appeal_id", fmt.Sprintf("%d", p.id)),
			sdk.NewAttribute("action_type", p.appeal.ActionType.String()),
			sdk.NewAttribute("action_target", p.appeal.ActionTarget),
			sdk.NewAttribute("appellant", p.appeal.Appellant),
			sdk.NewAttribute("deadline", fmt.Sprintf("%d", p.appeal.Deadline)),
		))
	}

	return nil
}
