package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

func TestCreateInvitation(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Setup: Create inviter member with invitation credits
	inviter := sdk.AccAddress([]byte("inviter"))
	k.Member.Set(ctx, inviter.String(), types.Member{
		Address:           inviter.String(),
		DreamBalance:      PtrInt(math.NewInt(1000)),
		StakedDream:       PtrInt(math.ZeroInt()),
		LifetimeEarned:    PtrInt(math.ZeroInt()),
		LifetimeBurned:    PtrInt(math.ZeroInt()),
		ReputationScores:  map[string]string{"backend": "100.0"},
		InvitationCredits: 3,
		TrustLevel:        types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
	})

	invitee := sdk.AccAddress([]byte("invitee"))
	stakedAmount := math.NewInt(100)
	tags := []string{"backend", "frontend"}

	// Test: Create invitation
	invitationID, err := k.CreateInvitation(ctx, inviter, invitee, stakedAmount, tags)
	require.NoError(t, err)
	require.NoError(t, err)

	// Verify invitation
	invitation, err := k.Invitation.Get(ctx, invitationID)
	require.NoError(t, err)
	require.Equal(t, inviter.String(), invitation.Inviter)
	require.Equal(t, invitee.String(), invitation.InviteeAddress)
	require.Equal(t, stakedAmount.String(), invitation.StakedDream.String())
	require.Equal(t, tags, invitation.VouchedTags)
	require.Equal(t, types.InvitationStatus_INVITATION_STATUS_PENDING, invitation.Status)

	// Verify invitation credits decremented
	inviterMember, err := k.Member.Get(ctx, inviter.String())
	require.NoError(t, err)
	require.Equal(t, uint32(2), inviterMember.InvitationCredits)

	// Verify DREAM was locked
	require.Equal(t, stakedAmount.String(), inviterMember.StakedDream.String())
}

func TestCreateInvitationErrors(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	inviter := sdk.AccAddress([]byte("inviter"))
	invitee := sdk.AccAddress([]byte("invitee"))

	// Test: No invitation credits
	k.Member.Set(ctx, inviter.String(), types.Member{
		Address:           inviter.String(),
		DreamBalance:      PtrInt(math.NewInt(1000)),
		StakedDream:       PtrInt(math.ZeroInt()),
		LifetimeEarned:    PtrInt(math.ZeroInt()),
		LifetimeBurned:    PtrInt(math.ZeroInt()),
		ReputationScores:  make(map[string]string),
		InvitationCredits: 0, // No credits
	})

	_, err := k.CreateInvitation(ctx, inviter, invitee, math.NewInt(100), []string{"tag"})
	require.ErrorIs(t, err, types.ErrNoInvitationCredits)

	// Test: Insufficient balance
	k.Member.Set(ctx, inviter.String(), types.Member{
		Address:           inviter.String(),
		DreamBalance:      PtrInt(math.NewInt(10)), // Too low
		StakedDream:       PtrInt(math.ZeroInt()),
		LifetimeEarned:    PtrInt(math.ZeroInt()),
		LifetimeBurned:    PtrInt(math.ZeroInt()),
		ReputationScores:  make(map[string]string),
		InvitationCredits: 1,
	})

	_, err = k.CreateInvitation(ctx, inviter, invitee, math.NewInt(100), []string{"tag"})
	require.ErrorIs(t, err, types.ErrInsufficientBalance)

	// Test: Invitee already exists
	k.Member.Set(ctx, inviter.String(), types.Member{
		Address:           inviter.String(),
		DreamBalance:      PtrInt(math.NewInt(1000)),
		StakedDream:       PtrInt(math.ZeroInt()),
		LifetimeEarned:    PtrInt(math.ZeroInt()),
		LifetimeBurned:    PtrInt(math.ZeroInt()),
		ReputationScores:  make(map[string]string),
		InvitationCredits: 1,
	})
	k.Member.Set(ctx, invitee.String(), types.Member{
		Address:          invitee.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: make(map[string]string),
	})

	_, err = k.CreateInvitation(ctx, inviter, invitee, math.NewInt(100), []string{"tag"})
	require.ErrorIs(t, err, types.ErrMemberAlreadyExists)
}

