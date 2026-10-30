package keeper

import (
	"context"
	"errors"
	"fmt"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// IsEpochEnd checks if the current block is the end of an epoch
func (k Keeper) IsEpochEnd(ctx context.Context) (bool, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return false, err
	}
	if params.EpochBlocks == 0 {
		return false, nil
	}
	return sdk.UnwrapSDKContext(ctx).BlockHeight()%params.EpochBlocks == 0, nil
}

// UpdateInitiativeConviction updates the conviction for a specific initiative
func (k Keeper) UpdateInitiativeConviction(ctx context.Context, initiativeID uint64) error {
	return k.UpdateInitiativeConvictionLazy(ctx, initiativeID)
}

// TransitionToChallengePeriod moves an initiative from SUBMITTED to IN_REVIEW
func (k Keeper) TransitionToChallengePeriod(ctx context.Context, initiativeID uint64) error {
	initiative, err := k.GetInitiative(ctx, initiativeID)
	if err != nil {
		return err
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW

	// Set review period end (current time + default review period)
	currentHeight := sdk.UnwrapSDKContext(ctx).BlockHeight()
	reviewDuration := params.DefaultReviewPeriodEpochs * params.EpochBlocks
	initiative.ReviewPeriodEnd = currentHeight + reviewDuration

	// Also set challenge period end which follows review. Self-assigned
	// initiatives get an extended window: fewer independent parties were
	// involved before submission, so the community gets extra time to contest
	// the work. See IsSelfAssigned — authoring the initiative counts, not only
	// owning the project it sits under.
	challengeDuration := params.DefaultChallengePeriodEpochs * params.EpochBlocks
	project, err := k.GetProject(ctx, initiative.ProjectId)
	if err != nil {
		return err
	}
	if k.isSelfAssigned(ctx, initiative, project, initiative.Assignee) {
		challengeDuration *= params.SelfAssignedChallengeMultiplier
	}
	initiative.ChallengePeriodEnd = initiative.ReviewPeriodEnd + challengeDuration

	return k.UpdateInitiative(ctx, initiative)
}

// ApplyDecay applies decay to unstaked DREAM balances of all members
func (k Keeper) ApplyDecay(ctx context.Context) error {
	// Iterate all members and apply pending decay
	// This ensures everyone is up to date at epoch end
	return k.Member.Walk(ctx, nil, func(key string, member types.Member) (stop bool, err error) {
		// Apply pending decay
		if err := k.ApplyPendingDecay(ctx, &member); err != nil {
			return true, err
		}
		// Save updated member back to store
		if err := k.Member.Set(ctx, key, member); err != nil {
			return true, err
		}
		return false, nil
	})
}

// maxDecayRecordsPerBlock caps the member and stake records one block's
// share of the per-epoch decay pass reads.
const maxDecayRecordsPerBlock = 500

// MaybeApplyBulkDecay runs the per-epoch decay pass: the unstaked decay of
// every member, then the staked decay of every reward-bearing stake record
// (decayStakesBatch). REP-E-10: the pass is split across blocks, at most
// maxDecayRecordsPerBlock records per block, resuming at a persisted cursor
// (DecayPassEpoch / DecayPassMemberCursor / DecayPassStakeCursor), so no
// block's cost scales with the member or stake count.
//
// Members not yet visited are still exact: every member read path applies
// pending decay lazily (GetMember / ApplyPendingDecay, keyed on
// LastDecayEpoch), so the member phase only persists what the lazy path
// would compute. Stake decay has no lazy path; it is applied once per pass,
// by the number of epochs since the previous pass (lazy catch-up, capped at
// 500), so a pass that outlives its epoch neither skips nor repeats decay.
func (k Keeper) MaybeApplyBulkDecay(ctx context.Context) error {
	currentEpoch, err := k.GetCurrentEpoch(ctx)
	if err != nil {
		return err
	}
	if currentEpoch <= 0 {
		return nil
	}

	last, err := k.DecayLastProcessedEpoch.Get(ctx)
	hasLast := err == nil
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return err
	}

	passEpoch, err := k.DecayPassEpoch.Get(ctx)
	if errors.Is(err, collections.ErrNotFound) {
		if hasLast && last >= uint64(currentEpoch) {
			return nil
		}
		passEpoch = uint64(currentEpoch)
		if err := k.DecayPassEpoch.Set(ctx, passEpoch); err != nil {
			return err
		}
		if err := k.DecayPassMemberCursor.Remove(ctx); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("MaybeApplyBulkDecay: DecayPassMemberCursor.Remove failed", "err", err)
		}
		if err := k.DecayPassStakeCursor.Remove(ctx); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("MaybeApplyBulkDecay: DecayPassStakeCursor.Remove failed", "err", err)
		}
	} else if err != nil {
		return err
	}

	budget := maxDecayRecordsPerBlock

	// Phase 1: members.
	if has, _ := k.DecayPassStakeCursor.Has(ctx); !has {
		after, cErr := k.DecayPassMemberCursor.Get(ctx)
		if cErr != nil && !errors.Is(cErr, collections.ErrNotFound) {
			return cErr
		}
		visited, lastAddr, done, mErr := k.decayMembersBatch(ctx, after, budget)
		if mErr != nil {
			return mErr
		}
		budget -= visited
		if !done {
			return k.DecayPassMemberCursor.Set(ctx, lastAddr)
		}
		if err := k.DecayPassStakeCursor.Set(ctx, 0); err != nil {
			return err
		}
		if budget <= 0 {
			return nil
		}
	}

	// Phase 2: stake records.
	from, err := k.DecayPassStakeCursor.Get(ctx)
	if err != nil {
		return err
	}
	epochs := uint64(1)
	if hasLast && passEpoch > last {
		epochs = passEpoch - last
	}
	if epochs > 500 {
		epochs = 500
	}
	next, done, err := k.decayStakesBatch(ctx, from, budget, epochs)
	if err != nil {
		return err
	}
	if !done {
		return k.DecayPassStakeCursor.Set(ctx, next)
	}
	if err := k.DecayPassEpoch.Remove(ctx); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("MaybeApplyBulkDecay: DecayPassEpoch.Remove failed", "err", err)
	}
	if err := k.DecayPassMemberCursor.Remove(ctx); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("MaybeApplyBulkDecay: DecayPassMemberCursor.Remove failed", "err", err)
	}
	if err := k.DecayPassStakeCursor.Remove(ctx); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("MaybeApplyBulkDecay: DecayPassStakeCursor.Remove failed", "err", err)
	}
	return k.DecayLastProcessedEpoch.Set(ctx, passEpoch)
}

