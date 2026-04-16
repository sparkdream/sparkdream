package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// CreateInvitation creates a new invitation from an inviter to an invitee.
// The inviter must stake DREAM tokens and have available invitation credits.
func (k Keeper) CreateInvitation(ctx context.Context, inviter, invitee sdk.AccAddress, stakedAmount math.Int, vouchedTags []string) (uint64, error) {
	// Validate amount
	if stakedAmount.IsNegative() || stakedAmount.IsZero() {
		return 0, types.ErrInvalidAmount
	}

	// Lazily reset invitation credits if we're in a new season
	// This is O(1) per inviter instead of O(n) per block
	if _, err := k.EnsureInvitationCreditsReset(ctx, inviter.String()); err != nil {
		return 0, err
	}

	// Get inviter member (re-fetch after potential credit reset)
	inviterMember, err := k.Member.Get(ctx, inviter.String())
	if err != nil {
		return 0, types.ErrMemberNotFound
	}

	// Check invitation credits
	if inviterMember.InvitationCredits == 0 {
		return 0, types.ErrNoInvitationCredits
	}

	// Check if invitee already exists or has pending invitation
	_, err = k.Member.Get(ctx, invitee.String())
	if err == nil {
		return 0, types.ErrMemberAlreadyExists
	}

	// Check if there's already an invitation for this address (via secondary index)
	_, err = k.InvitationsByInvitee.Get(ctx, invitee.String())
	if err == nil {
		return 0, types.ErrInvitationAlreadyExists
	}

	// Apply decay and check balance
	if err := k.ApplyPendingDecay(ctx, &inviterMember); err != nil {
		return 0, err
	}

	if inviterMember.DreamBalance.LT(stakedAmount) {
		return 0, types.ErrInsufficientBalance
	}

	// Lock the staked DREAM
	if err := k.LockDREAM(ctx, inviter, stakedAmount); err != nil {
		return 0, err
	}

	// Reload inviter member to get updated balance/staked from LockDREAM
	inviterMember, err = k.Member.Get(ctx, inviter.String())
	if err != nil {
		return 0, types.ErrMemberNotFound
	}

	// Get current time
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentTime := sdkCtx.BlockTime().Unix()

	// Get params for accountability period
	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0, err
	}

	currentEpoch, err := k.GetCurrentEpoch(ctx)
	if err != nil {
		return 0, err
	}

	// Calculate accountability end using the configured accountability period
	accountabilityEndEpoch := currentEpoch + int64(params.InvitationAccountabilityEpochs)
	accountabilityEndTime := currentTime + (accountabilityEndEpoch-currentEpoch)*params.EpochBlocks*6 // ~6 sec per block

	// Create invitation
	invitationID, err := k.InvitationSeq.Next(ctx)
	if err != nil {
		return 0, err
	}

	referralRate := params.ReferralRewardRate
	invitation := types.Invitation{
		Id:                invitationID,
		Inviter:           inviter.String(),
		InviteeAddress:    invitee.String(),
		StakedDream:       &stakedAmount,
		VouchedTags:       vouchedTags,
		AccountabilityEnd: accountabilityEndTime,
		ReferralRate:      &referralRate,
		ReferralEnd:       accountabilityEndTime,
		ReferralEarned:    new(math.Int),
		Status:            types.InvitationStatus_INVITATION_STATUS_PENDING,
		CreatedAt:         currentTime,
		AcceptedAt:        0,
	}

	if err := k.Invitation.Set(ctx, invitationID, invitation); err != nil {
		return 0, err
	}

	// Populate the invitee -> invitation ID secondary index
	if err := k.InvitationsByInvitee.Set(ctx, invitee.String(), invitationID); err != nil {
		return 0, err
	}

	// Decrement invitation credits
	inviterMember.InvitationCredits--
	if err := k.Member.Set(ctx, inviter.String(), inviterMember); err != nil {
		return 0, err
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"create_invitation",
			sdk.NewAttribute("invitation_id", fmt.Sprintf("%d", invitationID)),
			sdk.NewAttribute("inviter", inviter.String()),
			sdk.NewAttribute("invitee", invitee.String()),
			sdk.NewAttribute("staked_amount", stakedAmount.String()),
		),
	)

	return invitationID, nil
}

