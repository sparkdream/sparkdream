package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) WithdrawTagBudget(ctx context.Context, msg *types.MsgWithdrawTagBudget) (*types.MsgWithdrawTagBudgetResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	budget, err := k.TagBudget.Get(ctx, msg.BudgetId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrTagBudgetNotFound, fmt.Sprintf("budget %d not found", msg.BudgetId))
	}

	if budget.GroupAccount != msg.Creator {
		return nil, errorsmod.Wrap(types.ErrNotGroupAccount, "only the group account can withdraw the budget")
	}

	remainingBalance, _ := math.NewIntFromString(budget.PoolBalance)
	if remainingBalance.IsZero() {
		return nil, errorsmod.Wrap(types.ErrTagBudgetInsufficient, "budget pool is empty")
	}

	groupAddr, _ := sdk.AccAddressFromBech32(budget.GroupAccount)
	withdrawCoins := sdk.NewCoins(sdk.NewCoin(types.TagBudgetFeeDenom, remainingBalance))
	if err := k.bankKeeper.SendCoins(ctx, TagBudgetEscrowAddress(), groupAddr, withdrawCoins); err != nil {
		return nil, errorsmod.Wrap(err, "failed to withdraw tag budget funds")
	}

	budget.Active = false
	budget.PoolBalance = "0"

	if err := k.TagBudget.Set(ctx, msg.BudgetId, budget); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update tag budget")
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"tag_budget_withdrawn",
			sdk.NewAttribute("budget_id", fmt.Sprintf("%d", msg.BudgetId)),
			sdk.NewAttribute("withdrawn_by", msg.Creator),
			sdk.NewAttribute("amount_withdrawn", remainingBalance.String()),
		),
	)

	return &types.MsgWithdrawTagBudgetResponse{}, nil
}
