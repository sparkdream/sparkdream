package keeper

import (
	"context"
	"strconv"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/collect/types"

	commontypes "sparkdream/x/common/types"
	reptypes "sparkdream/x/rep/types"
)

func (k msgServer) HideContent(ctx context.Context, msg *types.MsgHideContent) (*types.MsgHideContentResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	if k.repKeeper == nil {
		return nil, types.ErrNotSentinel
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockHeight := sdkCtx.BlockHeight()

	// Creator must hold an active FORUM_SENTINEL bonded role (the shared
	// moderation role across forum and collect — see commit c286f48).
	role, err := k.repKeeper.GetBondedRole(ctx, reptypes.RoleType_ROLE_TYPE_FORUM_SENTINEL, msg.Creator)
	if err != nil {
		return nil, types.ErrNotSentinel
	}
	if role.BondStatus == reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED {
		return nil, types.ErrNotSentinel
	}

	// Target must exist and be ACTIVE, PUBLIC
	coll, err := k.ValidatePublicActiveTarget(ctx, msg.TargetType, msg.TargetId)
	if err != nil {
		return nil, err
	}

	// Check for existing unresolved hide record on this target
	targetKey := HideRecordTargetCompositeKey(msg.TargetType, msg.TargetId)
	hasUnresolved := false
	_ = k.HideRecordByTarget.Walk(ctx,
		collections.NewPrefixedPairRange[string, uint64](targetKey),
		func(key collections.Pair[string, uint64]) (bool, error) {
			hr, hrErr := k.HideRecord.Get(ctx, key.K2())
			if hrErr == nil && !hr.Resolved {
				hasUnresolved = true
				return true, nil // stop walking
			}
			return false, nil
		},
	)
	if hasUnresolved {
		return nil, types.ErrAlreadyHidden
	}

	// Get params
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	// Validate ReasonCode is not UNSPECIFIED
	if msg.ReasonCode == commontypes.ModerationReason_MODERATION_REASON_UNSPECIFIED {
		return nil, errorsmod.Wrap(types.ErrInvalidFlagReason, "reason code must not be UNSPECIFIED")
	}

	// Sentinel must have available bond >= sentinel_commit_amount
	availableBond, err := k.repKeeper.GetAvailableBond(ctx, reptypes.RoleType_ROLE_TYPE_FORUM_SENTINEL, msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get sentinel bond")
	}
	if availableBond.LT(params.SentinelCommitAmount) {
		return nil, types.ErrInsufficientBondAvailable
	}

	// Set target status to HIDDEN
	switch msg.TargetType {
	case types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION:
		coll.Status = types.CollectionStatus_COLLECTION_STATUS_HIDDEN
		if err := k.Collection.Set(ctx, coll.Id, coll); err != nil {
			return nil, errorsmod.Wrap(err, "failed to update collection status")
		}
		// Update CollectionsByStatus index: remove ACTIVE, add HIDDEN
		k.CollectionsByStatus.Remove(ctx, collections.Join(int32(types.CollectionStatus_COLLECTION_STATUS_ACTIVE), coll.Id)) //nolint:errcheck
		if err := k.CollectionsByStatus.Set(ctx, collections.Join(int32(types.CollectionStatus_COLLECTION_STATUS_HIDDEN), coll.Id)); err != nil {
			return nil, errorsmod.Wrap(err, "failed to set status index")
		}
	case types.FlagTargetType_FLAG_TARGET_TYPE_ITEM:
		item, err := k.Item.Get(ctx, msg.TargetId)
		if err != nil {
			return nil, types.ErrItemNotFound
		}
		item.Status = types.ItemStatus_ITEM_STATUS_HIDDEN
		if err := k.Item.Set(ctx, item.Id, item); err != nil {
			return nil, errorsmod.Wrap(err, "failed to update item status")
		}
	}

	// Get next hide record ID (needed for bond commitment reference)
	hideRecordID, err := k.HideRecordSeq.Next(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get next hide record ID")
	}

	// Reserve sentinel_commit_amount on the sentinel's bond record. The
	// committed amount is mirrored on the HideRecord for later release/slash.
	if err := k.repKeeper.ReserveBond(ctx, reptypes.RoleType_ROLE_TYPE_FORUM_SENTINEL, msg.Creator, params.SentinelCommitAmount); err != nil {
		return nil, errorsmod.Wrap(err, "failed to reserve sentinel bond")
	}

	appealDeadline := blockHeight + params.HideExpiryBlocks

	hideRecord := types.HideRecord{
		Id:              hideRecordID,
		TargetId:        msg.TargetId,
		TargetType:      msg.TargetType,
		Sentinel:        msg.Creator,
		HiddenAt:        blockHeight,
		CommittedAmount: params.SentinelCommitAmount,
		ReasonCode:      msg.ReasonCode,
		ReasonText:      msg.ReasonText,
		AppealDeadline:  appealDeadline,
		Appealed:        false,
		Resolved:        false,
	}

	if err := k.HideRecord.Set(ctx, hideRecordID, hideRecord); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store hide record")
	}

	// Set HideRecordByTarget index (targetKey already computed above for duplicate check)
	if err := k.HideRecordByTarget.Set(ctx, collections.Join(targetKey, hideRecordID)); err != nil {
		return nil, errorsmod.Wrap(err, "failed to set hide record target index")
	}

	// Set HideRecordExpiry index
	if err := k.HideRecordExpiry.Set(ctx, collections.Join(appealDeadline, hideRecordID)); err != nil {
		return nil, errorsmod.Wrap(err, "failed to set hide record expiry")
	}

	// Clear existing CollectionFlag for this target (if any)
	flagKey := FlagCompositeKey(msg.TargetType, msg.TargetId)
	existingFlag, err := k.Flag.Get(ctx, flagKey)
	if err == nil {
		// Remove from review queue if present
		if existingFlag.InReviewQueue {
			k.FlagReviewQueue.Remove(ctx, collections.Join(int32(msg.TargetType), msg.TargetId)) //nolint:errcheck
		}
		// Remove expiry entry
		expiryBlock := existingFlag.LastFlagAt + params.FlagExpirationBlocks
		k.FlagExpiry.Remove(ctx, collections.Join(expiryBlock, flagKey)) //nolint:errcheck
		// Remove the flag itself
		k.Flag.Remove(ctx, flagKey) //nolint:errcheck
	}

	// Slash author bond on collection moderation (best-effort: log if no bond exists)
	if msg.TargetType == types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION && k.repKeeper != nil {
		if err := k.repKeeper.SlashAuthorBond(ctx, reptypes.StakeTargetType_STAKE_TARGET_COLLECTION_AUTHOR_BOND, msg.TargetId); err != nil {
			sdkCtx.Logger().Debug("author bond slash skipped", "target_id", msg.TargetId, "error", err)
		}
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("content_hidden",
		sdk.NewAttribute("hide_record_id", strconv.FormatUint(hideRecordID, 10)),
		sdk.NewAttribute("sentinel", msg.Creator),
		sdk.NewAttribute("target_id", strconv.FormatUint(msg.TargetId, 10)),
		sdk.NewAttribute("target_type", msg.TargetType.String()),
		sdk.NewAttribute("reason_code", msg.ReasonCode.String()),
		sdk.NewAttribute("appeal_deadline", strconv.FormatInt(appealDeadline, 10)),
	))

	return &types.MsgHideContentResponse{HideRecordId: hideRecordID}, nil
}