func TestAcceptInvitation(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Setup: Create invitation
	inviter := sdk.AccAddress([]byte("inviter"))
	k.Member.Set(ctx, inviter.String(), types.Member{
		Address:           inviter.String(),
		DreamBalance:      PtrInt(math.NewInt(1000)),
		StakedDream:       PtrInt(math.ZeroInt()),
		LifetimeEarned:    PtrInt(math.ZeroInt()),
		LifetimeBurned:    PtrInt(math.ZeroInt()),
		ReputationScores:  map[string]string{"backend": "100.0"},
		InvitationCredits: 3,
		InvitationChain:   []string{"founder1", "founder2"},
	})

	invitee := sdk.AccAddress([]byte("invitee"))
	stakedAmount := math.NewInt(100)
	tags := []string{"backend", "frontend"}

	invitationID, err := k.CreateInvitation(ctx, inviter, invitee, stakedAmount, tags)
	require.NoError(t, err)

	// Test: Accept invitation
	err = k.AcceptInvitation(ctx, invitationID, invitee)
	require.NoError(t, err)

	// Verify new member created
	newMember, err := k.Member.Get(ctx, invitee.String())
	require.NoError(t, err)
	require.Equal(t, invitee.String(), newMember.Address)
	require.Equal(t, inviter.String(), newMember.InvitedBy)
	require.Equal(t, types.MemberStatus_MEMBER_STATUS_ACTIVE, newMember.Status)
	require.Equal(t, types.TrustLevel_TRUST_LEVEL_NEW, newMember.TrustLevel)

	// Verify invitation chain (max 5 ancestors)
	require.Len(t, newMember.InvitationChain, 3) // inviter + 2 from inviter's chain
	require.Equal(t, inviter.String(), newMember.InvitationChain[0])
	require.Equal(t, "founder1", newMember.InvitationChain[1])
	require.Equal(t, "founder2", newMember.InvitationChain[2])

	// Verify vouched tags initialized
	require.Contains(t, newMember.ReputationScores, "backend")
	require.Contains(t, newMember.ReputationScores, "frontend")
	require.Equal(t, "0", newMember.ReputationScores["backend"])

	// Verify invitation status updated
	invitation, err := k.Invitation.Get(ctx, invitationID)
	require.NoError(t, err)
	require.Equal(t, types.InvitationStatus_INVITATION_STATUS_ACCEPTED, invitation.Status)
	require.NotZero(t, invitation.AcceptedAt)

	// Verify stake returned to inviter
	inviterMember, err := k.Member.Get(ctx, inviter.String())
	require.NoError(t, err)
	require.Equal(t, math.ZeroInt().String(), inviterMember.StakedDream.String())
}

func TestAcceptInvitationErrors(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	inviter := sdk.AccAddress([]byte("inviter"))
	k.Member.Set(ctx, inviter.String(), types.Member{
		Address:           inviter.String(),
		DreamBalance:      PtrInt(math.NewInt(1000)),
		StakedDream:       PtrInt(math.ZeroInt()),
		LifetimeEarned:    PtrInt(math.ZeroInt()),
		LifetimeBurned:    PtrInt(math.ZeroInt()),
		ReputationScores:  make(map[string]string),
		InvitationCredits: 1,
	})

	invitee := sdk.AccAddress([]byte("invitee"))
	invitationID, _ := k.CreateInvitation(ctx, inviter, invitee, math.NewInt(100), []string{"tag"})

	// Test: Wrong invitee address
	wrongInvitee := sdk.AccAddress([]byte("wrong"))
	err := k.AcceptInvitation(ctx, invitationID, wrongInvitee)
	require.ErrorIs(t, err, types.ErrInviteeAddressMismatch)

	// Accept invitation
	err = k.AcceptInvitation(ctx, invitationID, invitee)
	require.NoError(t, err)

	// Test: Already accepted (not pending)
	err = k.AcceptInvitation(ctx, invitationID, invitee)
	require.ErrorIs(t, err, types.ErrInvitationNotPending)
}

