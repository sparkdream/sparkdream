package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Member-action appeals (REP-R-14, FRM-02).
//
// MsgResolveMemberReport writes a MemberActionRestoration for every WARNING,
// DEMOTION or ZEROING it applies: what an OVERTURNED MsgAppealGovAction needs
// to undo the action. The record lives while the action is appealable: until
// the filing window closes with no appeal (pruned through
// MemberActionRestorationExpiry), or until the filed appeal reaches a terminal
// outcome (UPHELD, OVERTURNED or TIMEOUT). One record per member: a new report
// is refused while one exists.
//
// Reversal on OVERTURNED:
//   - WARNING: the warning the resolution created is deleted.
//   - DEMOTION: the reputation the slash took is added back per tag (to the
//     lifetime archive if a season reset came in between).
//   - ZEROING: prior status, trust level, invitation credits and per-tag
//     reputation are restored and the liquid DREAM the zeroing burned is
//     re-minted. Escrows the zeroing forfeited (challenge and invitation
//     stakes, role, report and bounty bonds, the inviter's accountability
//     slash) went to counterparties or were burned and are not restored, nor
//     is any other module's zeroing-hook cleanup.
//
// A member who is no longer in the state the action left them in is not
// touched: a DEMOTION is not reversed onto a member zeroed meanwhile, and a
// ZEROING is reversed only while the member is still zeroed by that same
// zeroing (zeroed_count unchanged).

const maxMemberActionRestorationPrunesPerBlock = 50

// memberActionRestorationExpiry is when the filing window of rest closes.
func memberActionRestorationExpiry(rest types.MemberActionRestoration) int64 {
	return rest.ResolvedAt + types.DefaultAppealDeadline
}

// setMemberActionRestoration writes rest and, while no appeal is filed, its
// filing-window expiry entry.
func (k Keeper) setMemberActionRestoration(ctx context.Context, rest types.MemberActionRestoration) error {
	if old, err := k.MemberActionRestorations.Get(ctx, rest.Member); err == nil {
		if err := k.MemberActionRestorationExpiry.Remove(ctx, collections.Join(memberActionRestorationExpiry(old), old.Member)); err != nil {
			return err
		}
	}
	if err := k.MemberActionRestorations.Set(ctx, rest.Member, rest); err != nil {
		return err
	}
	if !rest.Appealed {
		return k.MemberActionRestorationExpiry.Set(ctx, collections.Join(memberActionRestorationExpiry(rest), rest.Member))
	}
	return nil
}

// removeMemberActionRestoration drops the record of member and its expiry entry.
func (k Keeper) removeMemberActionRestoration(ctx context.Context, rest types.MemberActionRestoration) error {
	if err := k.MemberActionRestorationExpiry.Remove(ctx, collections.Join(memberActionRestorationExpiry(rest), rest.Member)); err != nil {
		return err
	}
	return k.MemberActionRestorations.Remove(ctx, rest.Member)
}

// HasMemberActionRestoration reports whether the last member action applied to
// member is still appealable (filing window open or appeal pending).
func (k Keeper) HasMemberActionRestoration(ctx context.Context, member string) bool {
	has, getErr := k.MemberActionRestorations.Has(ctx, k.Canonical(ctx, member))
	if getErr != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("Has failed", "err", getErr)
	}
	return has
}

// newMemberActionRestoration starts the record for an action on member.
func (k Keeper) newMemberActionRestoration(ctx context.Context, member string, action types.GovActionType) (types.MemberActionRestoration, error) {
	season, err := k.GetCurrentSeason(ctx)
	if err != nil {
		return types.MemberActionRestoration{}, err
	}
	return types.MemberActionRestoration{
		Member:     member,
		Action:     action,
		ResolvedAt: sdk.UnwrapSDKContext(ctx).BlockTime().Unix(),
		Season:     season,
	}, nil
}

// reputationSlashed returns, per tag, how much reputation went from before to
// after (only tags that went down).
func reputationSlashed(before, after map[string]string) map[string]string {
	out := map[string]string{}
	for tag, b := range before {
		bv, err := math.LegacyNewDecFromStr(b)
		if err != nil {
			continue
		}
		av := math.LegacyZeroDec()
		if a, ok := after[tag]; ok {
			if parsed, err := math.LegacyNewDecFromStr(a); err == nil {
				av = parsed
			}
		}
		if d := bv.Sub(av); d.IsPositive() {
			out[tag] = d.String()
		}
	}
	return out
}

func copyStringMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// addDec adds delta to m[tag] (missing or malformed reads as zero), flooring
// the result at zero.
func addDec(m map[string]string, tag string, delta math.LegacyDec) {
	cur := math.LegacyZeroDec()
	if s, ok := m[tag]; ok {
		if parsed, err := math.LegacyNewDecFromStr(s); err == nil {
			cur = parsed
		}
	}
	next := cur.Add(delta)
	if next.IsNegative() {
		next = math.LegacyZeroDec()
	}
	m[tag] = next.String()
}

// markMemberActionAppealed links the filed appeal to the member's record and
// stops the filing-window prune: the record now lives until the appeal ends.
func (k Keeper) markMemberActionAppealed(ctx context.Context, member string, appealID uint64) error {
	rest, err := k.MemberActionRestorations.Get(ctx, member)
	if err != nil {
		return err
	}
	if err := k.MemberActionRestorationExpiry.Remove(ctx, collections.Join(memberActionRestorationExpiry(rest), rest.Member)); err != nil {
		return err
	}
	rest.Appealed = true
	rest.AppealId = appealID
	return k.MemberActionRestorations.Set(ctx, rest.Member, rest)
}

// settleMemberActionAppeal applies a terminal appeal outcome to the member's
// restoration record: OVERTURNED reverses the action, every outcome drops
// the record. A reversal failure is logged and evented and never undoes the
// verdict (the appeal and its bond refund stand); its writes are discarded.
func (k Keeper) settleMemberActionAppeal(ctx context.Context, appeal types.GovActionAppeal, outcome types.GovAppealStatus) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	member := k.Canonical(ctx, appeal.ActionTarget)
	rest, err := k.MemberActionRestorations.Get(ctx, member)
	if err != nil || !rest.Appealed || rest.AppealId != appeal.Id || rest.Action != appeal.ActionType {
		sdkCtx.Logger().Error("member-action appeal has no restoration record",
			"appeal_id", appeal.Id, "member", member, "outcome", outcome.String())
		return
	}
	if outcome == types.GovAppealStatus_GOV_APPEAL_STATUS_OVERTURNED {
		cacheCtx, write := sdkCtx.CacheContext()
		result, rErr := k.reverseMemberAction(cacheCtx, rest)
		if rErr != nil {
			sdkCtx.Logger().Error("failed to reverse overturned member action",
				"appeal_id", appeal.Id, "member", member, "action", rest.Action.String(), "error", rErr)
			result = "failed: " + rErr.Error()
		} else {
			write()
		}
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("member_action_reversed",
			sdk.NewAttribute("appeal_id", fmt.Sprintf("%d", appeal.Id)),
			sdk.NewAttribute("member", member),
			sdk.NewAttribute("action", rest.Action.String()),
			sdk.NewAttribute("result", result),
		))
	}
	if err := k.removeMemberActionRestoration(ctx, rest); err != nil {
		sdkCtx.Logger().Error("failed to drop member-action restoration",
			"appeal_id", appeal.Id, "member", member, "error", err)
	}
}

