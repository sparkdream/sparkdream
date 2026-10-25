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

// MaybeApplyBulkDecay applies decay exactly once per epoch: the unstaked decay
// to every member, then the staked decay to every reward-bearing stake record
// (decayStakes). It runs at the top of EndBlocker so all subsequent reads
// within the epoch (whether via lazy ApplyPendingDecay on write paths or via
// gRPC queries on a cache-wrapped context) observe the same post-decay state —
// fixing the lazy-decay view inconsistency (x-rep-security.md Point 6). Cost:
// O(members) + O(stakes) once per epoch; for the 1000-member target this is
// cheap and amortizes across the epoch's blocks.
func (k Keeper) MaybeApplyBulkDecay(ctx context.Context) error {
	currentEpoch, err := k.GetCurrentEpoch(ctx)
	if err != nil {
		return err
	}
	if currentEpoch <= 0 {
		return nil
	}

	last, err := k.DecayLastProcessedEpoch.Get(ctx)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return err
	}
	if err == nil && last >= uint64(currentEpoch) {
		return nil
	}

	if err := k.ApplyDecay(ctx); err != nil {
		return err
	}
	if err := k.decayStakes(ctx); err != nil {
		return err
	}
	return k.DecayLastProcessedEpoch.Set(ctx, uint64(currentEpoch))
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
			_ = k.UpdateTrustLevel(ctx, memberAddr)
		}
		return false, nil
	})
}

// ProcessExpiredAccountability expires PENDING invitations whose acceptance
// window has passed. Runs once per epoch (at the epoch-end block). Without
// it a never-accepted invitation would lock the inviter's stake forever and
// keep the invitee address permanently reserved. Accountability for
// accepted invitations is passive (checked when the invitee is penalized).
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
	now := sdkCtx.BlockTime().Unix()
	// Same seconds-per-epoch estimate CreateInvitation uses for
	// accountability_end, so both deadlines move together.
	window := params.InvitationAcceptanceEpochs * params.EpochBlocks * 6

	var expired []types.Invitation
	if err := k.Invitation.Walk(ctx, nil, func(_ uint64, inv types.Invitation) (bool, error) {
		if inv.Status == types.InvitationStatus_INVITATION_STATUS_PENDING && now > inv.CreatedAt+window {
			expired = append(expired, inv)
		}
		return false, nil
	}); err != nil {
		return err
	}
	for _, inv := range expired {
		if err := k.closePendingInvitation(ctx, inv, types.InvitationStatus_INVITATION_STATUS_EXPIRED, "invitation_expired"); err != nil {
			sdkCtx.Logger().Error("failed to expire invitation", "invitation_id", inv.Id, "error", err)
		}
	}
	return nil
}

// closePendingInvitation ends a PENDING invitation without acceptance
// (EXPIRED by the epoch sweep, REVOKED by the inviter): the status is
// recorded, the inviter's stake is unlocked, the invitation credit it
// consumed is restored and the invitee address is freed. The record is kept
// for history.
func (k Keeper) closePendingInvitation(ctx context.Context, inv types.Invitation, status types.InvitationStatus, eventType string) error {
	if inv.Status != types.InvitationStatus_INVITATION_STATUS_PENDING {
		return types.ErrInvitationNotPending
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

	inviterAddr, err := sdk.AccAddressFromBech32(inv.Inviter)
	if err != nil {
		return err
	}
	unlocked := math.ZeroInt()
	if _, err := k.Member.Get(ctx, inv.Inviter); err == nil {
		if inv.StakedDream != nil && inv.StakedDream.IsPositive() {
			// UnlockDREAM caps at the actual staked balance (decay may have
			// reduced it).
			if err := k.UnlockDREAM(ctx, inviterAddr, *inv.StakedDream); err != nil {
				return err
			}
			unlocked = *inv.StakedDream
		}
		// Reload: UnlockDREAM rewrote the member.
		inviter, err := k.Member.Get(ctx, inv.Inviter)
		if err == nil {
			inviter.InvitationCredits++
			if err := k.Member.Set(ctx, inv.Inviter, inviter); err != nil {
				return err
			}
		}
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
		sdk.NewEvent(
			eventType,
			sdk.NewAttribute("invitation_id", fmt.Sprintf("%d", inv.Id)),
			sdk.NewAttribute("inviter", inv.Inviter),
			sdk.NewAttribute("invitee", inv.InviteeAddress),
			sdk.NewAttribute("stake_unlocked", unlocked.String()),
		),
	)
	return nil
}
