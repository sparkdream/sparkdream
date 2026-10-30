package keeper

import (
	"context"
	"fmt"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
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

	// A group member cannot award the group's SPARK to their own post
	// (REP-E-19). Each award is capped at 25% of the pool, but one member
	// acting alone could otherwise take 1-0.75^k of it with k posts of
	// their own. Compared canonically, so a migrated author is still the
	// awarder.
	if k.SameMember(ctx, author, msg.Creator) {
		return nil, errorsmod.Wrap(types.ErrSelfAward, "the awarder wrote this post")
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
		if err != nil || !k.IsActiveMember(ctx, sdk.AccAddress(authorBytes)) {
			return nil, errorsmod.Wrap(types.ErrNotMember, "budget is members-only and post author is not an active member")
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

	// Cap a single award at 25% of the current pool balance so a single post can't drain the budget.
	maxSingle := poolBalance.QuoRaw(4)
	if maxSingle.IsPositive() && awardAmount.GT(maxSingle) {
		return nil, errorsmod.Wrapf(types.ErrTagBudgetInsufficient,
			"award amount %s exceeds per-call cap of 25%% of pool (%s)", awardAmount.String(), maxSingle.String())
	}

	// Enforce a per-(budget, post) cooldown to prevent a single post from being awarded repeatedly in a short window.
	budgetPostKey := collections.Join(msg.BudgetId, msg.PostId)
	if lastHeight, err := k.TagBudgetAwardByPost.Get(ctx, budgetPostKey); err == nil {
		if sdkCtx.BlockHeight()-lastHeight < types.TagBudgetAwardCooldownBlocks {
			return nil, errorsmod.Wrapf(types.ErrTagBudgetInsufficient,
				"post %d was awarded from budget %d at height %d; cooldown of %d blocks not yet elapsed",
				msg.PostId, msg.BudgetId, lastHeight, types.TagBudgetAwardCooldownBlocks)
		}
	}

	// Per-awarder epoch cap: one group member may award at most
	// tag_budget_awarder_epoch_cap_bps of the pool as it stood before their
	// own awards this epoch, however many posts (by other authors) they
	// spread it over. Keyed by the canonical awarder so a migrated key does
	// not get a fresh allowance.
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}
	epoch, err := k.GetCurrentEpoch(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get current epoch")
	}
	awarder := k.Canonical(ctx, msg.Creator)
	usageKey := collections.Join3(msg.BudgetId, awarder, epoch)
	awardedThisEpoch, err := k.TagBudgetAwarderEpoch.Get(ctx, usageKey)
	if err != nil {
		awardedThisEpoch = math.ZeroInt()
	}
	capBps := params.TagBudgetAwarderEpochCapBpsOrDefault()
	awarderCap := poolBalance.Add(awardedThisEpoch).MulRaw(commontypes.Saturate[int64](capBps)).QuoRaw(10000)
	if awardedThisEpoch.Add(awardAmount).GT(awarderCap) {
		return nil, errorsmod.Wrapf(types.ErrTagBudgetAwarderCap,
			"awarder %s already awarded %s from budget %d this epoch; cap is %s (%d bps of the pool)",
			msg.Creator, awardedThisEpoch.String(), msg.BudgetId, awarderCap.String(), capBps)
	}
	if err := k.recordAwarderEpochUsage(ctx, msg.BudgetId, awarder, epoch, awardedThisEpoch.Add(awardAmount)); err != nil {
		return nil, errorsmod.Wrap(err, "failed to record awarder epoch usage")
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

	if err := k.TagBudgetAwardByPost.Set(ctx, budgetPostKey, sdkCtx.BlockHeight()); err != nil {
		return nil, errorsmod.Wrap(err, "failed to record tag budget award cooldown")
	}

	recipientAddr, _ := sdk.AccAddressFromBech32(author)
	awardCoins := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), awardAmount))
	if err := k.bankKeeper.SendCoins(ctx, TagBudgetEscrowAddress(), recipientAddr, awardCoins); err != nil {
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

// recordAwarderEpochUsage stores the awarder's running total for the epoch
// and drops their entries for earlier epochs of the same budget, so the map
// holds at most one row per (budget, awarder).
func (k Keeper) recordAwarderEpochUsage(ctx context.Context, budgetID uint64, awarder string, epoch int64, total math.Int) error {
	var stale []collections.Triple[uint64, string, int64]
	rng := collections.NewSuperPrefixedTripleRange[uint64, string, int64](budgetID, awarder)
	if err := k.TagBudgetAwarderEpoch.Walk(ctx, rng, func(key collections.Triple[uint64, string, int64], _ math.Int) (bool, error) {
		if key.K3() < epoch {
			stale = append(stale, key)
		}
		return false, nil
	}); err != nil {
		return err
	}
	for _, key := range stale {
		if err := k.TagBudgetAwarderEpoch.Remove(ctx, key); err != nil {
			return err
		}
	}
	return k.TagBudgetAwarderEpoch.Set(ctx, collections.Join3(budgetID, awarder, epoch), total)
}
