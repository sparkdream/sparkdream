package keeper_test

import (
	"fmt"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

// TestCreateStakeRefusesBeyondRecordCap pins REP-E-10: a member may hold at
// most MaxStakeRecordsPerMember stake records across every target, since
// reward-bearing records are rewritten every epoch and an uncapped count
// would let one member buy unbounded per-epoch writes for a refundable
// minimum stake.
func TestCreateStakeRefusesBeyondRecordCap(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	creator := sdk.AccAddress([]byte("phoenix_creator"))
	projectID, err := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"backend"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
	require.NoError(t, err)
	require.NoError(t, k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000)))

	assignee := sdk.AccAddress([]byte("aurora_assignee"))
	require.NoError(t, k.Member.Set(ctx, assignee.String(), types.Member{
		Address:          assignee.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": "100.0"},
	}))
	initID, err := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"backend"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(100))
	require.NoError(t, err)
	require.NoError(t, k.AssignInitiativeToMember(ctx, initID, assignee))

	staker := sdk.AccAddress([]byte("zenith_staker"))
	require.NoError(t, k.Member.Set(ctx, staker.String(), types.Member{
		Address:          staker.String(),
		DreamBalance:     PtrInt(math.NewInt(100_000_000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": "50.0"},
	}))

	// Fill the staker's by-staker index up to the cap with minimal records.
	// Ids start high so they cannot collide with the sequence.
	for i := 0; i < types.MaxStakeRecordsPerMember; i++ {
		id := uint64(1_000_000 + i)
		require.NoError(t, k.Stake.Set(ctx, id, types.Stake{
			Id:               id,
			Staker:           staker.String(),
			TargetType:       types.StakeTargetType_STAKE_TARGET_MEMBER,
			TargetIdentifier: fmt.Sprintf("target-%d", i),
			Amount:           math.NewInt(2000),
		}))
		require.NoError(t, k.StakesByStaker.Set(ctx, collections.Join(staker.String(), id)))
	}

	_, err = k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(2000))
	require.ErrorIs(t, err, types.ErrTooManyStakes)
	require.ErrorContains(t, err, fmt.Sprintf("at most %d", types.MaxStakeRecordsPerMember))

	// One record below the cap is still accepted.
	last := uint64(1_000_000 + types.MaxStakeRecordsPerMember - 1)
	require.NoError(t, k.Stake.Remove(ctx, last))
	require.NoError(t, k.StakesByStaker.Remove(ctx, collections.Join(staker.String(), last)))
	_, err = k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(2000))
	require.NoError(t, err)
}

// TestCreateInvitationRefusesTooManyVouchedTags pins REP-E-16: vouched tags
// become reputation keys on the new member, so an invitation names at most
// MaxVouchedTags of them.
func TestCreateInvitationRefusesTooManyVouchedTags(t *testing.T) {
	fixture := initFixture(t, WithCustomParams(pinnedCreditsParams()))
	k := fixture.keeper
	ctx := fixture.ctx

	inviter := sdk.AccAddress([]byte("phoenix_inviter"))
	require.NoError(t, k.Member.Set(ctx, inviter.String(), types.Member{
		Address:           inviter.String(),
		DreamBalance:      PtrInt(math.NewInt(1_000_000_000)),
		StakedDream:       PtrInt(math.ZeroInt()),
		LifetimeEarned:    PtrInt(math.ZeroInt()),
		LifetimeBurned:    PtrInt(math.ZeroInt()),
		ReputationScores:  map[string]string{"backend": "100.0"},
		InvitationCredits: 3,
		TrustLevel:        types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
	}))
	invitee := sdk.AccAddress([]byte("aurora_invitee"))

	tags := make([]string, 0, types.MaxVouchedTags+1)
	for i := 0; i <= types.MaxVouchedTags; i++ {
		tags = append(tags, fmt.Sprintf("tag-%d", i))
	}
	_, err := k.CreateInvitation(ctx, inviter, invitee, math.NewInt(150_000_000), tags)
	require.ErrorIs(t, err, types.ErrTooManyVouchedTags)
	require.ErrorContains(t, err, fmt.Sprintf("at most %d", types.MaxVouchedTags))
}
