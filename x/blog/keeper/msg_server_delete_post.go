package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/blog/types"

	"cosmossdk.io/store/prefix"
	errorsmod "cosmossdk.io/errors"
	"github.com/cosmos/cosmos-sdk/runtime"
	sdk "github.com/cosmos/cosmos-sdk/types"

	reptypes "sparkdream/x/rep/types"
)

func (k msgServer) DeletePost(ctx context.Context, msg *types.MsgDeletePost) (*types.MsgDeletePostResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	val, found := k.GetPost(ctx, msg.Id)
	if !found {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("post %d doesn't exist", msg.Id))
	}
	if msg.Creator != val.Creator {
		return nil, errorsmod.Wrap(types.ErrUnauthorized, "incorrect owner")
	}
	if val.Status == types.PostStatus_POST_STATUS_DELETED {
		return nil, errorsmod.Wrap(types.ErrPostDeleted, "post is already deleted")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Remove from expiry index if ephemeral
	if val.ExpiresAt > 0 {
		k.RemoveFromExpiryIndex(ctx, val.ExpiresAt, "post", val.Id)
	}

	// Remove initiative link if post references an initiative
	if val.InitiativeId > 0 && k.repKeeper != nil {
		if err := k.repKeeper.RemoveContentInitiativeLink(ctx, val.InitiativeId, int32(reptypes.StakeTargetType_STAKE_TARGET_BLOG_CONTENT), val.Id); err != nil {
			// Log but don't fail — link may already be removed
			sdkCtx.Logger().Error("failed to remove content initiative link on delete", "post_id", val.Id, "error", err)
		}
	}

	// Remove creator post index entry (key format: "{creator}/" + postID bytes)
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	creatorStore := prefix.NewStore(storeAdapter, []byte(types.CreatorPostKey))
	creatorKey := append([]byte(val.Creator+"/"), GetPostIDBytes(val.Id)...)
	creatorStore.Delete(creatorKey)

	// Remove tag secondary index entries — tombstoned posts are excluded from
	// ListPostsByTag results.
	k.removeTagIndexEntries(ctx, val.Id, val.Tags)

	// Tombstone the post instead of hard delete
	val.Title = ""
	val.Body = ""
	val.Status = types.PostStatus_POST_STATUS_DELETED
	val.UpdatedAt = sdkCtx.BlockTime().Unix()
	val.HiddenBy = ""
	val.HiddenAt = 0
	val.ExpiresAt = 0
	val.Tags = nil
	k.SetPost(ctx, val)

	// Emit event
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("blog.post.deleted",
		sdk.NewAttribute("post_id", fmt.Sprintf("%d", msg.Id)),
		sdk.NewAttribute("creator", msg.Creator),
	))

	return &types.MsgDeletePostResponse{}, nil
}
