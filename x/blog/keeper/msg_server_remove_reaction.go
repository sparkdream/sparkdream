package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/blog/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) RemoveReaction(ctx context.Context, msg *types.MsgRemoveReaction) (*types.MsgRemoveReactionResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	// Check reaction exists
	// The reaction may be stored under a pre-migration address (alias contract).
	existing, found := k.getReactionByAliases(ctx, msg.PostId, msg.ReplyId, k.recentAddressesOf(ctx, msg.Creator))
	if !found {
		return nil, errorsmod.Wrap(types.ErrReactionNotFound, fmt.Sprintf("no reaction found for %s on post %d reply %d", msg.Creator, msg.PostId, msg.ReplyId))
	}

	// Get current counts and decrement
	counts := k.GetReactionCounts(ctx, msg.PostId, msg.ReplyId)
	adjustReactionCount(&counts, existing.ReactionType, -1)
	k.SetReactionCounts(ctx, msg.PostId, msg.ReplyId, counts)

	// Remove reaction record
	k.Keeper.RemoveReaction(ctx, msg.PostId, msg.ReplyId, existing.Creator)

	// Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("blog.reaction.removed",
		sdk.NewAttribute("creator", msg.Creator),
		sdk.NewAttribute("post_id", fmt.Sprintf("%d", msg.PostId)),
		sdk.NewAttribute("reply_id", fmt.Sprintf("%d", msg.ReplyId)),
		sdk.NewAttribute("reaction_type", existing.ReactionType.String()),
	))

	return &types.MsgRemoveReactionResponse{}, nil
}
