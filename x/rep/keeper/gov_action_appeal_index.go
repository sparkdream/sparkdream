package keeper

import (
	"context"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"cosmossdk.io/collections"

	"sparkdream/x/rep/types"
)

// SetGovActionAppeal writes an appeal and keeps its indexes in step: the
// by-target index (one pending appeal per action, member-action dedup), the
// pending-by-deadline queue (EndBlock timeout) and the jury-review lookup
// (verdict dispatch). Every write of a GovActionAppeal goes through here, so
// no path scans the whole appeal history, which is never pruned.
func (k Keeper) SetGovActionAppeal(ctx context.Context, appeal types.GovActionAppeal) error {
	if old, err := k.GovActionAppeal.Get(ctx, appeal.Id); err == nil {
		if err := k.PendingGovActionAppeals.Remove(ctx, collections.Join(old.Deadline, old.Id)); err != nil {
			return err
		}
	}
	if err := k.GovActionAppeal.Set(ctx, appeal.Id, appeal); err != nil {
		return err
	}
	if err := k.GovActionAppealsByTarget.Set(ctx, collections.Join3(int32(appeal.ActionType), appeal.ActionTarget, appeal.Id)); err != nil {
		return err
	}
	if appeal.InitiativeId != 0 {
		if err := k.GovActionAppealByInitiative.Set(ctx, appeal.InitiativeId, appeal.Id); err != nil {
			return err
		}
	}
	if appeal.Status == types.GovAppealStatus_GOV_APPEAL_STATUS_PENDING && appeal.Deadline != 0 {
		return k.PendingGovActionAppeals.Set(ctx, collections.Join(appeal.Deadline, appeal.Id))
	}
	return nil
}

// govActionAppealsFor returns every appeal ever filed against (actionType,
// actionTarget), oldest first.
func (k Keeper) govActionAppealsFor(ctx context.Context, actionType types.GovActionType, actionTarget string) []types.GovActionAppeal {
	var out []types.GovActionAppeal
	rng := collections.NewSuperPrefixedTripleRange[int32, string, uint64](int32(actionType), actionTarget)
	if err := k.GovActionAppealsByTarget.Walk(ctx, rng, func(key collections.Triple[int32, string, uint64]) (bool, error) {
		if a, err := k.GovActionAppeal.Get(ctx, key.K3()); err == nil {
			out = append(out, a)
		}
		return false, nil
	}); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("govActionAppealsFor: GovActionAppealsByTarget.Walk failed", "err", err)
	}
	return out
}

// appealRoleType is the bonded role whose bond an appeal of actionType
// releases or slashes: the target's own choice when it implements
// ModerationAppealRole, the content sentinel otherwise.
func (k Keeper) appealRoleType(actionType types.GovActionType) types.RoleType {
	if r, ok := k.appealTarget(actionType).(types.ModerationAppealRole); ok {
		if rt := r.AppealRoleType(actionType); rt != types.RoleType_ROLE_TYPE_UNSPECIFIED {
			return rt
		}
	}
	return types.RoleType_ROLE_TYPE_CONTENT_SENTINEL
}
