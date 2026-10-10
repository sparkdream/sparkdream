package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	commontypes "sparkdream/x/common/types"
	reptypes "sparkdream/x/rep/types"

	"sparkdream/x/artifact/types"
)

// targetKey returns the HideByTarget key; token_id 0 means the class.
func targetKey(classID, tokenID uint64) U64Pair { return collections.Join(classID, tokenID) }

// setTargetStatus sets the status of the hide target. For a class hide it
// also queues the class's listings for cancellation (or, when restoring,
// drops the queue). For a token hide it cancels the token's listing.
func (k Keeper) setTargetStatus(ctx context.Context, hr types.HideRecord, status types.ContentStatus) error {
	switch hr.TargetKind {
	case types.HideTargetKind_HIDE_TARGET_KIND_CLASS:
		c, err := k.getClass(ctx, hr.ClassId)
		if err != nil {
			return err
		}
		c.Status = status
		if err := k.saveClass(ctx, c); err != nil {
			return err
		}
		if status == types.ContentStatus_CONTENT_STATUS_HIDDEN {
			return k.ClassCancelQueue.Set(ctx, c.Id, 0)
		}
		return k.ClassCancelQueue.Remove(ctx, c.Id)
	case types.HideTargetKind_HIDE_TARGET_KIND_TOKEN:
		t, err := k.getToken(ctx, hr.ClassId, hr.TokenId)
		if err != nil {
			return err
		}
		if status == types.ContentStatus_CONTENT_STATUS_HIDDEN && t.Lock == types.TokenLock_TOKEN_LOCK_LISTED {
			if err := k.removeListing(ctx, t.ClassId, t.Id, "hidden"); err != nil {
				return err
			}
			t.Lock = types.TokenLock_TOKEN_LOCK_NONE
		}
		t.Status = status
		return k.saveToken(ctx, t)
	}
	return errorsmod.Wrap(types.ErrHideNotFound, "unknown target kind")
}

// closeHide finalizes a hide record with the given outcome and removes its
// open-record indexes.
func (k Keeper) closeHide(ctx context.Context, hr types.HideRecord, outcome types.HideOutcome) error {
	if err := k.HideByTarget.Remove(ctx, targetKey(hr.ClassId, hr.TokenId)); err != nil {
		return err
	}
	if err := k.HideExpiry.Remove(ctx, collections.Join(hr.AppealDeadline, hr.Id)); err != nil {
		return err
	}
	hr.Outcome = outcome
	if err := k.HideRecords.Set(ctx, hr.Id, hr); err != nil {
		return err
	}
	emit(ctx, types.EventHideResolved, sdk.NewAttribute("hide_id", u64(hr.Id)),
		sdk.NewAttribute("outcome", outcome.String()))
	return nil
}

func (k Keeper) releaseSentinelBond(ctx context.Context, hr types.HideRecord) {
	if hr.Sentinel == "" || k.late.rep == nil || !hr.CommittedAmount.IsPositive() {
		return
	}
	if err := k.late.rep.ReleaseBond(ctx, sentinelRole, hr.Sentinel, hr.CommittedAmount); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Warn("artifact: release sentinel bond failed", "hide_id", hr.Id, "error", err)
	}
}

func (k Keeper) recordSentinelOutcome(ctx context.Context, hr types.HideRecord, upheld bool) {
	if hr.Sentinel == "" || k.late.rep == nil {
		return
	}
	if err := k.late.rep.RecordRoleOutcome(ctx, sentinelRole, hr.Sentinel, reptypes.ActionKindArtifactHide, upheld); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Warn("artifact: record sentinel outcome failed", "hide_id", hr.Id, "error", err)
	}
}