// AcceptInvitation processes an invitation acceptance and creates a new member.
func (k Keeper) AcceptInvitation(ctx context.Context, invitationID uint64, invitee sdk.AccAddress) error {
	// Get invitation
	invitation, err := k.Invitation.Get(ctx, invitationID)
	if err != nil {
		return types.ErrInvitationNotFound
	}

	// Validate invitation status
	if invitation.Status != types.InvitationStatus_INVITATION_STATUS_PENDING {
		return types.ErrInvitationNotPending
	}

	// Validate invitee address matches
	if invitation.InviteeAddress != invitee.String() {
		return types.ErrInviteeAddressMismatch
	}

	// Check if member already exists
	_, err = k.Member.Get(ctx, invitee.String())
	if err == nil {
		return types.ErrMemberAlreadyExists
	}

	// Get inviter member to build invitation chain
	inviterMember, err := k.Member.Get(ctx, invitation.Inviter)
	if err != nil {
		return types.ErrMemberNotFound
	}

	// Get current time, epoch, and season
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentTime := sdkCtx.BlockTime().Unix()
	currentEpoch, err := k.GetCurrentEpoch(ctx)
	if err != nil {
		return err
	}
	currentSeason, err := k.GetCurrentSeason(ctx)
	if err != nil {
		return err
	}

	// Build invitation chain (max 5 ancestors)
	invitationChain := make([]string, 0, 5)
	invitationChain = append(invitationChain, invitation.Inviter)
	for i := 0; i < len(inviterMember.InvitationChain) && i < 4; i++ {
		invitationChain = append(invitationChain, inviterMember.InvitationChain[i])
	}

	// Create new member
	newMember := types.Member{
		Address:             invitee.String(),
		DreamBalance:        new(math.Int),
		StakedDream:         new(math.Int),
		LifetimeEarned:      new(math.Int),
		LifetimeBurned:      new(math.Int),
		ReputationScores:    make(map[string]string),
		LifetimeReputation:  make(map[string]string),
		TrustLevel:          types.TrustLevel_TRUST_LEVEL_NEW,
		TrustLevelUpdatedAt: currentTime,
		JoinedSeason:        uint32(currentSeason),
		JoinedAt:            currentTime,
		InvitedBy:           invitation.Inviter,
		InvitationChain:     invitationChain,
		InvitationCredits:   0, // New members start with 0 credits
		Status:              types.MemberStatus_MEMBER_STATUS_ACTIVE,
		ZeroedAt:            0,
		ZeroedCount:         0,
		LastDecayEpoch:      currentEpoch,
		GiftsSentThisEpoch:  new(math.Int), // Initialize gift tracking
		LastGiftEpoch:       0,
	}

	// Initialize vouched tag reputations
	for _, tag := range invitation.VouchedTags {
		newMember.ReputationScores[tag] = "0"
		newMember.LifetimeReputation[tag] = "0"
	}

	// Save new member
	if err := k.Member.Set(ctx, invitee.String(), newMember); err != nil {
		return err
	}

	// Mark trust tree dirty — new member joined
	k.MarkMemberDirty(ctx, invitee.String())

	// Update invitation status
	invitation.Status = types.InvitationStatus_INVITATION_STATUS_ACCEPTED
	invitation.AcceptedAt = currentTime
	if err := k.Invitation.Set(ctx, invitationID, invitation); err != nil {
		return err
	}

	// Return staked DREAM to inviter (minus anti-sybil burn)
	inviterAddr, err := sdk.AccAddressFromBech32(invitation.Inviter)
	if err != nil {
		return err
	}

	// Get params for burn rate
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	stakedAmount := *invitation.StakedDream
	burnAmount := params.InvitationStakeBurnRate.MulInt(stakedAmount).TruncateInt()

	// Unlock full amount from staked balance, then burn the fraction from free balance.
	// UnlockDREAM handles decay-induced shortfalls by capping to actual staked balance.
	if err := k.UnlockDREAM(ctx, inviterAddr, stakedAmount); err != nil {
		return err
	}
	if burnAmount.IsPositive() {
		if err := k.BurnDREAM(ctx, inviterAddr, burnAmount); err != nil {
			return err
		}
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"accept_invitation",
			sdk.NewAttribute("invitation_id", fmt.Sprintf("%d", invitationID)),
			sdk.NewAttribute("inviter", invitation.Inviter),
			sdk.NewAttribute("new_member", invitee.String()),
			sdk.NewAttribute("stake_burned", burnAmount.String()),
		),
	)

	return nil
}

