package keeper_test

import (
	"fmt"
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestUnstakeNomination(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initBeginBlockFixture(t)
		msgServer := keeper.NewMsgServerImpl(f.keeper)

		msg := &types.MsgUnstakeNomination{
			Creator:      "invalid-address",
			NominationId: 1,
		}

		_, err := msgServer.UnstakeNomination(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("no active season", func(t *testing.T) {
		f := initBeginBlockFixture(t)
		msgServer := keeper.NewMsgServerImpl(f.keeper)

		creatorAddr, err := f.addressCodec.BytesToString(TestAddrCreator)
		require.NoError(t, err)

		msg := &types.MsgUnstakeNomination{
			Creator:      creatorAddr,
			NominationId: 1,
		}

		_, err = msgServer.UnstakeNomination(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrSeasonNotActive)
	})

	t.Run("season not in nomination phase", func(t *testing.T) {
		f := initBeginBlockFixture(t)
		msgServer := keeper.NewMsgServerImpl(f.keeper)

		// Set up season in ACTIVE status (not NOMINATION)
		season := types.Season{
			Number:     1,
			Name:       "Test Season",
			StartBlock: 0,
			EndBlock:   100000,
			Status:     types.SeasonStatus_SEASON_STATUS_ACTIVE,
		}
		err := f.keeper.Season.Set(f.ctx, season)
		require.NoError(t, err)

		creatorAddr, err := f.addressCodec.BytesToString(TestAddrCreator)
		require.NoError(t, err)

		msg := &types.MsgUnstakeNomination{
			Creator:      creatorAddr,
			NominationId: 1,
		}

		_, err = msgServer.UnstakeNomination(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrSeasonNotInNominationPhase)
	})

	t.Run("maintenance mode blocks unstake", func(t *testing.T) {
		f := initBeginBlockFixture(t)
		msgServer := keeper.NewMsgServerImpl(f.keeper)

		// Put system in maintenance mode
		transitionState := types.SeasonTransitionState{
			Phase:           types.TransitionPhase_TRANSITION_PHASE_SNAPSHOT,
			MaintenanceMode: true,
		}
		err := f.keeper.SeasonTransitionState.Set(f.ctx, transitionState)
		require.NoError(t, err)

		creatorAddr, err := f.addressCodec.BytesToString(TestAddrCreator)
		require.NoError(t, err)

		msg := &types.MsgUnstakeNomination{
			Creator:      creatorAddr,
			NominationId: 1,
		}

		_, err = msgServer.UnstakeNomination(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrMaintenanceMode)
	})

	t.Run("nomination not found", func(t *testing.T) {
		f := initBeginBlockFixture(t)
		msgServer := keeper.NewMsgServerImpl(f.keeper)

		// Set up season in NOMINATION status
		season := types.Season{
			Number:     1,
			Name:       "Test Season",
			StartBlock: 0,
			EndBlock:   100000,
			Status:     types.SeasonStatus_SEASON_STATUS_NOMINATION,
		}
		err := f.keeper.Season.Set(f.ctx, season)
		require.NoError(t, err)

		creatorAddr, err := f.addressCodec.BytesToString(TestAddrCreator)
		require.NoError(t, err)

		msg := &types.MsgUnstakeNomination{
			Creator:      creatorAddr,
			NominationId: 999, // Non-existent nomination
		}

		_, err = msgServer.UnstakeNomination(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNominationNotFound)
	})

	t.Run("stake not found for staker", func(t *testing.T) {
		f := initBeginBlockFixture(t)
		msgServer := keeper.NewMsgServerImpl(f.keeper)

		creatorAddr, err := f.addressCodec.BytesToString(TestAddrCreator)
		require.NoError(t, err)

		// Set up season in NOMINATION status
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
		nominationId := uint64(1)
		nomination := types.Nomination{
			Id:           nominationId,
			Nominator:    "cosmos1nominator",
			ContentRef:   "blog/1",
			Rationale:    "Great content",
			Season:       1,
			TotalStaked:  math.LegacyNewDec(100),
			Conviction:   math.LegacyNewDec(50),
			RewardAmount: math.LegacyZeroDec(),
		}
		err = f.keeper.Nomination.Set(f.ctx, nominationId, nomination)
		require.NoError(t, err)

		// Do NOT create a NominationStake for the creator

		msg := &types.MsgUnstakeNomination{
			Creator:      creatorAddr,
			NominationId: nominationId,
		}

		_, err = msgServer.UnstakeNomination(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNominationStakeNotFound)
	})

	t.Run("successful unstake", func(t *testing.T) {
		f := initBeginBlockFixture(t)
		msgServer := keeper.NewMsgServerImpl(f.keeper)

		stakerAddr, err := f.addressCodec.BytesToString(TestAddrMember1)
		require.NoError(t, err)

		// Set up season in NOMINATION status
		season := types.Season{
			Number:     1,
			Name:       "Test Season",
			StartBlock: 0,
			EndBlock:   100000,
			Status:     types.SeasonStatus_SEASON_STATUS_NOMINATION,
		}
		err = f.keeper.Season.Set(f.ctx, season)
		require.NoError(t, err)

		// Create a nomination with TotalStaked = 100
		nominationId := uint64(1)
		nomination := types.Nomination{
			Id:           nominationId,
			Nominator:    "cosmos1nominator",
			ContentRef:   "blog/1",
			Rationale:    "Great content",
			Season:       1,
			TotalStaked:  math.LegacyNewDec(100),
			Conviction:   math.LegacyNewDec(50),
			RewardAmount: math.LegacyZeroDec(),
		}
		err = f.keeper.Nomination.Set(f.ctx, nominationId, nomination)
		require.NoError(t, err)

		// Create a NominationStake record for the staker
		stakeKey := fmt.Sprintf("%d/%s", nominationId, stakerAddr)
		stake := types.NominationStake{
			NominationId:  nominationId,
			Staker:        stakerAddr,
			Amount:        math.LegacyNewDec(100),
			StakedAtBlock: 50,
		}
		err = f.keeper.NominationStake.Set(f.ctx, stakeKey, stake)
		require.NoError(t, err)

		// Set block height for conviction calculation
		f.ctx = f.ctx.WithBlockHeight(100)

		msg := &types.MsgUnstakeNomination{
			Creator:      stakerAddr,
			NominationId: nominationId,
		}

		resp, err := msgServer.UnstakeNomination(f.ctx, msg)
		require.NoError(t, err)
		require.NotNil(t, resp)

		// Verify stake record was removed
		_, err = f.keeper.NominationStake.Get(f.ctx, stakeKey)
		require.Error(t, err, "stake record should be removed after unstake")

		// Verify nomination TotalStaked decreased to 0
		updatedNomination, err := f.keeper.Nomination.Get(f.ctx, nominationId)
		require.NoError(t, err)
		require.True(t, updatedNomination.TotalStaked.IsZero(),
			"TotalStaked should be zero after unstaking all, got %s", updatedNomination.TotalStaked.String())

		// Verify conviction was recalculated (should be zero with no stakes)
		require.True(t, updatedNomination.Conviction.IsZero(),
			"Conviction should be zero after removing all stakes, got %s", updatedNomination.Conviction.String())
	})

	t.Run("successful partial unstake from multiple stakers", func(t *testing.T) {
		f := initBeginBlockFixture(t)
		msgServer := keeper.NewMsgServerImpl(f.keeper)

		staker1Addr, err := f.addressCodec.BytesToString(TestAddrMember1)
		require.NoError(t, err)
		staker2Addr, err := f.addressCodec.BytesToString(TestAddrMember2)
		require.NoError(t, err)

		// Set up season in NOMINATION status
		season := types.Season{
			Number:     1,
			Name:       "Test Season",
			StartBlock: 0,
			EndBlock:   100000,
			Status:     types.SeasonStatus_SEASON_STATUS_NOMINATION,
		}
		err = f.keeper.Season.Set(f.ctx, season)
		require.NoError(t, err)

		// Create a nomination with TotalStaked = 250 (100 + 150 from two stakers)
		nominationId := uint64(1)
		nomination := types.Nomination{
			Id:           nominationId,
			Nominator:    "cosmos1nominator",
			ContentRef:   "blog/1",
			Rationale:    "Great content",
			Season:       1,
			TotalStaked:  math.LegacyNewDec(250),
			Conviction:   math.LegacyNewDec(100),
			RewardAmount: math.LegacyZeroDec(),
		}
		err = f.keeper.Nomination.Set(f.ctx, nominationId, nomination)
		require.NoError(t, err)

		// Create stake records for both stakers
		stakeKey1 := fmt.Sprintf("%d/%s", nominationId, staker1Addr)
		stake1 := types.NominationStake{
			NominationId:  nominationId,
			Staker:        staker1Addr,
			Amount:        math.LegacyNewDec(100),
			StakedAtBlock: 50,
		}
		err = f.keeper.NominationStake.Set(f.ctx, stakeKey1, stake1)
		require.NoError(t, err)

		stakeKey2 := fmt.Sprintf("%d/%s", nominationId, staker2Addr)
		stake2 := types.NominationStake{
			NominationId:  nominationId,
			Staker:        staker2Addr,
			Amount:        math.LegacyNewDec(150),
			StakedAtBlock: 60,
		}
		err = f.keeper.NominationStake.Set(f.ctx, stakeKey2, stake2)
		require.NoError(t, err)

		// Set block height for conviction calculation
		f.ctx = f.ctx.WithBlockHeight(100)

		// Unstake only staker1
		msg := &types.MsgUnstakeNomination{
			Creator:      staker1Addr,
			NominationId: nominationId,
		}

		resp, err := msgServer.UnstakeNomination(f.ctx, msg)
		require.NoError(t, err)
		require.NotNil(t, resp)

		// Verify staker1's stake record was removed
		_, err = f.keeper.NominationStake.Get(f.ctx, stakeKey1)
		require.Error(t, err, "staker1's stake should be removed")

		// Verify staker2's stake record still exists
		_, err = f.keeper.NominationStake.Get(f.ctx, stakeKey2)
		require.NoError(t, err, "staker2's stake should still exist")

		// Verify nomination TotalStaked decreased by staker1's amount
		updatedNomination, err := f.keeper.Nomination.Get(f.ctx, nominationId)
		require.NoError(t, err)
		expectedTotalStaked := math.LegacyNewDec(150) // 250 - 100
		require.True(t, updatedNomination.TotalStaked.Equal(expectedTotalStaked),
			"TotalStaked should be 150 after removing staker1's 100, got %s", updatedNomination.TotalStaked.String())

		// Verify conviction is non-zero (staker2's stake still contributes)
		require.False(t, updatedNomination.Conviction.IsZero(),
			"Conviction should be non-zero with staker2's stake remaining")
	})

	t.Run("unstake with season in ending status rejects", func(t *testing.T) {
		f := initBeginBlockFixture(t)
		msgServer := keeper.NewMsgServerImpl(f.keeper)

		stakerAddr, err := f.addressCodec.BytesToString(TestAddrMember1)
		require.NoError(t, err)

		// Set up season in ENDING status (not NOMINATION)
		season := types.Season{
			Number:     1,
			Name:       "Test Season",
			StartBlock: 0,
			EndBlock:   100000,
			Status:     types.SeasonStatus_SEASON_STATUS_ENDING,
		}
		err = f.keeper.Season.Set(f.ctx, season)
		require.NoError(t, err)

		msg := &types.MsgUnstakeNomination{
			Creator:      stakerAddr,
			NominationId: 1,
		}

		_, err = msgServer.UnstakeNomination(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrSeasonNotInNominationPhase)
	})

	t.Run("unstake emits nomination_unstaked event", func(t *testing.T) {
		f := initBeginBlockFixture(t)
		msgServer := keeper.NewMsgServerImpl(f.keeper)

		stakerAddr, err := f.addressCodec.BytesToString(TestAddrMember1)
		require.NoError(t, err)

		// Set up season in NOMINATION status
		season := types.Season{
			Number:     1,
			Name:       "Test Season",
			StartBlock: 0,
			EndBlock:   100000,
			Status:     types.SeasonStatus_SEASON_STATUS_NOMINATION,
		}
		err = f.keeper.Season.Set(f.ctx, season)
		require.NoError(t, err)

		// Create nomination
		nominationId := uint64(1)
		nomination := types.Nomination{
			Id:           nominationId,
			Nominator:    "cosmos1nominator",
			ContentRef:   "blog/1",
			Rationale:    "Great content",
			Season:       1,
			TotalStaked:  math.LegacyNewDec(100),
			Conviction:   math.LegacyNewDec(50),
			RewardAmount: math.LegacyZeroDec(),
		}
		err = f.keeper.Nomination.Set(f.ctx, nominationId, nomination)
		require.NoError(t, err)

		// Create stake record
		stakeKey := fmt.Sprintf("%d/%s", nominationId, stakerAddr)
		stake := types.NominationStake{
			NominationId:  nominationId,
			Staker:        stakerAddr,
			Amount:        math.LegacyNewDec(100),
			StakedAtBlock: 50,
		}
		err = f.keeper.NominationStake.Set(f.ctx, stakeKey, stake)
		require.NoError(t, err)

		f.ctx = f.ctx.WithBlockHeight(100)

		msg := &types.MsgUnstakeNomination{
			Creator:      stakerAddr,
			NominationId: nominationId,
		}

		_, err = msgServer.UnstakeNomination(f.ctx, msg)
		require.NoError(t, err)

		// Verify event was emitted
		events := f.ctx.EventManager().Events()
		found := false
		for _, event := range events {
			if event.Type == "nomination_unstaked" {
				found = true
				// Check event attributes
				attrMap := make(map[string]string)
				for _, attr := range event.Attributes {
					attrMap[attr.Key] = attr.Value
				}
				require.Equal(t, fmt.Sprintf("%d", nominationId), attrMap["nomination_id"])
				require.Equal(t, stakerAddr, attrMap["staker"])
				require.Equal(t, stake.Amount.String(), attrMap["amount"])
				break
			}
		}
		require.True(t, found, "nomination_unstaked event should be emitted")
	})

	t.Run("unstake with zero truncated amount skips unlock", func(t *testing.T) {
		f := initBeginBlockFixture(t)
		msgServer := keeper.NewMsgServerImpl(f.keeper)

		stakerAddr, err := f.addressCodec.BytesToString(TestAddrMember1)
		require.NoError(t, err)

		// Set up season in NOMINATION status
		season := types.Season{
			Number:     1,
			Name:       "Test Season",
			StartBlock: 0,
			EndBlock:   100000,
			Status:     types.SeasonStatus_SEASON_STATUS_NOMINATION,
		}
		err = f.keeper.Season.Set(f.ctx, season)
		require.NoError(t, err)

		// Create nomination
		nominationId := uint64(1)
		nomination := types.Nomination{
			Id:           nominationId,
			Nominator:    "cosmos1nominator",
			ContentRef:   "blog/1",
			Rationale:    "Great content",
			Season:       1,
			TotalStaked:  math.LegacyMustNewDecFromStr("0.5"),
			Conviction:   math.LegacyZeroDec(),
			RewardAmount: math.LegacyZeroDec(),
		}
		err = f.keeper.Nomination.Set(f.ctx, nominationId, nomination)
		require.NoError(t, err)

		// Create stake record with a fractional amount that truncates to 0
		stakeKey := fmt.Sprintf("%d/%s", nominationId, stakerAddr)
		stake := types.NominationStake{
			NominationId:  nominationId,
			Staker:        stakerAddr,
			Amount:        math.LegacyMustNewDecFromStr("0.5"),
			StakedAtBlock: 50,
		}
		err = f.keeper.NominationStake.Set(f.ctx, stakeKey, stake)
		require.NoError(t, err)

		f.ctx = f.ctx.WithBlockHeight(100)

		msg := &types.MsgUnstakeNomination{
			Creator:      stakerAddr,
			NominationId: nominationId,
		}

		// Should succeed without error (zero truncated amount skips UnlockDREAM call)
		resp, err := msgServer.UnstakeNomination(f.ctx, msg)
		require.NoError(t, err)
		require.NotNil(t, resp)

		// Verify stake record was still removed
		_, err = f.keeper.NominationStake.Get(f.ctx, stakeKey)
		require.Error(t, err, "stake record should be removed even with fractional amount")

		// Verify nomination TotalStaked was updated
		updatedNomination, err := f.keeper.Nomination.Get(f.ctx, nominationId)
		require.NoError(t, err)
		require.True(t, updatedNomination.TotalStaked.IsZero(),
			"TotalStaked should be zero, got %s", updatedNomination.TotalStaked.String())
	})
}
