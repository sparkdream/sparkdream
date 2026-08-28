package keeper

import (
	"context"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/rep/types"
)

// RoleActivity is the shared accountability record for a bonded role
// (streaks, overturn cooldown, accuracy ring, per-kind action counters).
// Owning modules REPORT here; x/rep applies the consequences — including
// streak demotion, since rep owns the bond being demoted. See
// docs/x-rep-spec.md (RoleActivity).

func roleActivityKey(roleType types.RoleType, addr string) collections.Pair[int32, string] {
	return collections.Join(int32(roleType), addr)
}

// GetRoleActivity returns the record, or an empty (zero-counter) record
// with no error when the holder has not been reported on yet.
func (k Keeper) GetRoleActivity(ctx context.Context, roleType types.RoleType, addr string) (types.RoleActivity, error) {
	if err := validateRoleType(roleType); err != nil {
		return types.RoleActivity{}, err
	}
	ra, err := k.RoleActivities.Get(ctx, roleActivityKey(roleType, addr))
	if err != nil {
		return types.RoleActivity{RoleType: roleType, Address: addr}, nil
	}
	return ra, nil
}

func (k Keeper) getOrInitRoleActivity(ctx context.Context, roleType types.RoleType, addr string) types.RoleActivity {
	ra, err := k.RoleActivities.Get(ctx, roleActivityKey(roleType, addr))
	if err != nil {
		ra = types.RoleActivity{RoleType: roleType, Address: addr}
	}
	if ra.EpochActions == nil {
		ra.EpochActions = map[string]uint64{}
	}
	if ra.TotalActions == nil {
		ra.TotalActions = map[string]uint64{}
	}
	if ra.UpheldActions == nil {
		ra.UpheldActions = map[string]uint64{}
	}
	if ra.OverturnedActions == nil {
		ra.OverturnedActions = map[string]uint64{}
	}
	return ra
}

// RecordRoleAction counts one action of the given kind (epoch + lifetime).
// Called by the owning module at action time.
func (k Keeper) RecordRoleAction(ctx context.Context, roleType types.RoleType, addr, kind string) error {
	if err := validateRoleType(roleType); err != nil {
		return err
	}
	ra := k.getOrInitRoleActivity(ctx, roleType, addr)
	ra.EpochActions[kind]++
	ra.TotalActions[kind]++
	return k.RoleActivities.Set(ctx, roleActivityKey(roleType, addr), ra)
}

