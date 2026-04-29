package keeper

import (
	"context"

	"sparkdream/x/federation/types"
	reptypes "sparkdream/x/rep/types"
)

// SyncVerifierBondedRoleConfig pushes federation's verifier config fields
// through to x/rep's BondedRoleConfig for ROLE_TYPE_FEDERATION_VERIFIER.
// Called from MsgUpdateOperationalParams and InitGenesis so rep's enforcement
// state tracks federation's source-of-truth params. No-op when the rep keeper
// is not wired (tests may construct the federation keeper standalone).
//
// Mapping:
//   - MinVerifierBond          → BondedRoleConfig.MinBond
//   - MinVerifierTrustLevel    → BondedRoleConfig.MinTrustLevel (trust-level name)
//   - VerifierRecoveryThreshold→ BondedRoleConfig.DemotionThreshold
//   - VerifierDemotionCooldown → BondedRoleConfig.DemotionCooldown (seconds)
//
// Federation's verifier is trust-level-gated, not rep-tier-gated, so
// MinRepTier is left at zero. Age-of-bond is not enforced, so MinAgeBlocks
// stays at zero.
func (k Keeper) SyncVerifierBondedRoleConfig(ctx context.Context, p types.Params) error {
	if k.late.repKeeper == nil {
		return nil
	}
	if p.MinVerifierBond.IsNil() {
		panic("MinVerifierBond is nil; must be validated upstream in Params.Validate")
	}
	minBond := p.MinVerifierBond.String()
	demotionThreshold := "0"
	if !p.VerifierRecoveryThreshold.IsNil() {
		demotionThreshold = p.VerifierRecoveryThreshold.String()
	}

	// Translate the uint32 TrustLevel id back to an enum name (e.g.
	// "TRUST_LEVEL_ESTABLISHED"). Empty when the param is unset.
	trustLevel := ""
	if p.MinVerifierTrustLevel > 0 {
		if name, ok := reptypes.TrustLevel_name[int32(p.MinVerifierTrustLevel)]; ok {
			trustLevel = name
		}
	}

	return k.late.repKeeper.SetBondedRoleConfig(ctx, reptypes.BondedRoleConfig{
		RoleType:          reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER,
		MinBond:           minBond,
		MinRepTier:        0,
		MinTrustLevel:     trustLevel,
		MinAgeBlocks:      0,
		DemotionCooldown:  int64(p.VerifierDemotionCooldown.Seconds()),
		DemotionThreshold: demotionThreshold,
	})
}
