package keeper

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Escrow termination on zeroing (REP-E-06).
//
// ZeroMember burns the member's whole balance, locked DREAM included. Every
// record that claims part of that lock has to end with it: left in place, its
// later UnlockDREAM failed against a zero aggregate and wedged whatever was
// waiting on it (a challenged initiative, an invitee slot, a bounty payout).
// Escrows whose loss is a forfeit by design (an open challenge, a pending
// invitation) are resolved through their ordinary forfeit path, which burns
// the lock; the rest are dropped as records and their DREAM goes with the
// final balance burn. Another module's lock records are dropped by that
// module through the MemberZeroingHook (x/reveal, x/season).
//
// After zeroing, LockDREAM refuses the member, so StakedDream stays zero and
// any record this pass missed unlocks against nothing (ErrInsufficientStake)
// instead of against a lock that backs something else.

// zeroedJuryReason is recorded on a jury review closed because its challenger
// was zeroed.
const zeroedJuryReason = "voided: challenger zeroed"

// terminateEscrowsForZeroing forfeits or drops every escrow the member holds.
// Runs before ZeroMember burns the balance, so the forfeits below burn their
// own locks out of a balance that still holds them.
func (k Keeper) terminateEscrowsForZeroing(ctx context.Context, member string) error {
	addrs := k.AllAddressesOf(ctx, member)

	if err := k.forfeitChallengesForZeroing(ctx, addrs); err != nil {
		return err
	}
	if err := k.forfeitContentChallengesForZeroing(ctx, addrs); err != nil {
		return err
	}
	if err := k.forfeitPendingInvitationsForZeroing(ctx, addrs); err != nil {
		return err
	}
	if err := k.dropReviewBountyContributions(ctx, addrs); err != nil {
		return err
	}
	if err := k.dropTagReportBonds(ctx, addrs); err != nil {
		return err
	}
	if err := k.dropMemberReportBonds(ctx, addrs); err != nil {
		return err
	}
	return k.zeroBondedRoles(ctx, member)
}

// forfeitChallengesForZeroing rejects every open initiative challenge the
// member filed. Rejection burns the challenger's stake and restores the
// initiative, exactly as a lost verdict does; a seated jury is closed so the
// EndBlocker never tallies a verdict on a resolved challenge.
func (k Keeper) forfeitChallengesForZeroing(ctx context.Context, addrs []string) error {
	var ids []uint64
	for _, status := range []types.ChallengeStatus{
		types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE,
		types.ChallengeStatus_CHALLENGE_STATUS_IN_JURY_REVIEW,
	} {
		if err := k.IterateChallengesByStatus(ctx, status, func(id uint64, c types.Challenge) bool {
			if AddressesMatch(addrs, c.Challenger) {
				ids = append(ids, id)
			}
			return false
		}); err != nil {
			return err
		}
	}
	for _, id := range ids {
		k.forfeitBestEffort(ctx, "challenge", id, func(ctx context.Context) error {
			if err := k.RejectChallenge(ctx, id); err != nil {
				return err
			}
			return k.voidPendingJuryReviewForChallenge(ctx, id, zeroedJuryReason)
		})
	}
	return nil
}

// forfeitBestEffort runs one forfeit in its own cache context. A failure is
// logged and reported as an event, never returned: the member's own escrow
// must not be able to block its zeroing. A forfeit that fails leaves the
// escrow record behind, which then unlocks against nothing
// (ErrInsufficientStake) because LockDREAM refuses a zeroed member.
func (k Keeper) forfeitBestEffort(ctx context.Context, kind string, id uint64, fn func(context.Context) error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	cacheCtx, write := sdkCtx.CacheContext()
	if err := fn(cacheCtx); err != nil {
		sdkCtx.Logger().Error("zeroing: forfeit failed", "kind", kind, "id", id, "error", err)
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			"zeroing_forfeit_failed",
			sdk.NewAttribute("kind", kind),
			sdk.NewAttribute("id", fmt.Sprintf("%d", id)),
			sdk.NewAttribute("error", err.Error()),
		))
		return
	}
	write()
}

