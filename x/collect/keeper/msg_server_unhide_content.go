package keeper

import (
	"context"
	"strconv"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"sparkdream/x/collect/types"

	reptypes "sparkdream/x/rep/types"
)

// UnhideContent reverses MsgHideContent. Authorized for:
//
//   - The Commons Operations Committee / governance (council path): any
//     unresolved, unappealed hide — council hides and sentinel-hide
//     overrides alike — at any time before resolution.
//   - The sentinel who created the hide (self-correct path): own hide
//     only, within params.SentinelUnhideWindowBlocks of the hide.
//
// Both paths stop at an appeal (once appealed, the x/rep jury owns the
// outcome — deliberate deviation from forum, whose council unhide ignores
// appeal state; collect's appeal escrows a SPARK fee and opens a jury
// case).
//
// No bonded-role status check on the self-correct path: unlike hide,
// unhide is undoing harm, so a sentinel who has since been demoted or
// unbonded can (and should) still walk back their own hide.
//
// Effects on success:
//
//   - Target status flips HIDDEN -> ACTIVE (collections also move in the
//     CollectionsByStatus index).
//   - The author bond and per-tag rep penalty snapshotted on the
//     HideRecord are restored (mint-back via RestoreAuthorBond +
//     AddReputation; best-effort, mirroring the hide-side slash).
//   - The HideRecord is marked Resolved + SelfCorrected, but the
//     HideRecordExpiry entry is retained and the sentinel's committed
//     bond stays reserved until the original appeal_deadline. Together
//     with the daily hide cap (max_hides_per_sentinel_per_day, whose
//     slot this unhide does NOT refund), that makes hide/unhide cycling
//     doubly expensive: each cycle burns a daily slot AND locks another
//     commit until the original deadline.
func (k msgServer) UnhideContent(ctx context.Context, msg *types.MsgUnhideContent) (*types.MsgUnhideContentResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockHeight := sdkCtx.BlockHeight()

	hr, err := k.HideRecord.Get(ctx, msg.HideRecordId)
	if err != nil {
		return nil, types.ErrHideRecordNotFound
	}
	if hr.Resolved {
		return nil, types.ErrHideRecordResolved
	}
	if hr.Appealed {
		return nil, types.ErrHideAppealed
	}
	// Authorization: council anytime (any unresolved, unappealed hide —
	// council hides and sentinel-hide overrides alike), OR the original
	// sentinel within the self-correct window. See
	// docs/x-collect-spec.md (MsgUnhideContent).
	isCouncil := k.commonsKeeper != nil &&
		k.commonsKeeper.IsCouncilAuthorized(ctx, msg.Creator, "commons", "operations")

	isSelfCorrect := false
	if !isCouncil {
		if hr.Sentinel == "" || hr.Sentinel != msg.Creator {
			return nil, errorsmod.Wrap(sdkerrors.ErrUnauthorized,
				"only the sentinel who hid the content, the Commons Operations Committee, or governance can unhide")
		}

		params, err := k.Params.Get(ctx)
		if err != nil {
			return nil, errorsmod.Wrap(err, "failed to get params")
		}
		if blockHeight-hr.HiddenAt > params.SentinelUnhideWindowBlocks {
			return nil, errorsmod.Wrapf(types.ErrUnhideWindowExpired,
				"sentinel unhide window is %d blocks", params.SentinelUnhideWindowBlocks)
		}
		isSelfCorrect = true
	}

	// Restore the target to ACTIVE. If the target no longer exists (deleted
	// while hidden — rare, ErrCannotDeleteHidden blocks owner deletes), fail
	// without resolving the record; the expiry handler cleans it up.
	switch hr.TargetType {
	case types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION:
		coll, collErr := k.Collection.Get(ctx, hr.TargetId)
		if collErr != nil {
			return nil, types.ErrCollectionNotFound
		}
		if coll.Status == types.CollectionStatus_COLLECTION_STATUS_HIDDEN {
			oldStatus := coll.Status
			coll.Status = types.CollectionStatus_COLLECTION_STATUS_ACTIVE
			if err := k.MoveCollectionStatusIndex(ctx, oldStatus, coll.Pinned, coll.Status, coll.Pinned, coll.Id); err != nil {
				return nil, errorsmod.Wrap(err, "failed to move status index")
			}
			if err := k.Collection.Set(ctx, coll.Id, coll); err != nil {
				return nil, errorsmod.Wrap(err, "failed to update collection status")
			}
		}
	case types.FlagTargetType_FLAG_TARGET_TYPE_ITEM:
		item, itemErr := k.Item.Get(ctx, hr.TargetId)
		if itemErr != nil {
			return nil, types.ErrItemNotFound
		}
		if item.Status == types.ItemStatus_ITEM_STATUS_HIDDEN {
			item.Status = types.ItemStatus_ITEM_STATUS_ACTIVE
			if err := k.Item.Set(ctx, item.Id, item); err != nil {
				return nil, errorsmod.Wrap(err, "failed to update item status")
			}
		}
	}

	// Restore the author-side penalties from the snapshots taken at hide
	// time (collection targets only — items carry neither).
	authorBondRestored, repPenaltyRestored := k.restoreAuthorPenalties(ctx, hr)

	// Close the record.
	//
	//   - Sentinel self-correct: retain the bond reservation — keep the
	//     HideRecordExpiry entry so the EndBlocker releases CommittedAmount
	//     at the original appeal_deadline (anti hide/unhide cycling — see
	//     docs/x-collect-spec.md, MsgUnhideContent).
	//   - Council unhide: not self-serve, so no retention — release any
	//     committed bond immediately (a council override of a sentinel hide
	//     is not the sentinel's doing) and remove the expiry entry. The
	//     sentinel's daily hide slot stays consumed either way.
	hr.Resolved = true
	hr.SelfCorrected = isSelfCorrect
	if err := k.HideRecord.Set(ctx, hr.Id, hr); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update hide record")
	}
	if !isSelfCorrect {
		if k.repKeeper != nil && hr.CommittedAmount.IsPositive() {
			if err := k.repKeeper.ReleaseBond(ctx, reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, hr.Sentinel, hr.CommittedAmount); err != nil {
				sdkCtx.Logger().Warn("council unhide: release sentinel bond failed",
					"sentinel", hr.Sentinel, "hide_record_id", hr.Id, "error", err)
			}
		}
		if err := k.HideRecordExpiry.Remove(ctx, collections.Join(hr.AppealDeadline, hr.Id)); err != nil {
			sdkCtx.Logger().Warn("council unhide: remove expiry entry failed",
				"hide_record_id", hr.Id, "error", err)
		}
	}

	attrs := []sdk.Attribute{
		sdk.NewAttribute("hide_record_id", strconv.FormatUint(hr.Id, 10)),
		sdk.NewAttribute("target_id", strconv.FormatUint(hr.TargetId, 10)),
		sdk.NewAttribute("target_type", hr.TargetType.String()),
		sdk.NewAttribute("unhidden_by", msg.Creator),
		sdk.NewAttribute("is_council", strconv.FormatBool(isCouncil)),
		sdk.NewAttribute("is_self_correct", strconv.FormatBool(isSelfCorrect)),
		sdk.NewAttribute("author_bond_restored", strconv.FormatBool(authorBondRestored)),
		sdk.NewAttribute("rep_penalty_restored", strconv.FormatBool(repPenaltyRestored)),
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("content_unhidden", attrs...))

	return &types.MsgUnhideContentResponse{}, nil
}
