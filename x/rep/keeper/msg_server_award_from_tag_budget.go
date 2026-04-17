package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

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

	budget, err := k.TagBudget.Get(ctx, msg.BudgetId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrTagBudgetNotFound, fmt.Sprintf("budget %d not found", msg.BudgetId))
	}

	if !budget.Active {
		return nil, errorsmod.Wrap(types.ErrTagBudgetNotActive, "tag budget is not active")
	}

	if !k.IsGroupMember(ctx, budget.GroupAccount, msg.Creator) {
		return nil, errorsmod.Wrap(types.ErrNotGroupMember, "only group members can award from tag budget")
	}

	// Post lookup is delegated to x/forum via the narrow ForumKeeper surface;
	// x/rep doesn't own post state directly.
	if k.late.forumKeeper == nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, "forum keeper not wired")
	}

	author, err := k.late.forumKeeper.GetPostAuthor(ctx, msg.PostId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("post %d not found", msg.PostId))
	}

	postTags, err := k.late.forumKeeper.GetPostTags(ctx, msg.PostId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("post %d not found", msg.PostId))
	}

	hasTag := false
	for _, tag := range postTags {
		if tag == budget.Tag {
			hasTag = true
			break
		}
	}
	if !hasTag {
		return nil, errorsmod.Wrap(types.ErrInvalidTag, fmt.Sprintf("post does not have tag %s", budget.Tag))
	}

	if budget.MembersOnly {
		authorBytes, err := k.addressCodec.StringToBytes(author)
		if err != nil || !k.IsMember(ctx, sdk.AccAddress(authorBytes)) {
			return nil, errorsmod.Wrap(types.ErrNotMember, "budget is members-only and post author is not a member")
		}
	}

	awardAmount, ok := math.NewIntFromString(msg.Amount)
	if !ok || awardAmount.IsNegative() || awardAmount.IsZero() {
		return nil, errorsmod.Wrap(types.ErrInvalidAmount, "invalid award amount")
	}

	poolBalance, _ := math.NewIntFromString(budget.PoolBalance)
	if awardAmount.GT(poolBalance) {
		return nil, errorsmod.Wrap(types.ErrTagBudgetInsufficient, "award amount exceeds pool balance")
	}

	newBalance := poolBalance.Sub(awardAmount)
	budget.PoolBalance = newBalance.String()

	if err := k.TagBudget.Set(ctx, msg.BudgetId, budget); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update tag budget")
	}

	awardID, err := k.TagBudgetAwardSeq.Next(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to generate award ID")
	}

	award := types.TagBudgetAward{
		Id:        awardID,
		BudgetId:  msg.BudgetId,
		PostId:    msg.PostId,
		Recipient: author,
		Amount:    msg.Amount,
		Reason:    msg.Reason,
		AwardedAt: now,
		AwardedBy: msg.Creator,
	}

	if err := k.TagBudgetAward.Set(ctx, awardID, award); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store tag budget award")
	}

	recipientAddr, _ := sdk.AccAddressFromBech32(author)
	awardCoins := sdk.NewCoins(sdk.NewCoin(types.TagBudgetFeeDenom, awardAmount))
	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, recipientAddr, awardCoins); err != nil {
		return nil, errorsmod.Wrap(err, "failed to transfer award funds")
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"tag_budget_award",
			sdk.NewAttribute("award_id", fmt.Sprintf("%d", awardID)),
			sdk.NewAttribute("budget_id", fmt.Sprintf("%d", msg.BudgetId)),
			sdk.NewAttribute("post_id", fmt.Sprintf("%d", msg.PostId)),
			sdk.NewAttribute("recipient", author),
			sdk.NewAttribute("amount", msg.Amount),
			sdk.NewAttribute("awarded_by", msg.Creator),
			sdk.NewAttribute("reason", msg.Reason),
		),
	)

	return &types.MsgAwardFromTagBudgetResponse{}, nil
}