func TestReferralReward(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Setup: Create and accept invitation
	inviter := sdk.AccAddress([]byte("inviter"))
	k.Member.Set(ctx, inviter.String(), types.Member{
		Address:           inviter.String(),
		DreamBalance:      PtrInt(math.NewInt(1000)),
		StakedDream:       PtrInt(math.ZeroInt()),
		LifetimeEarned:    PtrInt(math.ZeroInt()),
		LifetimeBurned:    PtrInt(math.ZeroInt()),
		ReputationScores:  make(map[string]string),
		InvitationCredits: 1,
	})

	invitee := sdk.AccAddress([]byte("invitee"))
	invitationID, _ := k.CreateInvitation(ctx, inviter, invitee, math.NewInt(100), []string{"tag"})
	err := k.AcceptInvitation(ctx, invitationID, invitee)
	require.NoError(t, err)

	// Get inviter's initial balance
	inviterMember, _ := k.Member.Get(ctx, inviter.String())
	initialBalance := *inviterMember.DreamBalance

	// Test: Invitee earns DREAM (via MintDREAM), inviter should get referral reward automatically
	earnedAmount := math.NewInt(1000)
	err = k.MintDREAM(ctx, invitee, earnedAmount)
	require.NoError(t, err)

	// Verify inviter received 5% referral reward (automatically via MintDREAM integration)
	inviterMember, _ = k.Member.Get(ctx, inviter.String())
	expectedReward := math.LegacyNewDecWithPrec(5, 2).MulInt(earnedAmount).TruncateInt() // 5%
	expectedBalance := initialBalance.Add(expectedReward)
	require.Equal(t, expectedBalance.String(), inviterMember.DreamBalance.String())

	// Verify invitation record updated
	invitation, _ := k.Invitation.Get(ctx, invitationID)
	require.Equal(t, expectedReward.String(), invitation.ReferralEarned.String())
}

// TestReferralRewardAutomatic verifies that referral rewards are automatically
// calculated when invitees earn DREAM through various mechanisms (not just direct minting).
func TestReferralRewardAutomatic(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Setup: Create inviter and invitee
	inviter := sdk.AccAddress([]byte("inviter"))
	k.Member.Set(ctx, inviter.String(), types.Member{
		Address:           inviter.String(),
		DreamBalance:      PtrInt(math.NewInt(2000)),
		StakedDream:       PtrInt(math.ZeroInt()),
		LifetimeEarned:    PtrInt(math.ZeroInt()),
		LifetimeBurned:    PtrInt(math.ZeroInt()),
		ReputationScores:  map[string]string{"dev": "100.0"},
		InvitationCredits: 1,
	})

	invitee := sdk.AccAddress([]byte("invitee"))
	invitationID, _ := k.CreateInvitation(ctx, inviter, invitee, math.NewInt(100), []string{"dev"})
	err := k.AcceptInvitation(ctx, invitationID, invitee)
	require.NoError(t, err)

	// Get inviter's initial balance
	inviterMember, _ := k.Member.Get(ctx, inviter.String())
	initialBalance := *inviterMember.DreamBalance

	// Scenario: Invitee earns DREAM through multiple mechanisms
	// All should automatically trigger referral rewards

	// 1. Direct minting (already tested above, but showing it works)
	directEarning := math.NewInt(500)
	err = k.MintDREAM(ctx, invitee, directEarning)
	require.NoError(t, err)

	// 2. Additional earnings (e.g., from staking rewards, jury duty, etc.)
	additionalEarning := math.NewInt(300)
	err = k.MintDREAM(ctx, invitee, additionalEarning)
	require.NoError(t, err)

	// Verify inviter received cumulative 5% referral rewards
	inviterMember, _ = k.Member.Get(ctx, inviter.String())
	totalEarned := directEarning.Add(additionalEarning)
	expectedTotalReward := math.LegacyNewDecWithPrec(5, 2).MulInt(totalEarned).TruncateInt() // 5%
	expectedBalance := initialBalance.Add(expectedTotalReward)
	require.Equal(t, expectedBalance.String(), inviterMember.DreamBalance.String())

	// Verify invitation record shows cumulative referral earnings
	invitation, _ := k.Invitation.Get(ctx, invitationID)
	require.Equal(t, expectedTotalReward.String(), invitation.ReferralEarned.String())
}

