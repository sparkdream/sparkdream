package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

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

	if !k.IsGroupAccount(ctx, msg.Creator) {
		return nil, errorsmod.Wrap(types.ErrNotGroupAccount, "only group accounts can create tag budgets")
	}

	exists, err := k.TagExists(ctx, msg.Tag)
	if err != nil {
		return nil, errorsmod.Wrapf(err, "failed to check tag %q", msg.Tag)
	}
	if !exists {
		return nil, errorsmod.Wrap(types.ErrTagNotFound, fmt.Sprintf("tag %s not found", msg.Tag))
	}

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

	initialPool, ok := math.NewIntFromString(msg.InitialPool)
	if !ok || initialPool.IsNegative() || initialPool.IsZero() {
		return nil, errorsmod.Wrap(types.ErrInvalidAmount, "invalid initial pool amount")
	}

	creatorAddr, _ := sdk.AccAddressFromBech32(msg.Creator)
	escrowCoins := sdk.NewCoins(sdk.NewCoin(types.TagBudgetFeeDenom, initialPool))
	if err := k.bankKeeper.SendCoins(ctx, creatorAddr, TagBudgetEscrowAddress(), escrowCoins); err != nil {
		return nil, errorsmod.Wrap(err, "failed to escrow tag budget funds")
	}

	budgetID, err := k.TagBudgetSeq.Next(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to generate budget ID")
	}

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