// ProcessInviterAccountability slashes an inviter if their invitee fails during accountability period.
// This is called when an invitee is zeroed or severely penalized.
func (k Keeper) ProcessInviterAccountability(ctx context.Context, invitee sdk.AccAddress, reason string) error {
	// Get invitee member
	_, err := k.Member.Get(ctx, invitee.String())
	if err != nil {
		return types.ErrMemberNotFound
	}

	// Check if still in accountability period
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentTime := sdkCtx.BlockTime().Unix()

	// Find the invitation via secondary index
	invitationID, err := k.InvitationsByInvitee.Get(ctx, invitee.String())
	if err != nil {
		return types.ErrInvitationNotFound
	}
	inv, err := k.Invitation.Get(ctx, invitationID)
	if err != nil {
		return types.ErrInvitationNotFound
	}
	if inv.Status != types.InvitationStatus_INVITATION_STATUS_ACCEPTED {
		return types.ErrInvitationNotFound
	}
	invitation := &inv

	// Check if still in accountability period
	if currentTime > invitation.AccountabilityEnd {
		return nil // Accountability period has ended, no penalty
	}

	// Get inviter
	inviterAddr, err := sdk.AccAddressFromBech32(invitation.Inviter)
	if err != nil {
		return err
	}

	inviterMember, err := k.Member.Get(ctx, inviterAddr.String())
	if err != nil {
		return types.ErrMemberNotFound
	}

	// Calculate penalty (burn the staked amount that was returned)
	penaltyAmount := *invitation.StakedDream

	// Apply decay before slashing
	if err := k.ApplyPendingDecay(ctx, &inviterMember); err != nil {
		return err
	}

	// Slash inviter (burn DREAM)
	if inviterMember.DreamBalance.GTE(penaltyAmount) {
		if err := k.BurnDREAM(ctx, inviterAddr, penaltyAmount); err != nil {
			return err
		}
	} else {
		// Burn what's available
		if inviterMember.DreamBalance.IsPositive() {
			if err := k.BurnDREAM(ctx, inviterAddr, *inviterMember.DreamBalance); err != nil {
				return err
			}
		}
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"inviter_accountability",
			sdk.NewAttribute("inviter", inviterAddr.String()),
			sdk.NewAttribute("failed_invitee", invitee.String()),
			sdk.NewAttribute("penalty", penaltyAmount.String()),
			sdk.NewAttribute("reason", reason),
		),
	)

	return nil
}

// CalculateReferralReward calculates and pays referral rewards to an inviter.
// This is called when an invitee earns DREAM during the referral period.
func (k Keeper) CalculateReferralReward(ctx context.Context, invitee sdk.AccAddress, earnedAmount math.Int) error {
	// Get invitee member
	inviteeMember, err := k.Member.Get(ctx, invitee.String())
	if err != nil {
		return types.ErrMemberNotFound
	}

	if inviteeMember.InvitedBy == "" {
		return nil // Not an invited member
	}

	// Find the invitation via secondary index
	invitationID, err := k.InvitationsByInvitee.Get(ctx, invitee.String())
	if err != nil {
		return nil // No invitation found
	}
	inv, err := k.Invitation.Get(ctx, invitationID)
	if err != nil {
		return nil // No invitation found
	}
	if inv.Status != types.InvitationStatus_INVITATION_STATUS_ACCEPTED {
		return nil // Invitation not accepted
	}
	invitation := &inv

	// Check if still in referral period
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentTime := sdkCtx.BlockTime().Unix()
	if currentTime > invitation.ReferralEnd {
		return nil // Referral period has ended
	}

	// Check if ReferralRate is set
	if invitation.ReferralRate == nil || invitation.ReferralRate.IsZero() {
		return nil // No referral rate configured
	}

	// Calculate referral reward (based on ReferralRewardRate param)
	referralReward := invitation.ReferralRate.MulInt(earnedAmount).TruncateInt()

	if referralReward.IsZero() {
		return nil
	}

	// Mint reward to inviter
	inviterAddr, err := sdk.AccAddressFromBech32(invitation.Inviter)
	if err != nil {
		return err
	}

	if err := k.MintDREAM(ctx, inviterAddr, referralReward); err != nil {
		return err
	}

	// Update invitation record
	if invitation.ReferralEarned != nil {
		newEarned := invitation.ReferralEarned.Add(referralReward)
		invitation.ReferralEarned = &newEarned
	} else {
		invitation.ReferralEarned = &referralReward
	}
	if err := k.Invitation.Set(ctx, invitation.Id, *invitation); err != nil {
		return err
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"referral_reward",
			sdk.NewAttribute("inviter", inviterAddr.String()),
			sdk.NewAttribute("invitee", invitee.String()),
			sdk.NewAttribute("reward", referralReward.String()),
			sdk.NewAttribute("from_earnings", earnedAmount.String()),
		),
	)

	return nil
}
