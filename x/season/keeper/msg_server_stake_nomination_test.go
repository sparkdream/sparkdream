package keeper_test

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerStakeNomination(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initBeginBlockFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.StakeNomination(f.ctx, &types.MsgStakeNomination{
			Creator:      "invalid-address",
			NominationId: 1,
			Amount:       "100",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("no active season", func(t *testing.T) {
		f := initBeginBlockFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		stakerStr, err := f.addressCodec.BytesToString(TestAddrMember1)
		require.NoError(t, err)

		// No season set at all
		_, err = ms.StakeNomination(f.ctx, &types.MsgStakeNomination{
			Creator:      stakerStr,
			NominationId: 1,
			Amount:       "100",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrSeasonNotActive)
	})

	t.Run("season not in nomination phase", func(t *testing.T) {
		f := initBeginBlockFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		stakerStr, err := f.addressCodec.BytesToString(TestAddrMember1)
		require.NoError(t, err)

		// Set season with ACTIVE status (not NOMINATION)
		season := types.Season{
			Number:     1,
			Name:       "Test Season",
			StartBlock: 0,
			EndBlock:   100000,
			Status:     types.SeasonStatus_SEASON_STATUS_ACTIVE,
		}
		err = f.keeper.Season.Set(f.ctx, season)
		require.NoError(t, err)

		_, err = ms.StakeNomination(f.ctx, &types.MsgStakeNomination{
			Creator:      stakerStr,
			NominationId: 1,
			Amount:       "100",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrSeasonNotInNominationPhase)
	})

	t.Run("nomination not found", func(t *testing.T) {
		f := initBeginBlockFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		stakerStr, err := f.addressCodec.BytesToString(TestAddrMember1)
		require.NoError(t, err)

		// Set season in NOMINATION phase
		season := types.Season{
			Number:     1,
			Name:       "Test Season",
			StartBlock: 0,
			EndBlock:   100000,
			Status:     types.SeasonStatus_SEASON_STATUS_NOMINATION,
		}
		err = f.keeper.Season.Set(f.ctx, season)
		require.NoError(t, err)

		// Don't create any nomination -- ID 999 does not exist
		_, err = ms.StakeNomination(f.ctx, &types.MsgStakeNomination{
			Creator:      stakerStr,
			NominationId: 999,
			Amount:       "100",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNominationNotFound)
	})

	t.Run("not a member", func(t *testing.T) {
		f := initBeginBlockFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		nominatorStr, err := f.addressCodec.BytesToString(TestAddrCreator)
		require.NoError(t, err)
		stakerStr, err := f.addressCodec.BytesToString(TestAddrMember1)
		require.NoError(t, err)

		// Set season in NOMINATION phase
		season := types.Season{
			Number:     1,
			Name:       "Test Season",
			StartBlock: 0,
			EndBlock:   100000,
			Status:     types.SeasonStatus_SEASON_STATUS_NOMINATION,
		}
		err = f.keeper.Season.Set(f.ctx, season)
		require.NoError(t, err)

		// Create a nomination
		nomination := types.Nomination{
			Id:           1,
			Nominator:    nominatorStr,
			ContentRef:   "blog/post/1",
			Season:       1,
			TotalStaked:  math.LegacyZeroDec(),
			Conviction:   math.LegacyZeroDec(),
			RewardAmount: math.LegacyZeroDec(),
		}
		err = f.keeper.Nomination.Set(f.ctx, 1, nomination)
		require.NoError(t, err)

		// Do NOT register staker as a member in the mock
		// (f.repKeeper.Members[stakerStr] is false by default)

		_, err = ms.StakeNomination(f.ctx, &types.MsgStakeNomination{
			Creator:      stakerStr,
			NominationId: 1,
			Amount:       "100",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotMember)
	})

	t.Run("insufficient trust level", func(t *testing.T) {
		f := initBeginBlockFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		nominatorStr, err := f.addressCodec.BytesToString(TestAddrCreator)
		require.NoError(t, err)
		stakerStr, err := f.addressCodec.BytesToString(TestAddrMember1)
		require.NoError(t, err)

		// Set season in NOMINATION phase
		season := types.Season{
			Number:     1,
			Name:       "Test Season",
			StartBlock: 0,
			EndBlock:   100000,
			Status:     types.SeasonStatus_SEASON_STATUS_NOMINATION,
		}
		err = f.keeper.Season.Set(f.ctx, season)
		require.NoError(t, err)

		// Create a nomination
		nomination := types.Nomination{
			Id:           1,
			Nominator:    nominatorStr,
			ContentRef:   "blog/post/1",
			Season:       1,
			TotalStaked:  math.LegacyZeroDec(),
			Conviction:   math.LegacyZeroDec(),
			RewardAmount: math.LegacyZeroDec(),
		}
		err = f.keeper.Nomination.Set(f.ctx, 1, nomination)
		require.NoError(t, err)

		// Register staker as member (mock returns trust level 2)
		f.repKeeper.SetMember(stakerStr, 1000, nil, 0)

		// Set NominationStakeMinTrustLevel to 3, which exceeds the mock's trust level of 2
		params, err := f.keeper.Params.Get(f.ctx)
		require.NoError(t, err)
		params.NominationStakeMinTrustLevel = 3
		err = f.keeper.Params.Set(f.ctx, params)
		require.NoError(t, err)

		_, err = ms.StakeNomination(f.ctx, &types.MsgStakeNomination{
			Creator:      stakerStr,
			NominationId: 1,
			Amount:       "100",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrInsufficientTrustLevel)
		require.Contains(t, err.Error(), "trust level 2 < required 3")
	})

	t.Run("invalid amount string", func(t *testing.T) {
		f := initBeginBlockFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		nominatorStr, err := f.addressCodec.BytesToString(TestAddrCreator)
		require.NoError(t, err)
		stakerStr, err := f.addressCodec.BytesToString(TestAddrMember1)
		require.NoError(t, err)

		// Set season in NOMINATION phase
		season := types.Season{
			Number:     1,
			Name:       "Test Season",
			StartBlock: 0,
			EndBlock:   100000,
			Status:     types.SeasonStatus_SEASON_STATUS_NOMINATION,
		}
		err = f.keeper.Season.Set(f.ctx, season)
		require.NoError(t, err)

		// Create a nomination
		nomination := types.Nomination{
			Id:           1,
			Nominator:    nominatorStr,
			ContentRef:   "blog/post/1",
			Season:       1,
			TotalStaked:  math.LegacyZeroDec(),
			Conviction:   math.LegacyZeroDec(),
			RewardAmount: math.LegacyZeroDec(),
		}
		err = f.keeper.Nomination.Set(f.ctx, 1, nomination)
		require.NoError(t, err)

		// Register staker as member
		f.repKeeper.SetMember(stakerStr, 1000, nil, 0)

		_, err = ms.StakeNomination(f.ctx, &types.MsgStakeNomination{
			Creator:      stakerStr,
			NominationId: 1,
			Amount:       "not-a-number",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid stake amount")
	})

	t.Run("amount below minimum", func(t *testing.T) {
		f := initBeginBlockFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		nominatorStr, err := f.addressCodec.BytesToString(TestAddrCreator)
		require.NoError(t, err)
		stakerStr, err := f.addressCodec.BytesToString(TestAddrMember1)
		require.NoError(t, err)

		// Set season in NOMINATION phase
		season := types.Season{
			Number:     1,
			Name:       "Test Season",
			StartBlock: 0,
			EndBlock:   100000,
			Status:     types.SeasonStatus_SEASON_STATUS_NOMINATION,
		}
		err = f.keeper.Season.Set(f.ctx, season)
		require.NoError(t, err)

		// Create a nomination
		nomination := types.Nomination{
			Id:           1,
			Nominator:    nominatorStr,
			ContentRef:   "blog/post/1",
			Season:       1,
			TotalStaked:  math.LegacyZeroDec(),
			Conviction:   math.LegacyZeroDec(),
			RewardAmount: math.LegacyZeroDec(),
		}
		err = f.keeper.Nomination.Set(f.ctx, 1, nomination)
		require.NoError(t, err)

		// Register staker as member
		f.repKeeper.SetMember(stakerStr, 1000, nil, 0)

		// Default NominationMinStake is "10", so "5" is below
		_, err = ms.StakeNomination(f.ctx, &types.MsgStakeNomination{
			Creator:      stakerStr,
			NominationId: 1,
			Amount:       "5",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrStakeAmountTooLow)
		require.Contains(t, err.Error(), "amount 5")
	})

	t.Run("already staked on nomination", func(t *testing.T) {
		f := initBeginBlockFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		nominatorStr, err := f.addressCodec.BytesToString(TestAddrCreator)
		require.NoError(t, err)
		stakerStr, err := f.addressCodec.BytesToString(TestAddrMember1)
		require.NoError(t, err)

		// Set season in NOMINATION phase
		season := types.Season{
			Number:     1,
			Name:       "Test Season",
			StartBlock: 0,
			EndBlock:   100000,
			Status:     types.SeasonStatus_SEASON_STATUS_NOMINATION,
		}
		err = f.keeper.Season.Set(f.ctx, season)
		require.NoError(t, err)

		// Create a nomination
		nomination := types.Nomination{
			Id:           1,
			Nominator:    nominatorStr,
			ContentRef:   "blog/post/1",
			Season:       1,
			TotalStaked:  math.LegacyZeroDec(),
			Conviction:   math.LegacyZeroDec(),
			RewardAmount: math.LegacyZeroDec(),
		}
		err = f.keeper.Nomination.Set(f.ctx, 1, nomination)
		require.NoError(t, err)

		// Register staker as member
		f.repKeeper.SetMember(stakerStr, 1000, nil, 0)

		// Pre-create a NominationStake record for the staker on nomination 1
		stakeKey := fmt.Sprintf("%d/%s", 1, stakerStr)
		existingStake := types.NominationStake{
			NominationId:  1,
			Staker:        stakerStr,
			Amount:        math.LegacyNewDec(50),
			StakedAtBlock: 10,
		}
		err = f.keeper.NominationStake.Set(f.ctx, stakeKey, existingStake)
		require.NoError(t, err)

		_, err = ms.StakeNomination(f.ctx, &types.MsgStakeNomination{
			Creator:      stakerStr,
			NominationId: 1,
			Amount:       "100",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNominationStakeExists)
	})

	t.Run("cannot self-stake", func(t *testing.T) {
		f := initBeginBlockFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		// The nominator is also the staker
		nominatorStr, err := f.addressCodec.BytesToString(TestAddrCreator)
		require.NoError(t, err)

		// Set season in NOMINATION phase
		season := types.Season{
			Number:     1,
			Name:       "Test Season",
			StartBlock: 0,
			EndBlock:   100000,
			Status:     types.SeasonStatus_SEASON_STATUS_NOMINATION,
		}
		err = f.keeper.Season.Set(f.ctx, season)
		require.NoError(t, err)

		// Create a nomination where the nominator is TestAddrCreator
		nomination := types.Nomination{
			Id:           1,
			Nominator:    nominatorStr,
			ContentRef:   "blog/post/1",
			Season:       1,
			TotalStaked:  math.LegacyZeroDec(),
			Conviction:   math.LegacyZeroDec(),
			RewardAmount: math.LegacyZeroDec(),
		}
		err = f.keeper.Nomination.Set(f.ctx, 1, nomination)
		require.NoError(t, err)

		// Register nominator as member so they pass the membership check
		f.repKeeper.SetMember(nominatorStr, 1000, nil, 0)

		// Try to stake on own nomination
		_, err = ms.StakeNomination(f.ctx, &types.MsgStakeNomination{
			Creator:      nominatorStr,
			NominationId: 1,
			Amount:       "100",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNominationStakeExists)
		require.Contains(t, err.Error(), "cannot stake on own nomination")
	})

	t.Run("successful stake", func(t *testing.T) {
		f := initBeginBlockFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		nominatorStr, err := f.addressCodec.BytesToString(TestAddrCreator)
		require.NoError(t, err)
		stakerStr, err := f.addressCodec.BytesToString(TestAddrMember1)
		require.NoError(t, err)

		// Set block height for the context
		f.ctx = f.ctx.WithBlockHeight(50)

		// Set season in NOMINATION phase
		season := types.Season{
			Number:     1,
			Name:       "Test Season",
			StartBlock: 0,
			EndBlock:   100000,
			Status:     types.SeasonStatus_SEASON_STATUS_NOMINATION,
		}
		err = f.keeper.Season.Set(f.ctx, season)
		require.NoError(t, err)

		// Create a nomination with zero stakes
		nomination := types.Nomination{
			Id:           1,
			Nominator:    nominatorStr,
			ContentRef:   "blog/post/1",
			Season:       1,
			TotalStaked:  math.LegacyZeroDec(),
			Conviction:   math.LegacyZeroDec(),
			RewardAmount: math.LegacyZeroDec(),
		}
		err = f.keeper.Nomination.Set(f.ctx, 1, nomination)
		require.NoError(t, err)

		// Register staker as member
		f.repKeeper.SetMember(stakerStr, 5000, nil, 0)

		resp, err := ms.StakeNomination(f.ctx, &types.MsgStakeNomination{
			Creator:      stakerStr,
			NominationId: 1,
			Amount:       "100",
		})

		require.NoError(t, err)
		require.NotNil(t, resp)

		// Verify the NominationStake record was created
		stakeKey := fmt.Sprintf("%d/%s", 1, stakerStr)
		stake, err := f.keeper.NominationStake.Get(f.ctx, stakeKey)
		require.NoError(t, err)
		require.Equal(t, uint64(1), stake.NominationId)
		require.Equal(t, stakerStr, stake.Staker)
		require.True(t, stake.Amount.Equal(math.LegacyNewDec(100)), "stake amount should be 100, got %s", stake.Amount.String())
		require.Equal(t, int64(50), stake.StakedAtBlock)

		// Verify the nomination's TotalStaked was updated
		updatedNomination, err := f.keeper.Nomination.Get(f.ctx, 1)
		require.NoError(t, err)
		require.True(t, updatedNomination.TotalStaked.Equal(math.LegacyNewDec(100)),
			"nomination TotalStaked should be 100, got %s", updatedNomination.TotalStaked.String())

		// Verify conviction was recalculated (should be >= 0, since the stake was just created)
		require.False(t, updatedNomination.Conviction.IsNegative(),
			"conviction should not be negative")

		// Verify the "nomination_staked" event was emitted
		sdkCtx := sdk.UnwrapSDKContext(f.ctx)
		events := sdkCtx.EventManager().Events()
		found := false
		for _, event := range events {
			if event.Type == "nomination_staked" {
				found = true
				attrs := make(map[string]string)
				for _, attr := range event.Attributes {
					attrs[attr.Key] = attr.Value
				}
				require.Equal(t, "1", attrs["nomination_id"])
				require.Equal(t, stakerStr, attrs["staker"])
				require.Equal(t, "100.000000000000000000", attrs["amount"])
				require.Equal(t, "100.000000000000000000", attrs["total_staked"])
				break
			}
		}
		require.True(t, found, "nomination_staked event should be emitted")
	})

	t.Run("successful stake updates existing total", func(t *testing.T) {
		f := initBeginBlockFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		nominatorStr, err := f.addressCodec.BytesToString(TestAddrCreator)
		require.NoError(t, err)
		staker1Str, err := f.addressCodec.BytesToString(TestAddrMember1)
		require.NoError(t, err)
		staker2Str, err := f.addressCodec.BytesToString(TestAddrMember2)
		require.NoError(t, err)

		f.ctx = f.ctx.WithBlockHeight(10)

		// Set season in NOMINATION phase
		season := types.Season{
			Number:     1,
			Name:       "Test Season",
			StartBlock: 0,
			EndBlock:   100000,
			Status:     types.SeasonStatus_SEASON_STATUS_NOMINATION,
		}
		err = f.keeper.Season.Set(f.ctx, season)
		require.NoError(t, err)

		// Create a nomination that already has some staked amount
		nomination := types.Nomination{
			Id:           1,
			Nominator:    nominatorStr,
			ContentRef:   "blog/post/1",
			Season:       1,
			TotalStaked:  math.LegacyNewDec(200),
			Conviction:   math.LegacyNewDec(50),
			RewardAmount: math.LegacyZeroDec(),
		}
		err = f.keeper.Nomination.Set(f.ctx, 1, nomination)
		require.NoError(t, err)

		// Pre-create first staker's stake (to simulate a previous stake)
		stakeKey1 := fmt.Sprintf("%d/%s", 1, staker1Str)
		existingStake := types.NominationStake{
			NominationId:  1,
			Staker:        staker1Str,
			Amount:        math.LegacyNewDec(200),
			StakedAtBlock: 5,
		}
		err = f.keeper.NominationStake.Set(f.ctx, stakeKey1, existingStake)
		require.NoError(t, err)

		// Register second staker as member
		f.repKeeper.SetMember(staker2Str, 5000, nil, 0)

		// Second staker stakes 150 DREAM
		resp, err := ms.StakeNomination(f.ctx, &types.MsgStakeNomination{
			Creator:      staker2Str,
			NominationId: 1,
			Amount:       "150",
		})

		require.NoError(t, err)
		require.NotNil(t, resp)

		// Verify nomination TotalStaked = 200 + 150 = 350
		updatedNomination, err := f.keeper.Nomination.Get(f.ctx, 1)
		require.NoError(t, err)
		require.True(t, updatedNomination.TotalStaked.Equal(math.LegacyNewDec(350)),
			"nomination TotalStaked should be 350, got %s", updatedNomination.TotalStaked.String())

		// Verify second staker's stake record exists
		stakeKey2 := fmt.Sprintf("%d/%s", 1, staker2Str)
		stake2, err := f.keeper.NominationStake.Get(f.ctx, stakeKey2)
		require.NoError(t, err)
		require.True(t, stake2.Amount.Equal(math.LegacyNewDec(150)))
		require.Equal(t, int64(10), stake2.StakedAtBlock)
	})

	t.Run("maintenance mode blocks staking", func(t *testing.T) {
		f := initBeginBlockFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		stakerStr, err := f.addressCodec.BytesToString(TestAddrMember1)
		require.NoError(t, err)

		// IsInMaintenanceMode checks SeasonTransitionState.MaintenanceMode
		transitionState := types.SeasonTransitionState{
			Phase:           types.TransitionPhase_TRANSITION_PHASE_SNAPSHOT,
			MaintenanceMode: true,
		}
		err = f.keeper.SeasonTransitionState.Set(f.ctx, transitionState)
		require.NoError(t, err)

		_, err = ms.StakeNomination(f.ctx, &types.MsgStakeNomination{
			Creator:      stakerStr,
			NominationId: 1,
			Amount:       "100",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrMaintenanceMode)
	})
}