// forfeitContentChallengesForZeroing rejects every open content challenge the
// member filed (stake burned) and closes its jury review.
func (k Keeper) forfeitContentChallengesForZeroing(ctx context.Context, addrs []string) error {
	var open []types.ContentChallenge
	for _, status := range []types.ContentChallengeStatus{
		types.ContentChallengeStatus_CONTENT_CHALLENGE_STATUS_ACTIVE,
		types.ContentChallengeStatus_CONTENT_CHALLENGE_STATUS_IN_JURY_REVIEW,
	} {
		if err := k.IterateContentChallengesByStatus(ctx, status, func(_ uint64, cc types.ContentChallenge) bool {
			if AddressesMatch(addrs, cc.Challenger) {
				open = append(open, cc)
			}
			return false
		}); err != nil {
			return err
		}
	}
	for _, cc := range open {
		k.forfeitBestEffort(ctx, "content_challenge", cc.Id, func(ctx context.Context) error {
			if err := k.RejectContentChallenge(ctx, cc.Id); err != nil {
				return err
			}
			if cc.JuryReviewId == 0 {
				return nil
			}
			review, err := k.JuryReview.Get(ctx, cc.JuryReviewId)
			if err != nil || review.Verdict != types.Verdict_VERDICT_PENDING {
				return nil
			}
			review.Verdict = types.Verdict_VERDICT_INCONCLUSIVE
			review.Reasoning = zeroedJuryReason
			return k.JuryReview.Set(ctx, review.Id, review)
		})
	}
	return nil
}

// forfeitPendingInvitationsForZeroing ends every PENDING invitation the
// member sent. The stake is forfeited (burned), the invitee slot is freed and
// no credit comes back: a zeroed member does not vouch for anyone.
func (k Keeper) forfeitPendingInvitationsForZeroing(ctx context.Context, addrs []string) error {
	var pending []types.Invitation
	if err := k.PendingInvitations.Walk(ctx, nil, func(key collections.Pair[int64, uint64]) (bool, error) {
		inv, err := k.Invitation.Get(ctx, key.K2())
		if err != nil {
			return false, nil
		}
		if inv.Status == types.InvitationStatus_INVITATION_STATUS_PENDING && AddressesMatch(addrs, inv.Inviter) {
			pending = append(pending, inv)
		}
		return false, nil
	}); err != nil {
		return err
	}
	for _, inv := range pending {
		k.forfeitBestEffort(ctx, "invitation", inv.Id, func(ctx context.Context) error {
			burned := math.ZeroInt()
			if stake := DerefInt(inv.StakedDream); stake.IsPositive() {
				inviterAddr, err := sdk.AccAddressFromBech32(inv.Inviter)
				if err != nil {
					return err
				}
				if err := k.BurnLockedDREAM(ctx, inviterAddr, stake); err != nil {
					if !errors.Is(err, types.ErrInsufficientStake) {
						return err
					}
				} else {
					burned = stake
				}
			}
			if err := k.endPendingInvitation(ctx, inv, types.InvitationStatus_INVITATION_STATUS_REVOKED); err != nil {
				return err
			}
			sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
				"invitation_forfeited",
				sdk.NewAttribute("invitation_id", fmt.Sprintf("%d", inv.Id)),
				sdk.NewAttribute("inviter", inv.Inviter),
				sdk.NewAttribute("invitee", inv.InviteeAddress),
				sdk.NewAttribute("stake_burned", burned.String()),
				sdk.NewAttribute("reason", "inviter zeroed"),
			))
			return nil
		})
	}
	return nil
}

// dropReviewBountyContributions removes the member's review bounty
// contributions without unlocking them; the DREAM is burned with the balance.
func (k Keeper) dropReviewBountyContributions(ctx context.Context, addrs []string) error {
	var changed []types.ReviewBounty
	if err := k.ReviewBounty.Walk(ctx, nil, func(_ uint64, b types.ReviewBounty) (bool, error) {
		kept := make([]types.ReviewBountyContribution, 0, len(b.Contributions))
		dropped := math.ZeroInt()
		for _, c := range b.Contributions {
			if AddressesMatch(addrs, c.Funder) {
				dropped = dropped.Add(c.Amount)
				continue
			}
			kept = append(kept, c)
		}
		if len(kept) != len(b.Contributions) {
			b.Contributions = kept
			if b.Amount.IsNil() {
				b.Amount = math.ZeroInt()
			}
			b.Amount = math.MaxInt(b.Amount.Sub(dropped), math.ZeroInt())
			changed = append(changed, b)
		}
		return false, nil
	}); err != nil {
		return err
	}
	for _, b := range changed {
		if err := k.persistOrClearBounty(ctx, b.InitiativeId, b); err != nil {
			return err
		}
	}
	return nil
}