// reverseMemberAction undoes rest's action. It returns a short result for the
// event: "reversed", or "skipped: <why>" when the member's state no longer
// matches what the action left (nothing is written then).
func (k Keeper) reverseMemberAction(ctx context.Context, rest types.MemberActionRestoration) (string, error) {
	// Records are keyed and re-keyed by canonical address; resolving again
	// costs one read and keeps the lookup alias-safe.
	addr := k.Canonical(ctx, rest.Member)
	switch rest.Action {
	case types.GovActionType_GOV_ACTION_TYPE_WARNING:
		w, err := k.MemberWarning.Get(ctx, rest.WarningId)
		if err != nil {
			return "skipped: warning not found", nil
		}
		if !k.SameMember(ctx, w.Member, rest.Member) {
			return "skipped: warning belongs to another member", nil
		}
		if err := k.MemberWarning.Remove(ctx, rest.WarningId); err != nil {
			return "", err
		}
		return "reversed", nil

	case types.GovActionType_GOV_ACTION_TYPE_DEMOTION:
		member, err := k.Member.Get(ctx, addr)
		if err != nil {
			return "skipped: member not found", nil
		}
		if member.Status != types.MemberStatus_MEMBER_STATUS_ACTIVE {
			return "skipped: member is not active", nil
		}
		season, err := k.GetCurrentSeason(ctx)
		if err != nil {
			return "", err
		}
		if member.ReputationScores == nil {
			member.ReputationScores = map[string]string{}
		}
		if member.LifetimeReputation == nil {
			member.LifetimeReputation = map[string]string{}
		}
		for tag, amt := range rest.Reputation {
			d, err := math.LegacyNewDecFromStr(amt)
			if err != nil || !d.IsPositive() {
				continue
			}
			if season == rest.Season {
				addDec(member.ReputationScores, tag, d)
			} else {
				// The season reset archived the slashed score; the slashed
				// part belongs in the archive too.
				addDec(member.LifetimeReputation, tag, d)
			}
		}
		if err := k.Member.Set(ctx, addr, member); err != nil {
			return "", err
		}
		return "reversed", nil

	case types.GovActionType_GOV_ACTION_TYPE_ZEROING:
		member, err := k.Member.Get(ctx, addr)
		if err != nil {
			return "skipped: member not found", nil
		}
		if member.Status != types.MemberStatus_MEMBER_STATUS_ZEROED || member.ZeroedCount != rest.ZeroedCount {
			return "skipped: member is not in the state this zeroing left", nil
		}
		season, err := k.GetCurrentSeason(ctx)
		if err != nil {
			return "", err
		}
		member.Status = rest.PriorStatus
		member.TrustLevel = rest.PriorTrustLevel
		member.TrustLevelUpdatedAt = sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
		member.InvitationCredits = rest.PriorInvitationCredits
		if member.ZeroedCount > 0 {
			member.ZeroedCount--
		}
		if season == rest.Season {
			// Zeroing archived the seasonal scores into the lifetime archive
			// and zeroed them: take them back out and restore them.
			if member.LifetimeReputation == nil {
				member.LifetimeReputation = map[string]string{}
			}
			for tag, score := range rest.Reputation {
				if d, err := math.LegacyNewDecFromStr(score); err == nil && d.IsPositive() {
					addDec(member.LifetimeReputation, tag, d.Neg())
				}
			}
			member.ReputationScores = copyStringMap(rest.Reputation)
		}
		// In a later season the scores stay archived, where the season reset
		// would have put them.

		burned, ok := math.NewIntFromString(rest.BurnedDream)
		if ok && burned.IsPositive() {
			// A restoration, not new issuance: it bypasses the per-epoch mint
			// ceiling but is counted in the season's minted total, as the
			// zeroing's burn was counted in the burned total.
			if err := k.TrackMint(ctx, burned); err != nil {
				return "", err
			}
			member.DreamBalance = PtrInt(DerefInt(member.DreamBalance).Add(burned))
			lb := DerefInt(member.LifetimeBurned).Sub(burned)
			if lb.IsNegative() {
				lb = math.ZeroInt()
			}
			member.LifetimeBurned = PtrInt(lb)
		}
		if err := k.Member.Set(ctx, addr, member); err != nil {
			return "", err
		}
		// The trust tree dropped the zeroed member; put it back.
		k.MarkMemberDirty(ctx, addr)
		return "reversed", nil
	}
	return "skipped: not a member action", nil
}

// PruneMemberActionRestorations drops the records of member actions whose
// filing window closed with no appeal. Due entries only, capped per block.
func (k Keeper) PruneMemberActionRestorations(ctx context.Context) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()
	// The window is open while now <= expiry, so prune strictly below now.
	rng := new(collections.Range[collections.Pair[int64, string]]).
		EndExclusive(collections.Join(now, ""))
	var due []collections.Pair[int64, string]
	if err := k.MemberActionRestorationExpiry.Walk(ctx, rng, func(key collections.Pair[int64, string]) (bool, error) {
		due = append(due, key)
		return len(due) >= maxMemberActionRestorationPrunesPerBlock, nil
	}); err != nil {
		sdkCtx.Logger().Error("failed to walk member-action restoration expiry", "error", err)
		return
	}
	for _, key := range due {
		if err := k.MemberActionRestorationExpiry.Remove(ctx, key); err != nil {
			sdkCtx.Logger().Error("failed to drop restoration expiry", "member", key.K2(), "error", err)
			continue
		}
		rest, err := k.MemberActionRestorations.Get(ctx, key.K2())
		if err != nil || rest.Appealed || memberActionRestorationExpiry(rest) != key.K1() {
			continue // stale entry: appealed, replaced or gone
		}
		if err := k.MemberActionRestorations.Remove(ctx, key.K2()); err != nil {
			sdkCtx.Logger().Error("failed to drop member-action restoration", "member", key.K2(), "error", err)
		}
	}
}
