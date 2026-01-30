package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) ToggleTagBudget(ctx context.Context, msg *types.MsgToggleTagBudget) (*types.MsgToggleTagBudgetResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Load tag budget
	budget, err := k.TagBudget.Get(ctx, msg.BudgetId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrTagBudgetNotFound, fmt.Sprintf("budget %d not found", msg.BudgetId))
	}

	// Only the group account itself can toggle (requires group decision)
	if budget.GroupAccount != msg.Creator {
		return nil, errorsmod.Wrap(types.ErrNotGroupAccount, "only the group account can toggle the budget")
	}

	// Toggle the active status
	budget.Active = msg.Active

	if err := k.TagBudget.Set(ctx, msg.BudgetId, budget); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update tag budget")
	}

	// Emit event
	status := "paused"
	if msg.Active {
		status = "resumed"
	}
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"tag_budget_toggled",
			sdk.NewAttribute("budget_id", fmt.Sprintf("%d", msg.BudgetId)),
			sdk.NewAttribute("status", status),
			sdk.NewAttribute("toggled_by", msg.Creator),
		),
	)

	return &types.MsgToggleTagBudgetResponse{}, nil
}
