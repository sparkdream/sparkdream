package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) TopUpTagBudget(ctx context.Context, msg *types.MsgTopUpTagBudget) (*types.MsgTopUpTagBudgetResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Load tag budget
	budget, err := k.TagBudget.Get(ctx, msg.BudgetId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrTagBudgetNotFound, fmt.Sprintf("budget %d not found", msg.BudgetId))
	}

	// Verify creator is a member of the group (any member can top up)
	if !k.IsGroupMember(ctx, budget.GroupAccount, msg.Creator) {
		return nil, errorsmod.Wrap(types.ErrNotGroupMember, "only group members can top up tag budget")
	}

	// Parse and validate top up amount
	topUpAmount, ok := math.NewIntFromString(msg.Amount)
	if !ok || topUpAmount.IsNegative() || topUpAmount.IsZero() {
		return nil, errorsmod.Wrap(types.ErrInvalidAmount, "invalid top up amount")
	}

	// Transfer SPARK from creator to module (escrow)
	creatorAddr, _ := sdk.AccAddressFromBech32(msg.Creator)
	topUpCoins := sdk.NewCoins(sdk.NewCoin(types.DefaultFeeDenom, topUpAmount))
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, creatorAddr, types.ModuleName, topUpCoins); err != nil {
		return nil, errorsmod.Wrap(err, "failed to escrow top up funds")
	}

	// Update pool balance
	poolBalance, _ := math.NewIntFromString(budget.PoolBalance)
	newBalance := poolBalance.Add(topUpAmount)
	budget.PoolBalance = newBalance.String()

	if err := k.TagBudget.Set(ctx, msg.BudgetId, budget); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update tag budget")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"tag_budget_topped_up",
			sdk.NewAttribute("budget_id", fmt.Sprintf("%d", msg.BudgetId)),
			sdk.NewAttribute("topped_up_by", msg.Creator),
			sdk.NewAttribute("amount", msg.Amount),
			sdk.NewAttribute("new_balance", budget.PoolBalance),
		),
	)

	return &types.MsgTopUpTagBudgetResponse{}, nil
}
