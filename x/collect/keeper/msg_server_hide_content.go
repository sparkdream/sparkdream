package keeper

import (
	"context"
	"strconv"
	"strings"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/collect/types"

	commontypes "sparkdream/x/common/types"
	reptypes "sparkdream/x/rep/types"
)

func (k msgServer) HideContent(ctx context.Context, msg *types.MsgHideContent) (*types.MsgHideContentResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockHeight := sdkCtx.BlockHeight()

	// Sentinel eligibility: an active CONTENT_SENTINEL bonded role (the shared
	// moderation role across forum and collect), gated
	// through rep's shared EligibleForRole (same check forum uses, including
	// the UNBONDING staying-bond rule — collect previously accepted UNBONDING
	// sentinels whose staying bond no longer covered the role minimum).
	sentinelEligible := false
	sentinelErr := error(types.ErrNotSentinel)
	if k.repKeeper != nil {
		if _, roleErr := k.repKeeper.EligibleForRole(ctx, reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, msg.Creator); roleErr == nil {
			sentinelEligible = true
		} else {
			sentinelErr = errorsmod.Wrap(types.ErrNotSentinel, roleErr.Error())
		}
	}

	// Council authorization: x/gov authority, Commons Council policy address,
	// or Operations Committee member.
	isCouncil := k.commonsKeeper != nil &&
		k.commonsKeeper.IsCouncilAuthorized(ctx, msg.Creator, "commons", "operations")

	// isGovAuthority is the resolved decision: take the council path. See
	// the authority-selection notes in docs/x-collect-spec.md (MsgHideContent).
	isGovAuthority, err := resolveModerationAuthority(msg.Authority, sentinelEligible, sentinelErr, isCouncil)
	if err != nil {
		return nil, err
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

	if !isGovAuthority {
		// Shared overturn cooldown: a sentinel who just lost an appeal (on
		// EITHER moderation surface — the RoleActivity record is shared and
		// rep-owned) cannot open new hides until the cooldown passes. Matches
		// forum's own action-time gate.
		if until := k.repKeeper.RoleOverturnCooldownUntil(ctx, reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, msg.Creator); until > sdkCtx.BlockTime().Unix() {
			return nil, errorsmod.Wrapf(types.ErrSentinelCooldown, "cooldown until %d", until)
		}

		// Per-sentinel daily hide cap (block-height day, same counter family
		// as pins/flags/reactions). The bond reservation below throttles
		// concurrent hides; this caps outright hide spam by a well-funded
		// sentinel. A later self-correct unhide does NOT refund the day's
		// slot. The council path skips both (matches forum's gov path) —
		// council accountability is political, not bonded.
		if err := k.checkDailyLimit(ctx, msg.Creator, blockHeight, "sentinel_hide", params.MaxHidesPerSentinelPerDay); err != nil {
			return nil, errorsmod.Wrap(err, "sentinel daily hide cap")
		}

		// Sentinel must have available bond >= sentinel_commit_amount
		availableBond, bondErr := k.repKeeper.GetAvailableBond(ctx, reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, msg.Creator)
		if bondErr != nil {
			return nil, errorsmod.Wrap(bondErr, "failed to get sentinel bond")
		}
		if availableBond.LT(params.SentinelCommitAmount) {
			return nil, types.ErrInsufficientBondAvailable
		}
	}

	// Set target status to HIDDEN
	switch msg.TargetType {
	case types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION:
		coll.Status = types.CollectionStatus_COLLECTION_STATUS_HIDDEN
		if err := k.Collection.Set(ctx, coll.Id, coll); err != nil {
			return nil, errorsmod.Wrap(err, "failed to update collection status")
		}
		// Update CollectionsByStatus index: ACTIVE → HIDDEN (pinned unchanged).
		if err := k.MoveCollectionStatusIndex(ctx,
			types.CollectionStatus_COLLECTION_STATUS_ACTIVE, coll.Pinned,
			types.CollectionStatus_COLLECTION_STATUS_HIDDEN, coll.Pinned, coll.Id); err != nil {
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

	// Sentinel path: reserve sentinel_commit_amount on the sentinel's bond
	// record; the committed amount is mirrored on the HideRecord for later
	// release/slash. Council path: no bond — Sentinel stays "" (the gov-hide
	// marker convention shared with forum) and CommittedAmount stays zero,
	// which the appeal/expiry handlers already skip via IsPositive() guards.
	hiddenBy := ""
	committedAmount := math.ZeroInt()
	if !isGovAuthority {
		if err := k.repKeeper.ReserveBond(ctx, reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, msg.Creator, params.SentinelCommitAmount); err != nil {
			return nil, errorsmod.Wrap(err, "failed to reserve sentinel bond")
		}
		hiddenBy = msg.Creator
		committedAmount = params.SentinelCommitAmount

		// Credit the collect hide on rep's shared RoleActivity record so
		// reward-epoch activity sees collect moderation work. Best-effort.
		if err := k.repKeeper.RecordRoleAction(ctx, reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, msg.Creator, reptypes.ActionKindCollectHide); err != nil {
			sdkCtx.Logger().Warn("record collect hide activity failed",
				"sentinel", msg.Creator, "error", err)
		}
	}

	appealDeadline := blockHeight + params.HideExpiryBlocks

	// Snapshot the author-side penalties BEFORE they are applied below, so a
	// hide reversal (self-correct, jury overturn, appeal timeout) can restore
	// exactly what this hide took, regardless of later param or tag changes.
	// The rep snapshot records the ACTUAL per-tag deduction,
	// min(current_score, penalty) — DeductReputation floors at zero, so
	// restoring the raw param would mint rep from nothing on every
	// hide/reversal cycle for authors with less rep than the penalty.
	authorBondAmount := math.ZeroInt()
	authorRepPenalty := math.LegacyZeroDec()
	var repPenaltyTags []string
	var repPenaltyAmounts []string
	if msg.TargetType == types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION && k.repKeeper != nil {
		if bond, bondErr := k.repKeeper.GetAuthorBond(ctx, reptypes.StakeTargetType_STAKE_TARGET_COLLECTION_AUTHOR_BOND, msg.TargetId); bondErr == nil {
			authorBondAmount = bond.Amount
		}
		if params.AuthorRepPenalty.IsPositive() && len(coll.Tags) > 0 {
			authorRepPenalty = params.AuthorRepPenalty
			repPenaltyTags = coll.Tags
			scores, scoresErr := k.repKeeper.GetReputationScores(ctx, coll.Owner)
			for _, tag := range coll.Tags {
				actual := math.LegacyZeroDec()
				if scoresErr == nil {
					if scoreStr, ok := scores[tag]; ok {
						if current, parseErr := math.LegacyNewDecFromStr(scoreStr); parseErr == nil {
							actual = math.LegacyMinDec(current, params.AuthorRepPenalty)
						}
					}
				}
				repPenaltyAmounts = append(repPenaltyAmounts, actual.String())
			}
		}
	}

	hideRecord := types.HideRecord{
		Id:                hideRecordID,
		TargetId:          msg.TargetId,
		TargetType:        msg.TargetType,
		Sentinel:          hiddenBy,
		HiddenAt:          blockHeight,
		CommittedAmount:   committedAmount,
		ReasonCode:        msg.ReasonCode,
		ReasonText:        msg.ReasonText,
		AppealDeadline:    appealDeadline,
		Appealed:          false,
		Resolved:          false,
		SelfCorrected:     false,
		AuthorBondAmount:  authorBondAmount,
		AuthorRepPenalty:  authorRepPenalty,
		RepPenaltyTags:    repPenaltyTags,
		RepPenaltyAmounts: repPenaltyAmounts,
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
	// and apply a matching per-tag rep deduction so the author's score on the
	// collection's topic tags reflects the moderation event. Every reversal
	// that favors the author — sentinel self-correct (MsgUnhideContent),
	// jury overturn, appeal timeout — restores both from the snapshots on
	// the HideRecord above (restoreAuthorPenalties). Only the
	// unappealed-expiry deletion path leaves them burned.
	authorRepApplied := false
	if msg.TargetType == types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION && k.repKeeper != nil {
		if err := k.repKeeper.SlashAuthorBond(ctx, reptypes.StakeTargetType_STAKE_TARGET_COLLECTION_AUTHOR_BOND, msg.TargetId); err != nil {
			sdkCtx.Logger().Debug("author bond slash skipped", "target_id", msg.TargetId, "error", err)
		}
		if ownerAddr, ownerErr := k.addressCodec.StringToBytes(coll.Owner); ownerErr == nil &&
			params.AuthorRepPenalty.IsPositive() && len(coll.Tags) > 0 {
			k.deductRepPerTag(ctx, ownerAddr, coll.Tags, params.AuthorRepPenalty)
			authorRepApplied = true
		}
	}

	// Emit event. "sentinel" mirrors HideRecord.Sentinel ("" for council
	// hides); "creator" always carries the signing account.
	authorityLabel := "sentinel"
	if isGovAuthority {
		authorityLabel = "council"
	}
	hideAttrs := []sdk.Attribute{
		sdk.NewAttribute("hide_record_id", strconv.FormatUint(hideRecordID, 10)),
		sdk.NewAttribute("sentinel", hiddenBy),
		sdk.NewAttribute("creator", msg.Creator),
		sdk.NewAttribute("authority", authorityLabel),
		sdk.NewAttribute("target_id", strconv.FormatUint(msg.TargetId, 10)),
		sdk.NewAttribute("target_type", msg.TargetType.String()),
		sdk.NewAttribute("reason_code", msg.ReasonCode.String()),
		sdk.NewAttribute("appeal_deadline", strconv.FormatInt(appealDeadline, 10)),
	}
	if authorRepApplied {
		hideAttrs = append(hideAttrs,
			sdk.NewAttribute("author", coll.Owner),
			sdk.NewAttribute("rep_penalty", params.AuthorRepPenalty.String()),
			sdk.NewAttribute("rep_penalty_tags", strings.Join(coll.Tags, ",")),
		)
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("content_hidden", hideAttrs...))

	return &types.MsgHideContentResponse{HideRecordId: hideRecordID}, nil
}
