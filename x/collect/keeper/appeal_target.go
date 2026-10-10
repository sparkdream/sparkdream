package keeper

import (
	"context"
	"fmt"
	"strconv"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/collect/types"
	reptypes "sparkdream/x/rep/types"
)

// RepAppealTarget is x/collect's side of x/rep's moderation-appeal machinery
// for GOV_ACTION_TYPE_COLLECT_HIDE (action target = HideRecord id). x/rep
// holds the appellant bond, seats the jury, owns the deadline, releases or
// slashes the sentinel's reserved bond and records the RoleActivity verdict;
// these callbacks only change collect state. Registered from app.go with
// RepKeeper.RegisterModerationAppealTarget.
type RepAppealTarget struct {
	k Keeper
}

// NewRepAppealTarget wraps the collect keeper for x/rep.
func NewRepAppealTarget(k Keeper) RepAppealTarget { return RepAppealTarget{k: k} }

var (
	_ reptypes.ModerationAppealTarget         = RepAppealTarget{}
	_ reptypes.ModerationAppealOutcomeHandler = RepAppealTarget{}
)

// appealedRecord returns the hide record behind an appeal and whether it is
// still awaiting the verdict. A record collect already resolved itself (e.g.
// the collection was deleted) reports false, which turns every callback into
// a no-op and hides the sentinel from x/rep so no bond moves twice.
func (t RepAppealTarget) appealedRecord(ctx context.Context, actionType reptypes.GovActionType, target string) (types.HideRecord, bool, error) {
	if actionType != reptypes.GovActionType_GOV_ACTION_TYPE_COLLECT_HIDE {
		return types.HideRecord{}, false, fmt.Errorf("collect: unexpected appeal type %s", actionType)
	}
	id, err := strconv.ParseUint(target, 10, 64)
	if err != nil {
		return types.HideRecord{}, false, fmt.Errorf("collect: bad appeal target %q", target)
	}
	hr, err := t.k.HideRecord.Get(ctx, id)
	if err != nil {
		return hr, false, nil
	}
	return hr, hr.Appealed && !hr.Resolved, nil
}

// markResolved persists the terminal state of an appealed hide.
func (t RepAppealTarget) markResolved(ctx context.Context, hr types.HideRecord) error {
	hr.Resolved = true
	if err := t.k.HideRecord.Set(ctx, hr.Id, hr); err != nil {
		return err
	}
	return t.k.HideRecordExpiry.Remove(ctx, collections.Join(hr.AppealDeadline, hr.Id))
}

// GetActionSentinel returns the hiding sentinel ("" for council hides and
// records collect already resolved).
func (t RepAppealTarget) GetActionSentinel(ctx context.Context, actionType reptypes.GovActionType, target string) (string, error) {
	hr, open, err := t.appealedRecord(ctx, actionType, target)
	if err != nil || !open {
		return "", err
	}
	return hr.Sentinel, nil
}

// GetActionCommittedAmount returns the DREAM the hide reserved on the
// sentinel bond (zero once collect resolved the record itself).
func (t RepAppealTarget) GetActionCommittedAmount(ctx context.Context, actionType reptypes.GovActionType, target string) (math.Int, error) {
	hr, open, err := t.appealedRecord(ctx, actionType, target)
	if err != nil || !open || hr.CommittedAmount.IsNil() {
		return math.ZeroInt(), err
	}
	return hr.CommittedAmount, nil
}

// OnSentinelActionResolved has no collect-local bookkeeping to do.
func (t RepAppealTarget) OnSentinelActionResolved(context.Context, reptypes.GovActionType, string) error {
	return nil
}