// TestCreateInvitation_LazyCreditsReset verifies that invitation credits are lazily
// reset when a member tries to invite in a new season.
func TestCreateInvitation_LazyCreditsReset(t *testing.T) {
	// Start with season 0
	fixture := initFixture(t, WithSeasonNumber(0))
	k := fixture.keeper
	ctx := fixture.ctx

	params, _ := k.Params.Get(ctx)

	// Setup: Create inviter member with 0 credits (used up), last reset at season 0
	inviter := sdk.AccAddress([]byte("inviter"))
	k.Member.Set(ctx, inviter.String(), types.Member{
		Address:               inviter.String(),
		DreamBalance:          PtrInt(math.NewInt(1000)),
		StakedDream:           PtrInt(math.ZeroInt()),
		LifetimeEarned:        PtrInt(math.ZeroInt()),
		LifetimeBurned:        PtrInt(math.ZeroInt()),
		ReputationScores:      map[string]string{"backend": "100.0"},
		InvitationCredits:     0, // Used up all credits
		TrustLevel:            types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		LastCreditResetSeason: 0, // Last reset at season 0
	})

	invitee := sdk.AccAddress([]byte("invitee"))
	stakedAmount := math.NewInt(100)
	tags := []string{"backend"}

	// Test at season 0: Should fail (no credits)
	_, err := k.CreateInvitation(ctx, inviter, invitee, stakedAmount, tags)
	require.ErrorIs(t, err, types.ErrNoInvitationCredits)

	// Change mock to return season 1
	fixture.seasonKeeper.GetCurrentSeasonFn = func(ctx context.Context) (types.SeasonState, error) {
		return types.SeasonState{Number: 1}, nil
	}

	// Test at season 1: Should succeed (credits lazily reset)
	invitationID, err := k.CreateInvitation(ctx, inviter, invitee, stakedAmount, tags)
	require.NoError(t, err)
	require.NotZero(t, invitationID)

	// Verify invitation was created
	invitation, err := k.Invitation.Get(ctx, invitationID)
	require.NoError(t, err)
	require.Equal(t, inviter.String(), invitation.Inviter)
	require.Equal(t, invitee.String(), invitation.InviteeAddress)

	// Verify credits were reset then decremented
	// ESTABLISHED has 3 credits per season, so after using 1, should have 2 left
	inviterMember, err := k.Member.Get(ctx, inviter.String())
	require.NoError(t, err)
	require.Equal(t, params.TrustLevelConfig.EstablishedInvitationCredits-1, inviterMember.InvitationCredits)
	require.Equal(t, int64(1), inviterMember.LastCreditResetSeason)
}

// TestCreateInvitation_NoResetSameSeason verifies that credits are NOT reset
// if the member has already been reset this season.
func TestCreateInvitation_NoResetSameSeason(t *testing.T) {
	// Use season 0 so member's LastCreditResetSeason=0 does NOT trigger reset
	fixture := initFixture(t, WithSeasonNumber(0))
	k := fixture.keeper
	ctx := fixture.ctx

	// Setup: Create inviter member with some credits, already reset this season
	inviter := sdk.AccAddress([]byte("inviter"))
	k.Member.Set(ctx, inviter.String(), types.Member{
		Address:               inviter.String(),
		DreamBalance:          PtrInt(math.NewInt(1000)),
		StakedDream:           PtrInt(math.ZeroInt()),
		LifetimeEarned:        PtrInt(math.ZeroInt()),
		LifetimeBurned:        PtrInt(math.ZeroInt()),
		ReputationScores:      map[string]string{"backend": "100.0"},
		InvitationCredits:     2, // Has 2 credits left
		TrustLevel:            types.TrustLevel_TRUST_LEVEL_CORE,
		LastCreditResetSeason: 0, // Already reset at season 0
	})

	invitee := sdk.AccAddress([]byte("invitee"))
	stakedAmount := math.NewInt(100)
	tags := []string{"backend"}

	// Test: Should succeed with existing credits (no reset since current season 0 == last reset season 0)
	invitationID, err := k.CreateInvitation(ctx, inviter, invitee, stakedAmount, tags)
	require.NoError(t, err)
	require.NotZero(t, invitationID)

	// Verify credits decremented from 2 to 1 (not reset to 10 for CORE)
	inviterMember, err := k.Member.Get(ctx, inviter.String())
	require.NoError(t, err)
	require.Equal(t, uint32(1), inviterMember.InvitationCredits)
	require.Equal(t, int64(0), inviterMember.LastCreditResetSeason) // Still season 0
}

