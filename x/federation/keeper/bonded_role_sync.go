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
//   - VerifierUnbondCooldown   → BondedRoleConfig.UnbondCooldown (seconds)
//   - UpheldToResetOverturns   → BondedRoleConfig.UpheldToResetOverturns
//   - VerifierOverturnBaseCooldown → BondedRoleConfig.OverturnBaseCooldown
//
// The last two, plus the OverturnCooldownEscalates flag set below, are the
// verdict-streak policy x/rep applies in RecordRoleOutcome now that the
// verifier's accountability record lives on the shared RoleActivity. They
// stay federation params — federation owns the role's policy — but rep is
// what enforces them, so they have to be written through like the rest.
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
		UnbondCooldown:    int64(p.VerifierUnbondCooldown.Seconds()),

		UpheldToResetOverturns: uint64(p.UpheldToResetOverturns),
		OverturnBaseCooldown:   int64(p.VerifierOverturnBaseCooldown.Seconds()),
		// The verifier's overturn cooldown DOUBLES per consecutive overturn,
		// unlike the moderation roles' flat lockout. An overturned hide is a
		// contested judgment call; an overturned verification means the holder
		// attested to a hash that was false. First mistake cheap, pattern
		// expensive — capped at 7 days so it stays a cooldown rather than an
		// unappealable ban.
		OverturnCooldownEscalates: true,
	})
}
