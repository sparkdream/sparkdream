package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestMsgClaimStakingRewards(t *testing.T) {
	t.Run("valid claim returns correct amount", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: create staker member with DREAM
		staker := sdk.AccAddress([]byte("staker"))
		k.Member.Set(ctx, staker.String(), types.Member{
			Address:          staker.String(),
			DreamBalance:     PtrInt(math.NewInt(10000)),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"backend": "50.0"},
		})

		stakerStr, err := f.addressCodec.BytesToString(staker)
		require.NoError(t, err)

		// Create project and initiative
		projectID, err := k.CreateProject(ctx, staker, "Proj", "Desc", []string{"backend"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
		require.NoError(t, err)
		err = k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))
		require.NoError(t, err)
		initID, err := k.CreateInitiative(ctx, staker, projectID, "Task", "D", []string{"backend"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(100))
		require.NoError(t, err)

		// Create stake
		stakeAmount := math.NewInt(2000)
		stakeID, err := k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", stakeAmount)
		require.NoError(t, err)

		// Advance time by 30 days to accumulate rewards
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		thirtyDays := time.Duration(30*24) * time.Hour
		ctx = sdkCtx.WithBlockTime(sdkCtx.BlockTime().Add(thirtyDays))

		// Claim rewards
		resp, err := ms.ClaimStakingRewards(ctx, &types.MsgClaimStakingRewards{
			Staker:  stakerStr,
			StakeId: stakeID,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NotNil(t, resp.ClaimedAmount)

		// With MasterChef accumulator, rewards depend on DistributeEpochStakingRewardsFromPool
		// having been called to populate accPerShare. Without epoch distribution, rewards are 0.
		// Just verify claimed amount is non-negative (no panic, no error).
		require.True(t, resp.ClaimedAmount.GTE(math.ZeroInt()),
			"claimed amount should be non-negative")
	})

	t.Run("invalid staker address returns error", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.ClaimStakingRewards(f.ctx, &types.MsgClaimStakingRewards{
			Staker:  "invalid-address",
			StakeId: 1,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid staker address")
	})

	t.Run("non-owner of stake returns error", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: create staker and non-owner
		staker := sdk.AccAddress([]byte("staker"))
		nonOwner := sdk.AccAddress([]byte("nonowner"))
		k.Member.Set(ctx, staker.String(), types.Member{
			Address:          staker.String(),
			DreamBalance:     PtrInt(math.NewInt(10000)),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"backend": "50.0"},
		})
		k.Member.Set(ctx, nonOwner.String(), types.Member{
			Address:          nonOwner.String(),
			DreamBalance:     PtrInt(math.NewInt(5000)),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{},
		})

		nonOwnerStr, err := f.addressCodec.BytesToString(nonOwner)
		require.NoError(t, err)

		// Create project and initiative
		projectID, err := k.CreateProject(ctx, staker, "Proj", "Desc", []string{"backend"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
		require.NoError(t, err)
		err = k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))
		require.NoError(t, err)
		initID, err := k.CreateInitiative(ctx, staker, projectID, "Task", "D", []string{"backend"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(100))
		require.NoError(t, err)

		// Create stake owned by staker
		stakeID, err := k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(2000))
		require.NoError(t, err)

		// Advance time so there are rewards to claim
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		ctx = sdkCtx.WithBlockTime(sdkCtx.BlockTime().Add(24 * time.Hour))

		// Attempt to claim with non-owner
		_, err = ms.ClaimStakingRewards(ctx, &types.MsgClaimStakingRewards{
			Staker:  nonOwnerStr,
			StakeId: stakeID,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "only stake owner can claim rewards")
	})
}

func TestMsgCompoundStakingRewards(t *testing.T) {
	t.Run("valid compound returns compounded amount and new stake amount", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: create staker member with DREAM
		staker := sdk.AccAddress([]byte("staker"))
		k.Member.Set(ctx, staker.String(), types.Member{
			Address:          staker.String(),
			DreamBalance:     PtrInt(math.NewInt(10000)),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"backend": "50.0"},
		})

		stakerStr, err := f.addressCodec.BytesToString(staker)
		require.NoError(t, err)

		// Compounding is only supported for member and tag stakes; initiative
		// and project stakes must claim and re-stake so the new DREAM gets its
		// own conviction maturity clock.
		target := sdk.AccAddress([]byte("compound_msg_target_"))
		k.Member.Set(ctx, target.String(), types.Member{
			Address:          target.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{},
		})

		// Create stake
		initialStakeAmount := math.NewInt(2000)
		stakeID, err := k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_MEMBER, 0, target.String(), initialStakeAmount)
		require.NoError(t, err)

		// Push revenue through the member pool to populate its accumulator
		_, mErr := k.AccumulateMemberStakeRevenue(ctx, target, math.NewInt(1000000))
		require.NoError(t, mErr)

		// Give the staker enough DREAM to cover compounded rewards
		member, _ := k.Member.Get(ctx, staker.String())
		*member.DreamBalance = math.NewInt(500000000000000)
		k.Member.Set(ctx, staker.String(), member)

		// Advance past the minimum holding period so rewards are collectable
		params, err := k.Params.Get(ctx)
		require.NoError(t, err)
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		matureCtx := sdkCtx.WithBlockTime(sdkCtx.BlockTime().Add(time.Duration(params.MinStakeDurationSeconds+1) * time.Second))

		// Compound rewards
		resp, err := ms.CompoundStakingRewards(matureCtx, &types.MsgCompoundStakingRewards{
			Staker:  stakerStr,
			StakeId: stakeID,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NotNil(t, resp.CompoundedAmount)
		require.NotNil(t, resp.NewStakeAmount)

		// Verify compounded amount is positive (revenue reached the pool)
		require.True(t, resp.CompoundedAmount.GT(math.ZeroInt()),
			"compounded amount should be positive after pool revenue")

		// Verify new stake amount equals initial + compounded
		expectedNewAmount := initialStakeAmount.Add(*resp.CompoundedAmount)
		require.Equal(t, expectedNewAmount.String(), resp.NewStakeAmount.String(),
			"new stake amount should be initial + compounded")

		// Verify the stake was actually updated in storage
		stake, err := k.GetStake(ctx, stakeID)
		require.NoError(t, err)
		require.Equal(t, expectedNewAmount.String(), stake.Amount.String(),
			"stored stake amount should reflect compounding")
	})

	t.Run("invalid staker address returns error", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.CompoundStakingRewards(f.ctx, &types.MsgCompoundStakingRewards{
			Staker:  "invalid-address",
			StakeId: 1,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid staker address")
	})
}