// scrubTarget erases the content of a hidden target from state (§7.11). A
// class scrub also queues every token (and pending mint) of the class.
func (k Keeper) scrubTarget(ctx context.Context, hr types.HideRecord) error {
	switch hr.TargetKind {
	case types.HideTargetKind_HIDE_TARGET_KIND_CLASS:
		c, err := k.getClass(ctx, hr.ClassId)
		if err != nil {
			return nil // class gone; nothing to scrub
		}
		c.Name, c.Description, c.Uri, c.UriHash, c.TokenUriBase = "", "", "", "", ""
		c.MetadataFrozen = true
		c.MediaFlags = types.LabelClass(c)
		if err := k.saveClass(ctx, c); err != nil {
			return err
		}
		if err := k.ClassScrubQueue.Set(ctx, c.Id, 0); err != nil {
			return err
		}
	case types.HideTargetKind_HIDE_TARGET_KIND_TOKEN:
		t, err := k.getToken(ctx, hr.ClassId, hr.TokenId)
		if err != nil {
			return nil
		}
		scrubToken(&t)
		if err := k.saveToken(ctx, t); err != nil {
			return err
		}
	}
	emit(ctx, types.EventMetadataScrubbed, sdk.NewAttribute("hide_id", u64(hr.Id)),
		sdk.NewAttribute("target_kind", hr.TargetKind.String()))
	return nil
}

func scrubToken(t *types.Token) {
	t.Metadata = types.ScrubbedMetadata()
	t.MetadataFrozen = true
	t.MediaFlags = types.LabelToken(t.Metadata)
}

// expireHide handles an unappealed hide whose expiry passed: the metadata is
// scrubbed and the sentinel bond released. Appealed hides are not in the
// expiry index; x/rep owns their deadline (see appeal_target.go).
func (k Keeper) expireHide(ctx context.Context, hr types.HideRecord) error {
	if hr.Outcome != types.HideOutcome_HIDE_OUTCOME_PENDING || hr.Appealed {
		return k.HideExpiry.Remove(ctx, collections.Join(hr.AppealDeadline, hr.Id))
	}
	if err := k.scrubTarget(ctx, hr); err != nil {
		return err
	}
	k.releaseSentinelBond(ctx, hr)
	return k.closeHide(ctx, hr, types.HideOutcome_HIDE_OUTCOME_EXPIRED)
}

// resolveHideOnBurn closes an open token hide when the holder burns the
// token: the sentinel bond is released with no verdict recorded. If the hide
// was under appeal, x/rep still settles its appeal bond when the appeal ends,
// but the closed record makes every artifact callback a no-op (and reports
// no sentinel), so nothing is released or slashed twice.
func (k Keeper) resolveHideOnBurn(ctx context.Context, classID, tokenID uint64) error {
	id, err := k.HideByTarget.Get(ctx, targetKey(classID, tokenID))
	if errors.Is(err, collections.ErrNotFound) {
		return nil
	} else if err != nil {
		return err
	}
	hr, err := k.HideRecords.Get(ctx, id)
	if err != nil {
		return err
	}
	k.releaseSentinelBond(ctx, hr)
	return k.closeHide(ctx, hr, types.HideOutcome_HIDE_OUTCOME_TARGET_BURNED)
}