// RecordRoleOutcome records a verdict on one of the role holder's actions:
// upheld/overturned per-kind counters, the shared streaks, the accuracy
// ring at the current reward epoch, the overturn cooldown (per the
// CooldownOnOverturn kind policy), and — when the overturn streak crosses
// the threshold — demotion of the role's bond.
func (k Keeper) RecordRoleOutcome(ctx context.Context, roleType types.RoleType, addr, kind string, upheld bool) error {
	if err := validateRoleType(roleType); err != nil {
		return err
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	epoch := k.roleRewardEpoch(ctx, roleType)

	// Per-role streak policy. A missing config yields the zero value, which
	// resolves to the moderation-role defaults below — so a role whose owning
	// module has not written a config through behaves exactly as before.
	cfg, _ := k.GetBondedRoleConfig(ctx, roleType)

	ra := k.getOrInitRoleActivity(ctx, roleType, addr)
	ra.EpochAppealsResolved++
	bumpRoleAccuracyWindow(&ra, epoch, upheld)

	if upheld {
		ra.UpheldActions[kind]++
		ra.ConsecutiveUpheld++
		// How many consecutive upheld verdicts clear an overturn streak.
		// Zero/one is the moderation default: one good call wipes the slate.
		// A role that sets this higher (the federation verifier sets 3) makes
		// the streak sticky, so alternating wrong/right cannot hold a holder
		// permanently one overturn short of demotion.
		needed := cfg.UpheldToResetOverturns
		if needed == 0 {
			needed = 1
		}
		if ra.ConsecutiveUpheld >= needed {
			ra.ConsecutiveOverturns = 0
		}
	} else {
		ra.OverturnedActions[kind]++
		ra.ConsecutiveOverturns++
		ra.ConsecutiveUpheld = 0
		if types.CooldownOnOverturn[kind] {
			ra.OverturnCooldownUntil = sdkCtx.BlockTime().Unix() +
				roleOverturnCooldown(cfg, ra.ConsecutiveOverturns)
		}
	}

	if err := k.RoleActivities.Set(ctx, roleActivityKey(roleType, addr), ra); err != nil {
		return err
	}

	// Streak demotion — internal now that the record lives beside the bond.
	// Best-effort with logged errors: a demotion failure must not roll back
	// the verdict that triggered it.
	if !upheld && ra.ConsecutiveOverturns >= types.DefaultMaxConsecutiveOverturnsBeforeDemotion {
		// Prefer the role's own configured demotion cooldown; the shared
		// constant is the fallback for roles whose module has not written a
		// config through.
		demotionCooldown := cfg.DemotionCooldown
		if demotionCooldown <= 0 {
			demotionCooldown = types.DefaultSentinelDemotionCooldown
		}
		cooldownUntil := sdkCtx.BlockTime().Unix() + demotionCooldown
		if err := k.SetBondStatus(ctx, roleType, addr,
			types.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED, cooldownUntil); err != nil {
			sdkCtx.Logger().Error("failed to demote role after overturn streak",
				"role_type", roleType.String(), "address", addr,
				"consecutive_overturns", ra.ConsecutiveOverturns, "error", err)
		}
	}
	return nil
}

// roleOverturnCooldown returns the lockout in seconds for an overturned
// verdict at the given consecutive-overturn count.
//
// Flat at the configured base by default. Roles that opt into escalation
// double it per consecutive overturn — base * 2^(streak-1) — which is the
// right shape where a single overturn means the holder asserted something
// false rather than made a contested judgment call: the first mistake is
// cheap, a pattern gets expensive fast. Capped at 7 days so escalation
// cannot become a de-facto permanent ban that sidesteps the demotion path,
// and the shift is clamped so the doubling cannot overflow.
func roleOverturnCooldown(cfg types.BondedRoleConfig, consecutiveOverturns uint64) int64 {
	base := cfg.OverturnBaseCooldown
	if base <= 0 {
		base = types.DefaultRoleOverturnCooldown
	}
	if !cfg.OverturnCooldownEscalates || consecutiveOverturns <= 1 {
		return base
	}
	shift := consecutiveOverturns - 1
	if shift > 20 {
		shift = 20
	}
	cooldown := base * (int64(1) << shift)
	if cooldown > types.MaxRoleOverturnCooldown || cooldown < 0 {
		return types.MaxRoleOverturnCooldown
	}
	return cooldown
}

// RoleOverturnCooldownUntil returns the unix timestamp until which the role
// holder is locked out of new moderation actions (0 = none). Consumed by
// every surface's action-time gate.
func (k Keeper) RoleOverturnCooldownUntil(ctx context.Context, roleType types.RoleType, addr string) int64 {
	ra, err := k.RoleActivities.Get(ctx, roleActivityKey(roleType, addr))
	if err != nil {
		return 0
	}
	return ra.OverturnCooldownUntil
}

// RoleEpochActionCount returns the current reward epoch's count for one
// action kind. Owning modules read this for their per-epoch caps — the
// single source of truth; there are no module-local copies.
func (k Keeper) RoleEpochActionCount(ctx context.Context, roleType types.RoleType, addr, kind string) uint64 {
	ra, err := k.RoleActivities.Get(ctx, roleActivityKey(roleType, addr))
	if err != nil {
		return 0
	}
	return ra.EpochActions[kind]
}

// roleRewardEpoch returns the reward-epoch index in the units the given role's
// reward distribution reads.
//
// The accuracy ring is stamped here and read as a window at distribution time,
// so the two must agree on what an epoch is. Each role with its own reward pool
// has its own independently committee-editable cadence, so deriving the stamp
// from any single role's dial silently zeroes another role's pay the moment the
// two are set apart — which is exactly what happened when every role stamped in
// sentinel epochs. Roles without a pool of their own share the sentinel clock.
func (k Keeper) roleRewardEpoch(ctx context.Context, roleType types.RoleType) uint64 {
	switch roleType {
	case types.RoleType_ROLE_TYPE_INITIATIVE_REVIEWER:
		return k.CurrentReviewerRewardEpoch(ctx)
	case types.RoleType_ROLE_TYPE_COLLECT_CURATOR:
		return k.CurrentCuratorRewardEpoch(ctx)
	case types.RoleType_ROLE_TYPE_FEDERATION_VERIFIER:
		return k.CurrentVerifierRewardEpoch(ctx)
	default:
		return k.CurrentSentinelRewardEpoch(ctx)
	}
}

// ResetRoleEpochCounters zeros the per-epoch state (epoch_actions map +
// epoch_appeals_resolved), preserving lifetime counters, streaks, cooldown,
// and the ring. Called at reward-epoch boundaries. Missing record -> no-op.
func (k Keeper) ResetRoleEpochCounters(ctx context.Context, roleType types.RoleType, addr string) error {
	ra, err := k.RoleActivities.Get(ctx, roleActivityKey(roleType, addr))
	if err != nil {
		return nil
	}
	ra.EpochActions = map[string]uint64{}
	ra.EpochAppealsResolved = 0
	return k.RoleActivities.Set(ctx, roleActivityKey(roleType, addr), ra)
}

// GetRoleWindowedAccuracy returns (upheld, overturned) verdict counts
// summed over the last `window` reward epochs ending at currentEpoch
// (inclusive). Slots stamped outside that range — stale entries or epochs
// older than the window — are ignored. Missing record or window 0 -> (0, 0).
func (k Keeper) GetRoleWindowedAccuracy(ctx context.Context, roleType types.RoleType, addr string, currentEpoch, window uint64) (uint64, uint64) {
	if window == 0 {
		return 0, 0
	}
	ra, err := k.RoleActivities.Get(ctx, roleActivityKey(roleType, addr))
	if err != nil {
		return 0, 0
	}
	var lo uint64
	if currentEpoch+1 > window {
		lo = currentEpoch - window + 1
	}
	var up, ov uint64
	for _, b := range ra.AccuracyWindow {
		if b != nil && b.Epoch >= lo && b.Epoch <= currentEpoch {
			up += b.Upheld
			ov += b.Overturned
		}
	}
	return up, ov
}

// bumpRoleAccuracyWindow records one verdict in the rolling accuracy ring
// at the slot for `epoch` (slot index = epoch % ring size). The ring is
// lazily allocated to its fixed size on first write. A slot whose stamp
// does not match `epoch` is stale (left by an earlier epoch that mapped to
// the same index) and is reset before counting — because the ring size is
// >= the read window, any slot being overwritten is necessarily older than
// the window, so the overwrite never drops in-window data.
func bumpRoleAccuracyWindow(ra *types.RoleActivity, epoch uint64, upheld bool) {
	if len(ra.AccuracyWindow) != types.RoleAccuracyRingSize {
		ra.AccuracyWindow = make([]*types.RoleAccuracyBucket, types.RoleAccuracyRingSize)
		for i := range ra.AccuracyWindow {
			ra.AccuracyWindow[i] = &types.RoleAccuracyBucket{}
		}
	}
	slot := ra.AccuracyWindow[epoch%uint64(types.RoleAccuracyRingSize)]
	if slot.Epoch != epoch {
		slot.Epoch, slot.Upheld, slot.Overturned = epoch, 0, 0
	}
	if upheld {
		slot.Upheld++
	} else {
		slot.Overturned++
	}
}

// BumpRoleEpochAppealsResolved increments the epoch appeals-resolved
// counter without recording a verdict. Used by forum's flag dismissal,
// which historically counted as a resolved appeal for the reward score's
// sqrt term but carries no accuracy tick (nobody won or lost).
func (k Keeper) BumpRoleEpochAppealsResolved(ctx context.Context, roleType types.RoleType, addr string) error {
	if err := validateRoleType(roleType); err != nil {
		return err
	}
	ra := k.getOrInitRoleActivity(ctx, roleType, addr)
	ra.EpochAppealsResolved++
	return k.RoleActivities.Set(ctx, roleActivityKey(roleType, addr), ra)
}

// stampRoleSlashEpoch records that the role was slashed in the current
// reward epoch (in that role's own epoch units — see roleRewardEpoch).
// Called from SlashBond for every role type.
//
// Reward distributions read it as a "no pay in an epoch you were slashed
// in" gate. Only the federation verifier gates on it today; sentinel,
// curator and reviewer eligibility is deliberately left as-is rather than
// silently tightened as a side effect of this stamp existing.
func (k Keeper) stampRoleSlashEpoch(ctx context.Context, roleType types.RoleType, addr string) error {
	if err := validateRoleType(roleType); err != nil {
		return err
	}
	ra := k.getOrInitRoleActivity(ctx, roleType, addr)
	ra.LastSlashEpoch = int64(k.roleRewardEpoch(ctx, roleType))
	return k.RoleActivities.Set(ctx, roleActivityKey(roleType, addr), ra)
}
