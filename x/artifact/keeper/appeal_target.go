package keeper

import (
	"context"
	"fmt"
	"strconv"

	"cosmossdk.io/math"

	reptypes "sparkdream/x/rep/types"

	"sparkdream/x/artifact/types"
)

// RepAppealTarget is x/artifact's side of x/rep's moderation-appeal
// machinery for GOV_ACTION_TYPE_ARTIFACT_HIDE (action target = hide record
// id). x/rep holds the appellant bond, seats the jury, releases or slashes
// the sentinel's reserved bond and records the RoleActivity verdict; these
// callbacks only change artifact state. Registered from app.go with
// RepKeeper.RegisterModerationAppealTarget.
type RepAppealTarget struct {
	k Keeper
}

// NewRepAppealTarget wraps the artifact keeper for x/rep.
func NewRepAppealTarget(k Keeper) RepAppealTarget { return RepAppealTarget{k: k} }

var (
	_ reptypes.ModerationAppealTarget         = RepAppealTarget{}
	_ reptypes.ModerationAppealOutcomeHandler = RepAppealTarget{}
)

// appealedRecord returns the hide record behind an appeal, and whether it is
// still awaiting the verdict. A record closed by artifact itself (e.g. the
// token was burned) reports false, which turns every callback into a no-op.
func (t RepAppealTarget) appealedRecord(ctx context.Context, actionType reptypes.GovActionType, target string) (types.HideRecord, bool, error) {
	if actionType != reptypes.GovActionType_GOV_ACTION_TYPE_ARTIFACT_HIDE {
		return types.HideRecord{}, false, fmt.Errorf("artifact: unexpected appeal type %s", actionType)
	}
	id, err := strconv.ParseUint(target, 10, 64)
	if err != nil {
		return types.HideRecord{}, false, fmt.Errorf("artifact: bad appeal target %q", target)
	}
	hr, err := t.k.HideRecords.Get(ctx, id)
	if err != nil {
		return hr, false, nil
	}
	return hr, hr.Outcome == types.HideOutcome_HIDE_OUTCOME_PENDING && hr.Appealed, nil
}

// GetActionSentinel returns the hiding sentinel ("" for council hides and
// for records artifact already closed).
func (t RepAppealTarget) GetActionSentinel(ctx context.Context, actionType reptypes.GovActionType, target string) (string, error) {
	hr, open, err := t.appealedRecord(ctx, actionType, target)
	if err != nil || !open {
		return "", err
	}
	return hr.Sentinel, nil
}

// GetActionCommittedAmount returns the DREAM the hide reserved on the
// sentinel bond (zero once artifact closed the record itself).
func (t RepAppealTarget) GetActionCommittedAmount(ctx context.Context, actionType reptypes.GovActionType, target string) (math.Int, error) {
	hr, open, err := t.appealedRecord(ctx, actionType, target)
	if err != nil || !open || hr.CommittedAmount.IsNil() {
		return math.ZeroInt(), err
	}
	return hr.CommittedAmount, nil
}

// OnSentinelActionResolved has no artifact-local bookkeeping to do.
func (t RepAppealTarget) OnSentinelActionResolved(context.Context, reptypes.GovActionType, string) error {
	return nil
}

// ReverseSentinelAction (verdict OVERTURNED): restore the content.
func (t RepAppealTarget) ReverseSentinelAction(ctx context.Context, actionType reptypes.GovActionType, target string) error {
	hr, open, err := t.appealedRecord(ctx, actionType, target)
	if err != nil || !open {
		return err
	}
	if err := t.k.setTargetStatus(ctx, hr, types.ContentStatus_CONTENT_STATUS_ACTIVE); err != nil {
		return err
	}
	return t.k.closeHide(ctx, hr, types.HideOutcome_HIDE_OUTCOME_OVERTURNED)
}

// OnAppealOutcome finalizes the hide for the other terminal outcomes:
// UPHELD scrubs the metadata (x/rep already released the sentinel bond);
// TIMEOUT restores the content and releases the bond, since x/rep blamed no
// one and left the reservation alone.
func (t RepAppealTarget) OnAppealOutcome(ctx context.Context, actionType reptypes.GovActionType, target string, outcome reptypes.GovAppealStatus) error {
	hr, open, err := t.appealedRecord(ctx, actionType, target)
	if err != nil || !open {
		return err
	}
	switch outcome {
	case reptypes.GovAppealStatus_GOV_APPEAL_STATUS_UPHELD:
		if err := t.k.scrubTarget(ctx, hr); err != nil {
			return err
		}
		return t.k.closeHide(ctx, hr, types.HideOutcome_HIDE_OUTCOME_UPHELD)
	case reptypes.GovAppealStatus_GOV_APPEAL_STATUS_TIMEOUT:
		if err := t.k.setTargetStatus(ctx, hr, types.ContentStatus_CONTENT_STATUS_ACTIVE); err != nil {
			return err
		}
		t.k.releaseSentinelBond(ctx, hr)
		return t.k.closeHide(ctx, hr, types.HideOutcome_HIDE_OUTCOME_APPEAL_TIMEOUT)
	}
	return nil // OVERTURNED is handled by ReverseSentinelAction
}