// TestProcessInviterAccountability tests that inviters are slashed when their
// invitees fail during the accountability period.
func TestProcessInviterAccountability(t *testing.T) {
	t.Run("within accountability period: inviter gets slashed", func(t *testing.T) {
		fixture := initFixture(t)
		k := fixture.keeper
		ctx := fixture.ctx

		// Setup: Create inviter member with DREAM balance
		inviter := sdk.AccAddress([]byte("inviter_acc1"))
		inviterInitialDream := math.NewInt(2000)
		k.Member.Set(ctx, inviter.String(), types.Member{
			Address:           inviter.String(),
			DreamBalance:      PtrInt(inviterInitialDream),
			StakedDream:       PtrInt(math.ZeroInt()),
			LifetimeEarned:    PtrInt(math.ZeroInt()),
			LifetimeBurned:    PtrInt(math.ZeroInt()),
			ReputationScores:  map[string]string{"backend": "100.0"},
			InvitationCredits: 3,
			TrustLevel:        types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		})

		// Create invitee member
		invitee := sdk.AccAddress([]byte("invitee_acc1"))
		k.Member.Set(ctx, invitee.String(), types.Member{
			Address:          invitee.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: make(map[string]string),
			InvitedBy:        inviter.String(),
		})

		// Create an accepted invitation with AccountabilityEnd in the future (30 days from now)
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		futureEnd := sdkCtx.BlockTime().Unix() + 30*24*60*60 // 30 days in future
		stakedAmount := math.NewInt(100)

		invitation := types.Invitation{
			Id:                1,
			Inviter:           inviter.String(),
			InviteeAddress:    invitee.String(),
			StakedDream:       PtrInt(stakedAmount),
			VouchedTags:       []string{"backend"},
			AccountabilityEnd: futureEnd,
			Status:            types.InvitationStatus_INVITATION_STATUS_ACCEPTED,
			CreatedAt:         sdkCtx.BlockTime().Unix(),
			AcceptedAt:        sdkCtx.BlockTime().Unix(),
			ReferralEarned:    PtrInt(math.ZeroInt()),
		}
		err := k.Invitation.Set(ctx, 1, invitation)
		require.NoError(t, err)
		err = k.InvitationsByInvitee.Set(ctx, invitee.String(), uint64(1))
		require.NoError(t, err)

		// Process inviter accountability
		err = k.ProcessInviterAccountability(ctx, invitee, "invitee failed challenge")
		require.NoError(t, err)

		// Verify inviter's DREAM was reduced by the staked amount
		inviterMember, err := k.Member.Get(ctx, inviter.String())
		require.NoError(t, err)

		// Inviter should have lost the staked amount
		expectedBalance := inviterInitialDream.Sub(stakedAmount)
		require.Equal(t, expectedBalance.String(), inviterMember.DreamBalance.String(),
			"inviter should be slashed by staked amount during accountability period")
	})

	t.Run("past accountability period: no penalty", func(t *testing.T) {
		fixture := initFixture(t)
		k := fixture.keeper
		ctx := fixture.ctx

		// Setup: Create inviter member with DREAM balance
		inviter := sdk.AccAddress([]byte("inviter_acc2"))
		inviterInitialDream := math.NewInt(2000)
		k.Member.Set(ctx, inviter.String(), types.Member{
			Address:           inviter.String(),
			DreamBalance:      PtrInt(inviterInitialDream),
			StakedDream:       PtrInt(math.ZeroInt()),
			LifetimeEarned:    PtrInt(math.ZeroInt()),
			LifetimeBurned:    PtrInt(math.ZeroInt()),
			ReputationScores:  map[string]string{"backend": "100.0"},
			InvitationCredits: 3,
			TrustLevel:        types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		})

		// Create invitee member
		invitee := sdk.AccAddress([]byte("invitee_acc2"))
		k.Member.Set(ctx, invitee.String(), types.Member{
			Address:          invitee.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: make(map[string]string),
			InvitedBy:        inviter.String(),
		})

		// Create an accepted invitation with AccountabilityEnd in the past (1 hour ago)
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		pastEnd := sdkCtx.BlockTime().Unix() - 3600 // 1 hour ago
		stakedAmount := math.NewInt(100)

		invitation := types.Invitation{
			Id:                1,
			Inviter:           inviter.String(),
			InviteeAddress:    invitee.String(),
			StakedDream:       PtrInt(stakedAmount),
			VouchedTags:       []string{"backend"},
			AccountabilityEnd: pastEnd,
			Status:            types.InvitationStatus_INVITATION_STATUS_ACCEPTED,
			CreatedAt:         sdkCtx.BlockTime().Unix() - 60*24*3600, // 60 days ago
			AcceptedAt:        sdkCtx.BlockTime().Unix() - 60*24*3600,
			ReferralEarned:    PtrInt(math.ZeroInt()),
		}
		err := k.Invitation.Set(ctx, 1, invitation)
		require.NoError(t, err)
		err = k.InvitationsByInvitee.Set(ctx, invitee.String(), uint64(1))
		require.NoError(t, err)

		// Process inviter accountability (should be no-op since accountability ended)
		err = k.ProcessInviterAccountability(ctx, invitee, "invitee failed challenge")
		require.NoError(t, err)

		// Verify inviter's DREAM was NOT reduced
		inviterMember, err := k.Member.Get(ctx, inviter.String())
		require.NoError(t, err)
		require.Equal(t, inviterInitialDream.String(), inviterMember.DreamBalance.String(),
			"inviter should not be penalized after accountability period ends")
	})

	t.Run("inviter has insufficient DREAM: burns what is available", func(t *testing.T) {
		fixture := initFixture(t)
		k := fixture.keeper
		ctx := fixture.ctx

		// Setup: Create inviter member with less DREAM than the staked amount
		inviter := sdk.AccAddress([]byte("inviter_acc3"))
		inviterLowDream := math.NewInt(30) // Less than staked amount of 100
		k.Member.Set(ctx, inviter.String(), types.Member{
			Address:           inviter.String(),
			DreamBalance:      PtrInt(inviterLowDream),
			StakedDream:       PtrInt(math.ZeroInt()),
			LifetimeEarned:    PtrInt(math.ZeroInt()),
			LifetimeBurned:    PtrInt(math.ZeroInt()),
			ReputationScores:  map[string]string{"backend": "100.0"},
			InvitationCredits: 3,
			TrustLevel:        types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		})

		// Create invitee member
		invitee := sdk.AccAddress([]byte("invitee_acc3"))
		k.Member.Set(ctx, invitee.String(), types.Member{
			Address:          invitee.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: make(map[string]string),
			InvitedBy:        inviter.String(),
		})

		// Create an accepted invitation with AccountabilityEnd in the future
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		futureEnd := sdkCtx.BlockTime().Unix() + 30*24*60*60 // 30 days in future
		stakedAmount := math.NewInt(100)                     // More than inviter's balance

		invitation := types.Invitation{
			Id:                1,
			Inviter:           inviter.String(),
			InviteeAddress:    invitee.String(),
			StakedDream:       PtrInt(stakedAmount),
			VouchedTags:       []string{"backend"},
			AccountabilityEnd: futureEnd,
			Status:            types.InvitationStatus_INVITATION_STATUS_ACCEPTED,
			CreatedAt:         sdkCtx.BlockTime().Unix(),
			AcceptedAt:        sdkCtx.BlockTime().Unix(),
			ReferralEarned:    PtrInt(math.ZeroInt()),
		}
		err := k.Invitation.Set(ctx, 1, invitation)
		require.NoError(t, err)
		err = k.InvitationsByInvitee.Set(ctx, invitee.String(), uint64(1))
		require.NoError(t, err)

		// Process inviter accountability
		err = k.ProcessInviterAccountability(ctx, invitee, "invitee failed challenge")
		require.NoError(t, err)

		// Verify inviter's DREAM was burned to zero (burns what's available)
		inviterMember, err := k.Member.Get(ctx, inviter.String())
		require.NoError(t, err)
		require.Equal(t, math.ZeroInt().String(), inviterMember.DreamBalance.String(),
			"inviter should have all remaining DREAM burned when insufficient for full penalty")
	})
}