// dropTagReportBonds removes the member from every open tag report; its bond
// leaves the report's total and is burned with the balance.
func (k Keeper) dropTagReportBonds(ctx context.Context, addrs []string) error {
	var changed []types.TagReport
	if err := k.TagReport.Walk(ctx, nil, func(_ string, r types.TagReport) (bool, error) {
		kept := make([]string, 0, len(r.Reporters))
		for _, rep := range r.Reporters {
			if !AddressesMatch(addrs, rep) {
				kept = append(kept, rep)
			}
		}
		if len(kept) != len(r.Reporters) {
			total, ok := math.NewIntFromString(r.TotalBond)
			if !ok {
				total = math.ZeroInt()
			}
			dropped := types.DefaultTagReportBond.MulRaw(int64(len(r.Reporters) - len(kept)))
			r.TotalBond = math.MaxInt(total.Sub(dropped), math.ZeroInt()).String()
			r.Reporters = kept
			changed = append(changed, r)
		}
		return false, nil
	}); err != nil {
		return err
	}
	for _, r := range changed {
		if len(r.Reporters) == 0 {
			if err := k.TagReport.Remove(ctx, r.TagName); err != nil {
				return err
			}
			continue
		}
		if err := k.TagReport.Set(ctx, r.TagName, r); err != nil {
			return err
		}
	}
	return nil
}

// dropMemberReportBonds zeroes the member's bond entry on every open member
// report it signed; the bond is burned with the balance.
func (k Keeper) dropMemberReportBonds(ctx context.Context, addrs []string) error {
	var changed []types.MemberReport
	if err := k.MemberReport.Walk(ctx, nil, func(_ string, r types.MemberReport) (bool, error) {
		if r.Status != types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING &&
			r.Status != types.MemberReportStatus_MEMBER_REPORT_STATUS_ESCALATED {
			return false, nil
		}
		dirty := false
		for _, e := range r.ReporterBonds {
			if e != nil && AddressesMatch(addrs, e.Address) && e.Amount != "0" {
				e.Amount = "0"
				dirty = true
			}
		}
		if dirty {
			changed = append(changed, r)
		}
		return false, nil
	}); err != nil {
		return err
	}
	for _, r := range changed {
		if err := k.MemberReport.Set(ctx, r.Member, r); err != nil {
			return err
		}
	}
	return nil
}

// zeroBondedRoles empties every bonded role the member holds: the bond is
// locked DREAM, burned with the balance. Left at face value, a later slash or
// unbond of the role would unlock DREAM the member no longer has.
func (k Keeper) zeroBondedRoles(ctx context.Context, member string) error {
	roles := make([]int32, 0, len(types.RoleType_name))
	for v := range types.RoleType_name {
		roles = append(roles, v)
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i] < roles[j] })
	for _, v := range roles {
		rt := types.RoleType(v)
		if rt == types.RoleType_ROLE_TYPE_UNSPECIFIED {
			continue
		}
		br, err := k.BondedRoles.Get(ctx, bondedRoleKey(rt, member))
		if err != nil {
			continue
		}
		br.CurrentBond = "0"
		br.TotalCommittedBond = "0"
		br.PendingUnbondAmount = "0"
		br.UnbondCompletionTime = 0
		br.BondStatus = types.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED
		if err := k.SetBondedRole(ctx, br); err != nil {
			return err
		}
	}
	return nil
}

// notifyZeroingHooks lets other modules drop the lock records they hold for
// the zeroed member. Each hook runs in its own cache context and a failure is
// logged, never returned: a buggy module must not block a zeroing.
func (k Keeper) notifyZeroingHooks(ctx context.Context, member string) {
	if len(k.late.zeroingHooks) == 0 {
		return
	}
	addrs := k.AllAddressesOf(ctx, member)
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	for _, h := range k.late.zeroingHooks {
		cacheCtx, write := sdkCtx.CacheContext()
		if err := h.AfterMemberZeroed(cacheCtx, member, addrs); err != nil {
			sdkCtx.Logger().Error("member zeroing hook failed", "member", member, "error", err)
			continue
		}
		write()
	}
}

// AddMemberZeroingHook registers a module that holds DREAM lock records and
// must drop a zeroed member's. Wired in app.go after depinject.
func (k Keeper) AddMemberZeroingHook(h types.MemberZeroingHook) {
	if h == nil {
		return
	}
	k.late.zeroingHooks = append(k.late.zeroingHooks, h)
}