// ReverseSentinelAction (verdict OVERTURNED): restore the content and the
// author bond / per-tag rep penalty snapshotted at hide time.
func (t RepAppealTarget) ReverseSentinelAction(ctx context.Context, actionType reptypes.GovActionType, target string) error {
	hr, open, err := t.appealedRecord(ctx, actionType, target)
	if err != nil || !open {
		return err
	}
	t.k.restoreHiddenTarget(ctx, hr)
	authorBondRestored, repPenaltyRestored := t.k.restoreAuthorPenalties(ctx, hr)
	if err := t.markResolved(ctx, hr); err != nil {
		return err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("hide_appeal_overturned",
		sdk.NewAttribute("hide_record_id", strconv.FormatUint(hr.Id, 10)),
		sdk.NewAttribute("appeal_id", strconv.FormatUint(hr.AppealId, 10)),
		sdk.NewAttribute("target_id", strconv.FormatUint(hr.TargetId, 10)),
		sdk.NewAttribute("target_type", hr.TargetType.String()),
		sdk.NewAttribute("author_bond_restored", strconv.FormatBool(authorBondRestored)),
		sdk.NewAttribute("rep_penalty_restored", strconv.FormatBool(repPenaltyRestored)),
	))
	return nil
}

// OnAppealOutcome finalizes the hide for the other terminal outcomes:
//
//   - UPHELD:  the hide stands and the content is deleted, as for an
//     unappealed hide (x/rep already released the sentinel bond).
//   - TIMEOUT: no verdict; x/rep blamed no one and left the sentinel's
//     reservation alone, so the content is restored (timeout favors the
//     appellant), author penalties are restored and the bond is released.
func (t RepAppealTarget) OnAppealOutcome(ctx context.Context, actionType reptypes.GovActionType, target string, outcome reptypes.GovAppealStatus) error {
	hr, open, err := t.appealedRecord(ctx, actionType, target)
	if err != nil || !open {
		return err
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	switch outcome {
	case reptypes.GovAppealStatus_GOV_APPEAL_STATUS_UPHELD:
		// Persist Resolved BEFORE deleting, so the endorsement slash gate in
		// deleteCollectionFull (hasInflightHideAppeal) does not see this
		// appeal as still in flight and suppress the endorser burn.
		if err := t.markResolved(ctx, hr); err != nil {
			return err
		}
		params, err := t.k.Params.Get(ctx)
		if err != nil {
			return err
		}
		deleted := t.k.deleteHiddenTarget(ctx, sdkCtx, hr, params)
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("hide_appeal_rejected",
			sdk.NewAttribute("hide_record_id", strconv.FormatUint(hr.Id, 10)),
			sdk.NewAttribute("appeal_id", strconv.FormatUint(hr.AppealId, 10)),
			sdk.NewAttribute("target_id", strconv.FormatUint(hr.TargetId, 10)),
			sdk.NewAttribute("target_type", hr.TargetType.String()),
			sdk.NewAttribute("target_deleted", strconv.FormatBool(deleted)),
		))
	case reptypes.GovAppealStatus_GOV_APPEAL_STATUS_TIMEOUT:
		t.k.restoreHiddenTarget(ctx, hr)
		if t.k.repKeeper != nil && hr.Sentinel != "" && hr.CommittedAmount.IsPositive() {
			if err := t.k.repKeeper.ReleaseBond(ctx, reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, hr.Sentinel, hr.CommittedAmount); err != nil {
				sdkCtx.Logger().Warn("collect: release sentinel bond on appeal timeout failed", "hide_record_id", hr.Id, "error", err)
			}
		}
		authorBondRestored, repPenaltyRestored := t.k.restoreAuthorPenalties(ctx, hr)
		if err := t.markResolved(ctx, hr); err != nil {
			return err
		}
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("hide_appeal_timeout",
			sdk.NewAttribute("hide_record_id", strconv.FormatUint(hr.Id, 10)),
			sdk.NewAttribute("appeal_id", strconv.FormatUint(hr.AppealId, 10)),
			sdk.NewAttribute("target_id", strconv.FormatUint(hr.TargetId, 10)),
			sdk.NewAttribute("target_type", hr.TargetType.String()),
			sdk.NewAttribute("author_bond_restored", strconv.FormatBool(authorBondRestored)),
			sdk.NewAttribute("rep_penalty_restored", strconv.FormatBool(repPenaltyRestored)),
		))
	}
	return nil // OVERTURNED is handled by ReverseSentinelAction
}
