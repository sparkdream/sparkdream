package keeper

import (
	"context"

	"sparkdream/x/rep/types"
)

// SyncReviewerBondedRoleConfig pushes rep's own reviewer params through to the
// BondedRoleConfig for ROLE_TYPE_INITIATIVE_REVIEWER. Called from InitGenesis,
// MsgUpdateParams and MsgUpdateOperationalParams, mirroring
// SyncSentinelBondedRoleConfig in x/forum and SyncCuratorBondedRoleConfig in
// x/collect.
//
// The reviewer is the one bonded role no other module owns, so before this
// existed its config was whatever DefaultBondedRoleConfigs seeded at genesis
// and nothing short of an upgrade could move it. Routing it through params
// gives it the same council-tunable path as every other role, and keeps the
// stored config from drifting away from the params meant to describe it.
//
// The projection is straight: every field is written exactly as params hold it,
// with no defaulting for zero values. Reading a zero as "unset" and substituting
// a default would reintroduce the drift this exists to close -- a council that
// sets reviewer_unbond_cooldown to 0 would get 0 in params and 14 days in the
// config. ReviewerBondPolicy.Validate is what guarantees the fields are all
// populated, and both callers run it before reaching here.
//
// The one exception is a nil MinBond or DemotionThreshold, which no validated
// params can carry but which would panic in Int.String(). Those fall back to
// the shipped default rather than to zero: a 0 floor would silently make the
// role free to enter.
func (k Keeper) SyncReviewerBondedRoleConfig(ctx context.Context, p types.Params) error {
	policy := p.ReviewerBondPolicy()
	defaults := types.DefaultParams().ReviewerBondPolicy()

	if policy.MinBond.IsNil() {
		policy.MinBond = defaults.MinBond
	}
	if policy.DemotionThreshold.IsNil() {
		policy.DemotionThreshold = defaults.DemotionThreshold
	}

	return k.SetBondedRoleConfig(ctx, types.BondedRoleConfig{
		RoleType:          types.RoleType_ROLE_TYPE_INITIATIVE_REVIEWER,
		MinBond:           policy.MinBond.String(),
		MinRepTier:        policy.MinRepTier,
		MinTrustLevel:     policy.MinTrustLevel,
		MinAgeBlocks:      policy.MinAgeBlocks,
		DemotionCooldown:  policy.DemotionCooldown,
		DemotionThreshold: policy.DemotionThreshold.String(),
		UnbondCooldown:    policy.UnbondCooldown,
	})
}
