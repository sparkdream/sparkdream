package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) AwardFromTagBudget(ctx context.Context, msg *types.MsgAwardFromTagBudget) (*types.MsgAwardFromTagBudgetResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// Load tag budget
	budget, err := k.TagBudget.Get(ctx, msg.BudgetId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrTagBudgetNotFound, fmt.Sprintf("budget %d not found", msg.BudgetId))
	}

	// Check budget is active
	if !budget.Active {
		return nil, errorsmod.Wrap(types.ErrTagBudgetNotActive, "tag budget is not active")
	}

	// Verify creator is a member of the group
	if !k.IsGroupMember(ctx, budget.GroupAccount, msg.Creator) {
		return nil, errorsmod.Wrap(types.ErrNotGroupMember, "only group members can award from tag budget")
	}

	// Load the post being awarded
	post, err := k.Post.Get(ctx, msg.PostId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("post %d not found", msg.PostId))
	}

	// Check post has the tag
	hasTag := false
	for _, tag := range post.Tags {
		if tag == budget.Tag {
			hasTag = true
			break
		}
	}
	if !hasTag {
		return nil, errorsmod.Wrap(types.ErrInvalidTag, fmt.Sprintf("post does not have tag %s", budget.Tag))
	}

	// If members_only, check recipient is a member
	if budget.MembersOnly && !k.IsMember(ctx, post.Author) {
		return nil, errorsmod.Wrap(types.ErrNotMember, "budget is members-only and post author is not a member")
	}

	// Parse and validate award amount
	awardAmount, ok := math.NewIntFromString(msg.Amount)
	if !ok || awardAmount.IsNegative() || awardAmount.IsZero() {
		return nil, errorsmod.Wrap(types.ErrInvalidAmount, "invalid award amount")
	}

	// Check sufficient balance in pool
	poolBalance, _ := math.NewIntFromString(budget.PoolBalance)
	if awardAmount.GT(poolBalance) {
		return nil, errorsmod.Wrap(types.ErrTagBudgetInsufficient, "award amount exceeds pool balance")
	}

	// Deduct from pool
	newBalance := poolBalance.Sub(awardAmount)
	budget.PoolBalance = newBalance.String()

	if err := k.TagBudget.Set(ctx, msg.BudgetId, budget); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update tag budget")
	}

	// Generate award ID
	awardID, err := k.TagBudgetAwardSeq.Next(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to generate award ID")
	}

	// Create award record
	award := types.TagBudgetAward{
		Id:        awardID,
		BudgetId:  msg.BudgetId,
		PostId:    msg.PostId,
		Recipient: post.Author,
		Amount:    msg.Amount,
		Reason:    msg.Reason,
		AwardedAt: now,
		AwardedBy: msg.Creator,
	}

	if err := k.TagBudgetAward.Set(ctx, awardID, award); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store tag budget award")
	}

	// Transfer SPARK from module to recipient
	recipientAddr, _ := sdk.AccAddressFromBech32(post.Author)
	awardCoins := sdk.NewCoins(sdk.NewCoin(types.DefaultFeeDenom, awardAmount))
	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, recipientAddr, awardCoins); err != nil {
		return nil, errorsmod.Wrap(err, "failed to transfer award funds")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"tag_budget_award",
			sdk.NewAttribute("award_id", fmt.Sprintf("%d", awardID)),
			sdk.NewAttribute("budget_id", fmt.Sprintf("%d", msg.BudgetId)),
			sdk.NewAttribute("post_id", fmt.Sprintf("%d", msg.PostId)),
			sdk.NewAttribute("recipient", post.Author),
			sdk.NewAttribute("amount", msg.Amount),
			sdk.NewAttribute("awarded_by", msg.Creator),
			sdk.NewAttribute("reason", msg.Reason),
		),
	)

	return &types.MsgAwardFromTagBudgetResponse{}, nil
}