// openHide validates and opens a hide (MsgHideContent body).
func (k Keeper) openHide(ctx context.Context, msg *types.MsgHideContent) (uint64, error) {
	if _, err := k.decodeAddr("authority", msg.Authority); err != nil {
		return 0, err
	}
	if msg.Reason == commontypes.ModerationReason_MODERATION_REASON_UNSPECIFIED {
		return 0, errorsmod.Wrap(types.ErrInvalidReason, "reason must not be UNSPECIFIED")
	}
	if _, ok := commontypes.ModerationReason_name[int32(msg.Reason)]; !ok {
		return 0, errorsmod.Wrap(types.ErrInvalidReason, "unknown reason")
	}
	if err := types.ValidateText("reason_text", msg.ReasonText, types.HideReasonMaxLength, true); err != nil {
		return 0, err
	}
	p := k.GetParams(ctx)

	// Authority path: an eligible sentinel takes the bonded path; otherwise
	// the Commons Operations authority takes the council path.
	sentinel := false
	if k.late.rep != nil {
		if _, err := k.late.rep.EligibleForRole(ctx, sentinelRole, msg.Authority); err == nil {
			sentinel = true
		}
	}
	if !sentinel && !k.isOpsAuthority(ctx, msg.Authority) {
		return 0, errorsmod.Wrap(types.ErrNotAuthorized, "not an eligible sentinel or council authority")
	}

	// Target must exist and be visible.
	var tokenID uint64
	c, err := k.getClass(ctx, msg.ClassId)
	if err != nil {
		return 0, err
	}
	if c.Status == types.ContentStatus_CONTENT_STATUS_HIDDEN {
		return 0, errorsmod.Wrap(types.ErrContentHidden, "class is already hidden")
	}
	switch msg.TargetKind {
	case types.HideTargetKind_HIDE_TARGET_KIND_CLASS:
	case types.HideTargetKind_HIDE_TARGET_KIND_TOKEN:
		t, err := k.getToken(ctx, msg.ClassId, msg.TokenId)
		if err != nil {
			return 0, err
		}
		if t.Status == types.ContentStatus_CONTENT_STATUS_HIDDEN {
			return 0, types.ErrAlreadyHidden
		}
		tokenID = t.Id
	default:
		return 0, errorsmod.Wrap(types.ErrInvalidMetadata, "target_kind must be CLASS or TOKEN")
	}
	if has, _ := k.HideByTarget.Has(ctx, targetKey(msg.ClassId, tokenID)); has {
		return 0, types.ErrAlreadyHidden
	}

	committed := math.ZeroInt()
	sentinelAddr := ""
	if sentinel {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		if until := k.late.rep.RoleOverturnCooldownUntil(ctx, sentinelRole, msg.Authority); until > sdkCtx.BlockTime().Unix() {
			return 0, errorsmod.Wrapf(types.ErrSentinelCooldown, "until %d", until)
		}
		day := height(ctx) / 14400
		dayKey := collections.Join(msg.Authority, day)
		used, _ := k.SentinelDailyHides.Get(ctx, dayKey)
		if used >= uint64(p.MaxHidesPerSentinelPerDay) {
			return 0, types.ErrSentinelRateLimit
		}
		avail, err := k.late.rep.GetAvailableBond(ctx, sentinelRole, msg.Authority)
		if err != nil {
			return 0, errorsmod.Wrap(types.ErrInsufficientBond, err.Error())
		}
		if avail.LT(p.SentinelHideCommitDream) {
			return 0, types.ErrInsufficientBond
		}
		if p.SentinelHideCommitDream.IsPositive() {
			if err := k.late.rep.ReserveBond(ctx, sentinelRole, msg.Authority, p.SentinelHideCommitDream); err != nil {
				return 0, errorsmod.Wrap(types.ErrInsufficientBond, err.Error())
			}
		}
		if err := k.SentinelDailyHides.Set(ctx, dayKey, used+1); err != nil {
			return 0, err
		}
		committed = p.SentinelHideCommitDream
		sentinelAddr = msg.Authority
		if err := k.late.rep.RecordRoleAction(ctx, sentinelRole, msg.Authority, reptypes.ActionKindArtifactHide); err != nil {
			sdkCtx.Logger().Warn("artifact: record hide action failed", "error", err)
		}
	}

	id, err := k.HideSeq.Next(ctx)
	if err != nil {
		return 0, err
	}
	id++ // hide ids start at 1
	hr := types.HideRecord{
		Id:              id,
		TargetKind:      msg.TargetKind,
		ClassId:         msg.ClassId,
		TokenId:         tokenID,
		Sentinel:        sentinelAddr,
		CommittedAmount: committed,
		Reason:          msg.Reason,
		ReasonText:      msg.ReasonText,
		HiddenAt:        height(ctx),
		AppealDeadline:  height(ctx) + p.HideExpiryBlocks,
		Outcome:         types.HideOutcome_HIDE_OUTCOME_PENDING,
	}
	if err := k.setTargetStatus(ctx, hr, types.ContentStatus_CONTENT_STATUS_HIDDEN); err != nil {
		return 0, err
	}
	if err := k.HideRecords.Set(ctx, id, hr); err != nil {
		return 0, err
	}
	if err := k.HideByTarget.Set(ctx, targetKey(hr.ClassId, tokenID), id); err != nil {
		return 0, err
	}
	if err := k.HidesByTargetAll.Set(ctx, collections.Join3(hr.ClassId, tokenID, id)); err != nil {
		return 0, err
	}
	if err := k.HideExpiry.Set(ctx, collections.Join(hr.AppealDeadline, id)); err != nil {
		return 0, err
	}
	authority := "council"
	if sentinel {
		authority = "sentinel"
	}
	emit(ctx, types.EventHidden, sdk.NewAttribute("hide_id", u64(id)), classAttr(hr.ClassId), tokenAttr(tokenID),
		sdk.NewAttribute("target_kind", hr.TargetKind.String()), sdk.NewAttribute("authority", authority),
		sdk.NewAttribute("reason", hr.Reason.String()))
	return id, nil
}
