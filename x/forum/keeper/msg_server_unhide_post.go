package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	reptypes "sparkdream/x/rep/types"
)

// UnhidePost reverses MsgHidePost. Authorized for:
//
//   - The sentinel who originally hid the post, but only within
//     params.SentinelUnhideWindow seconds of the original hide
//     (self-correction grace window, analogous to the author's
//     edit_max_window).
//   - The Commons Operations Committee / governance (council override),
//     at any time and regardless of whether a HideRecord exists (gov
//     hides skip HideRecord creation).
//
// Post author cannot self-unhide — that would defeat moderation.
//
// Effects on success:
//
//   - post.Status flips HIDDEN → ACTIVE, HiddenBy / HiddenAt cleared.
//   - If a HideRecord exists for the post (sentinel hide), it is removed,
//     the sentinel's reserved bond (CommittedAmount) is released, and
//     PendingHideCount on SentinelActivity is decremented.
//   - Refuses with ErrCategoryNotFound if the post's parent category was
//     deleted while the post was hidden — see the INVARIANT comment on
//     Keeper.HasPostInCategory in keeper.go.
//
// Author bond restoration: if the original MsgHidePost recorded an
// author_bond_amount on the HideRecord, the reversal mints that amount
// back to the author and re-locks it as a fresh author bond on the post.
// Net DREAM supply change across slash→restore is zero. This works
// uniformly for both sentinel hides (Sentinel == creator) and gov hides
// (Sentinel == "" marker) — both now write a HideRecord with the
// snapshotted bond amount.
func (k msgServer) UnhidePost(ctx context.Context, msg *types.MsgUnhidePost) (*types.MsgUnhidePostResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	params, err := k.Params.Get(ctx)
	if err != nil {
		params = types.DefaultParams()
	}
	if params.ModerationPaused {
		return nil, types.ErrModerationPaused
	}

	post, err := k.Post.Get(ctx, msg.PostId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("post %d not found", msg.PostId))
	}
	if post.Status != types.PostStatus_POST_STATUS_HIDDEN {
		return nil, errorsmod.Wrap(types.ErrPostNotHidden, "can only unhide posts that are currently hidden")
	}

	// Dangling reference guard — mirror MsgUnarchiveThread. The category
	// may have been deleted while the post was hidden (HasPostInCategory
	// skips non-ACTIVE posts). Flipping back to ACTIVE here without this
	// check would leave an ACTIVE post pointing at a non-existent category.
	if k.commonsKeeper == nil || !k.commonsKeeper.HasCategory(ctx, post.CategoryId) {
		return nil, errorsmod.Wrapf(types.ErrCategoryNotFound,
			"cannot unhide post %d: parent category %d has been deleted",
			msg.PostId, post.CategoryId)
	}

	// Authorization: council/gov anytime, OR original sentinel within window.
	isCouncil := k.isCouncilAuthorized(ctx, msg.Creator, "commons", "operations")

	hideRecord, recordErr := k.HideRecord.Get(ctx, msg.PostId)
	haveRecord := recordErr == nil

	isSelfCorrect := false
	if !isCouncil {
		if !haveRecord || hideRecord.Sentinel != msg.Creator {
			return nil, errorsmod.Wrap(sdkerrors.ErrUnauthorized,
				"only the sentinel who hid the post, the Commons Operations Committee, or governance can unhide")
		}
		window := params.SentinelUnhideWindow
		if window == 0 {
			window = types.DefaultSentinelUnhideWindow
		}
		if now-hideRecord.HiddenAt > window {
			return nil, errorsmod.Wrapf(types.ErrUnhideWindowExpired,
				"sentinel unhide window is %d seconds", window)
		}
		isSelfCorrect = true
	}

	// Flip post back to ACTIVE and clear hide metadata.
	post.Status = types.PostStatus_POST_STATUS_ACTIVE
	post.HiddenBy = ""
	post.HiddenAt = 0
	if err := k.Post.Set(ctx, msg.PostId, post); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update post")
	}

	// HideRecord cleanup + sentinel-side state reversal. Gov hides skip
	// HideRecord creation entirely, so a missing record on a council
	// unhide is normal — nothing to clean up.
	if haveRecord {
		sentinelAddr := hideRecord.Sentinel
		if committed, ok := math.NewIntFromString(hideRecord.CommittedAmount); ok && committed.IsPositive() && k.repKeeper != nil {
			if err := k.repKeeper.ReleaseBond(ctx, reptypes.RoleType_ROLE_TYPE_FORUM_SENTINEL, sentinelAddr, committed); err != nil {
				sdkCtx.Logger().Warn("release sentinel bond on unhide failed",
					"sentinel", sentinelAddr, "post_id", msg.PostId, "error", err)
			}
		}

		// Restore the author bond that MsgHidePost slashed. No-op if the
		// HideRecord didn't capture an amount (empty string), if the amount
		// is zero, or if a bond already exists on the post (idempotent on
		// the rep side). Mints DREAM to the author to replace what was
		// burned and re-locks it as a fresh bond.
		if k.repKeeper != nil && hideRecord.AuthorBondAmount != "" {
			if amt, ok := math.NewIntFromString(hideRecord.AuthorBondAmount); ok && amt.IsPositive() {
				authorAddr, addrErr := sdk.AccAddressFromBech32(post.Author)
				if addrErr != nil {
					sdkCtx.Logger().Warn("restore author bond on unhide: bad author address",
						"post_id", msg.PostId, "author", post.Author, "error", addrErr)
				} else if err := k.repKeeper.RestoreAuthorBond(ctx, authorAddr, reptypes.StakeTargetType_STAKE_TARGET_FORUM_AUTHOR_BOND, msg.PostId, amt); err != nil {
					sdkCtx.Logger().Warn("restore author bond on unhide failed",
						"post_id", msg.PostId, "author", post.Author, "error", err)
				}
			}
		}

		// Decrement pending-hide count. No upheld/overturned bump: this is
		// neither — the sentinel walked it back voluntarily (self-correct)
		// or the council overrode it before any appeal was resolved.
		if local, err := k.SentinelActivity.Get(ctx, sentinelAddr); err == nil {
			if local.PendingHideCount > 0 {
				local.PendingHideCount--
				if err := k.SentinelActivity.Set(ctx, sentinelAddr, local); err != nil {
					sdkCtx.Logger().Warn("update sentinel activity on unhide failed",
						"sentinel", sentinelAddr, "post_id", msg.PostId, "error", err)
				}
			}
		}

		if err := k.HideRecord.Remove(ctx, msg.PostId); err != nil {
			sdkCtx.Logger().Warn("remove hide record on unhide failed",
				"post_id", msg.PostId, "error", err)
		}
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"post_unhidden",
			sdk.NewAttribute("post_id", fmt.Sprintf("%d", msg.PostId)),
			sdk.NewAttribute("unhidden_by", msg.Creator),
			sdk.NewAttribute("is_council", fmt.Sprintf("%t", isCouncil)),
			sdk.NewAttribute("is_self_correct", fmt.Sprintf("%t", isSelfCorrect)),
		),
	)

	return &types.MsgUnhidePostResponse{}, nil
}
