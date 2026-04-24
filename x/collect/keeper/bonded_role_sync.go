package keeper

import (
	"context"

	"sparkdream/x/collect/types"
	reptypes "sparkdream/x/rep/types"
)

// SyncCuratorBondedRoleConfig pushes collect's curator config fields through
// to x/rep's BondedRoleConfig for ROLE_TYPE_COLLECT_CURATOR. Called from
// MsgUpdateOperationalParams and InitGenesis so rep's enforcement state
// tracks collect's source-of-truth params. No-op when the rep keeper is not
// wired (tests may construct the collect keeper standalone).
func (k Keeper) SyncCuratorBondedRoleConfig(ctx context.Context, p types.Params) error {
	if k.repKeeper == nil {
		return nil
	}
	minBond := "0"
	if !p.MinCuratorBond.IsNil() {
		minBond = p.MinCuratorBond.String()
	}
	demotionThreshold := "0"
	if !p.CuratorDemotionThreshold.IsNil() {
		demotionThreshold = p.CuratorDemotionThreshold.String()
	}
	return k.repKeeper.SetBondedRoleConfig(ctx, reptypes.BondedRoleConfig{
		RoleType:          reptypes.RoleType_ROLE_TYPE_COLLECT_CURATOR,
		MinBond:           minBond,
		MinRepTier:        0, // curator is trust-level-gated, not rep-tier-gated
		MinTrustLevel:     p.MinCuratorTrustLevel,
		MinAgeBlocks:      p.MinCuratorAgeBlocks,
		DemotionCooldown:  p.CuratorDemotionCooldown,
		DemotionThreshold: demotionThreshold,
	})
}