// decayMembersBatch applies pending unstaked decay to up to budget members
// after the address `after` (exclusive; "" = from the start). Only members
// whose record changes are written. Returns how many it read, the last
// address read and whether it reached the end of the member store.
func (k Keeper) decayMembersBatch(ctx context.Context, after string, budget int) (int, string, bool, error) {
	var rng collections.Ranger[string]
	if after != "" {
		rng = new(collections.Range[string]).StartExclusive(after)
	}
	type pending struct {
		key    string
		member types.Member
	}
	var batch []pending
	visited := 0
	lastAddr := after
	done := true
	if err := k.Member.Walk(ctx, rng, func(key string, member types.Member) (bool, error) {
		if visited >= budget {
			done = false
			return true, nil
		}
		visited++
		lastAddr = key
		batch = append(batch, pending{key, member})
		return false, nil
	}); err != nil {
		return 0, after, false, err
	}
	for _, p := range batch {
		before := p.member.LastDecayEpoch
		if err := k.ApplyPendingDecay(ctx, &p.member); err != nil {
			return 0, after, false, err
		}
		if p.member.LastDecayEpoch == before {
			continue
		}
		if err := k.Member.Set(ctx, p.key, p.member); err != nil {
			return 0, after, false, err
		}
	}
	return visited, lastAddr, done, nil
}

