package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) CreateTagBudget(ctx context.Context, msg *types.MsgCreateTagBudget) (*types.MsgCreateTagBudgetResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// Verify creator is a group account (groups can create tag budgets)
	if !k.IsGroupAccount(ctx, msg.Creator) {
		return nil, errorsmod.Wrap(types.ErrNotGroupAccount, "only group accounts can create tag budgets")
	}

	// Validate tag exists (O(1) direct key lookup instead of full table scan)
	_, err := k.Tag.Get(ctx, msg.Tag)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrTagNotFound, fmt.Sprintf("tag %s not found", msg.Tag))
	}

	// Check no existing active budget for this tag from this group.
	// No secondary index exists, so we iterate but break early on first match.
	budgetIter, err := k.TagBudget.Iterate(ctx, nil)
	if err == nil {
		defer budgetIter.Close()
		for ; budgetIter.Valid(); budgetIter.Next() {
			budget, _ := budgetIter.Value()
			if budget.GroupAccount == msg.Creator && budget.Tag == msg.Tag && budget.Active {
				return nil, errorsmod.Wrap(types.ErrTagBudgetAlreadyExists, fmt.Sprintf("active budget already exists for tag %s", msg.Tag))
			}
		}
	}

	// Parse and validate initial pool amount
	initialPool, ok := math.NewIntFromString(msg.InitialPool)
	if !ok || initialPool.IsNegative() || initialPool.IsZero() {
		return nil, errorsmod.Wrap(types.ErrInvalidAmount, "invalid initial pool amount")
	}

	// Transfer SPARK from group to module (escrow)
	creatorAddr, _ := sdk.AccAddressFromBech32(msg.Creator)
	escrowCoins := sdk.NewCoins(sdk.NewCoin(types.DefaultFeeDenom, initialPool))
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, creatorAddr, types.ModuleName, escrowCoins); err != nil {
		return nil, errorsmod.Wrap(err, "failed to escrow tag budget funds")
	}

	// Generate budget ID
	budgetID, err := k.TagBudgetSeq.Next(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to generate budget ID")
	}

	// Create tag budget
	budget := types.TagBudget{
		Id:           budgetID,
		GroupAccount: msg.Creator,
		Tag:          msg.Tag,
		PoolBalance:  msg.InitialPool,
		MembersOnly:  msg.MembersOnly,
		CreatedAt:    now,
		Active:       true,
	}

	if err := k.TagBudget.Set(ctx, budgetID, budget); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store tag budget")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"tag_budget_created",
			sdk.NewAttribute("budget_id", fmt.Sprintf("%d", budgetID)),
			sdk.NewAttribute("group_account", msg.Creator),
			sdk.NewAttribute("tag", msg.Tag),
			sdk.NewAttribute("initial_pool", msg.InitialPool),
			sdk.NewAttribute("members_only", fmt.Sprintf("%t", msg.MembersOnly)),
		),
	)

	return &types.MsgCreateTagBudgetResponse{}, nil
}