// DistributeEpochStakingRewards updates the seasonal reward pool accumulator.
// Called each epoch in EndBlocker. The accumulator enables O(1) lazy reward
// calculation per stake claim (MasterChef pattern).
//
// Each epoch: accPerShare += (poolRemaining / remainingEpochs) / totalStaked
// Individual rewards computed lazily: pending = stake * accPerShare - rewardDebt
//
// Member and tag stake rewards remain event-driven (unchanged):
//   - Member stakes: AccumulateMemberStakeRevenue on initiative completion
//   - Tag stakes: AccumulateTagStakeRevenue on initiative completion
//
// The epoch gate is load-bearing: EndBlocker runs this every block, and
// DistributeEpochStakingRewardsFromPool takes remaining/remainingEpochs each
// time it is called. Ungated, a season's entire staking budget would drain in a
// fraction of a season (EpochBlocks times too fast). This was invisible while
// the pool was never initialised and every call returned early.
func (k Keeper) DistributeEpochStakingRewards(ctx context.Context) error {
	isEpochEnd, err := k.IsEpochEnd(ctx)
	if err != nil {
		return err
	}
	if !isEpochEnd {
		return nil
	}
	return k.DistributeEpochStakingRewardsFromPool(ctx)
}

// UpdateAllTrustLevels updates trust levels for all members
func (k Keeper) UpdateAllTrustLevels(ctx context.Context) error {
	return k.Member.Walk(ctx, nil, func(key string, member types.Member) (stop bool, err error) {
		memberAddr, err := sdk.AccAddressFromBech32(member.Address)
		if err == nil {
			if err := k.UpdateTrustLevel(ctx, memberAddr); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("UpdateAllTrustLevels: k.UpdateTrustLevel failed", "err", err)
			}
		}
		return false, nil
	})
}

// ProcessExpiredAccountability expires PENDING invitations whose acceptance
// window has passed. Runs once per epoch (at the epoch-end block). Without
// it a never-accepted invitation would lock the inviter's stake forever and
// keep the invitee address permanently reserved. Accountability for
// accepted invitations is passive (checked when the invitee is penalized).
//
// Reads the PENDING index in creation order and stops at the first invitation
// still inside its window (REP-E-10): the old walk read every invitation ever
// made, every epoch. Each close runs in its own cache context, so a failure
// leaves that invitation untouched for the next epoch instead of half-closed.
func (k Keeper) ProcessExpiredAccountability(ctx context.Context) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	if params.InvitationAcceptanceEpochs <= 0 {
		return nil
	}
	isEnd, err := k.IsEpochEnd(ctx)
	if err != nil || !isEnd {
		return err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	// The window is counted in epochs from the invitation's created_epoch
	// (REP-E-16): epochs follow block height, so the window does not depend
	// on the block time. Expiry needs window full epochs to have passed.
	currentEpoch, err := k.GetCurrentEpoch(ctx)
	if err != nil {
		return err
	}
	window := params.InvitationAcceptanceEpochs

	var expired []types.Invitation
	if err := k.PendingInvitations.Walk(ctx, nil, func(key collections.Pair[int64, uint64]) (bool, error) {
		if currentEpoch <= key.K1()+window {
			return true, nil // ordered by creation: everything after is younger
		}
		inv, err := k.Invitation.Get(ctx, key.K2())
		if err != nil || inv.Status != types.InvitationStatus_INVITATION_STATUS_PENDING {
			return false, nil
		}
		expired = append(expired, inv)
		return false, nil
	}); err != nil {
		return err
	}
	for _, inv := range expired {
		cacheCtx, write := sdkCtx.CacheContext()
		if err := k.closePendingInvitation(cacheCtx, inv, types.InvitationStatus_INVITATION_STATUS_EXPIRED, "invitation_expired"); err != nil {
			sdkCtx.Logger().Error("failed to expire invitation", "invitation_id", inv.Id, "error", err)
			continue
		}
		write()
	}
	return nil
}

// endPendingInvitation records a PENDING invitation's terminal status and
// removes it from the PENDING and invitee indexes. It moves no DREAM.
func (k Keeper) endPendingInvitation(ctx context.Context, inv types.Invitation, status types.InvitationStatus) error {
	if err := k.removePendingInvitation(ctx, inv); err != nil {
		return err
	}
	inv.Status = status
	if err := k.Invitation.Set(ctx, inv.Id, inv); err != nil {
		return err
	}
	// Free the invitee slot only if it still points at this invitation.
	if id, err := k.InvitationsByInvitee.Get(ctx, inv.InviteeAddress); err == nil && id == inv.Id {
		if err := k.InvitationsByInvitee.Remove(ctx, inv.InviteeAddress); err != nil {
			return err
		}
	}
	return nil
}

// closePendingInvitation ends a PENDING invitation without acceptance
// (EXPIRED by the epoch sweep, REVOKED by the inviter): the inviter's stake is
// unlocked, the invitation credit it consumed is restored and the invitee
// address is freed. The record is kept for history.
//
// The unlock runs first (REP-E-06): the status used to be written before it,
// on the EndBlocker's deliver state, so a failed unlock left the invitation
// closed with the stake still locked. The inviter is resolved through the
// alias (REP-E-07): the invitation keeps the address it was sent from, and a
// raw lookup after a migration skipped the unlock and the credit entirely.
//
// The credit comes back only to an ACTIVE inviter and never above the trust
// level's maximum (REP-E-14): an invitation sent last season and closed after
// the lazy season reset would otherwise leave the member over the cap, and a
// zeroed inviter would get vouching power back.
func (k Keeper) closePendingInvitation(ctx context.Context, inv types.Invitation, status types.InvitationStatus, eventType string, extra ...sdk.Attribute) error {
	if inv.Status != types.InvitationStatus_INVITATION_STATUS_PENDING {
		return types.ErrInvitationNotPending
	}

	inviterAddr, err := sdk.AccAddressFromBech32(inv.Inviter)
	if err != nil {
		return err
	}
	canonical := k.Canonical(ctx, inv.Inviter)
	unlocked := math.ZeroInt()
	if stake := DerefInt(inv.StakedDream); stake.IsPositive() {
		if err := k.UnlockDREAM(ctx, inviterAddr, stake); err != nil {
			// The lock is gone only when the inviter was zeroed or left; any
			// other failure keeps the invitation open for a retry.
			if !errors.Is(err, types.ErrInsufficientStake) && !errors.Is(err, types.ErrMemberNotFound) {
				return err
			}
		} else {
			unlocked = stake
		}
	}

	if err := k.endPendingInvitation(ctx, inv, status); err != nil {
		return err
	}

	if inviter, err := k.Member.Get(ctx, canonical); err == nil &&
		inviter.Status == types.MemberStatus_MEMBER_STATUS_ACTIVE {
		params, err := k.Params.Get(ctx)
		if err != nil {
			return err
		}
		if maxCredits := GetMaxInvitationCredits(params.TrustLevelConfig, inviter.TrustLevel); inviter.InvitationCredits < maxCredits {
			inviter.InvitationCredits++
			if err := k.Member.Set(ctx, canonical, inviter); err != nil {
				return err
			}
		}
	}

	attrs := append([]sdk.Attribute{
		sdk.NewAttribute("invitation_id", fmt.Sprintf("%d", inv.Id)),
		sdk.NewAttribute("inviter", inv.Inviter),
		sdk.NewAttribute("invitee", inv.InviteeAddress),
		sdk.NewAttribute("stake_unlocked", unlocked.String()),
	}, extra...)
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(eventType, attrs...))
	return nil
}
