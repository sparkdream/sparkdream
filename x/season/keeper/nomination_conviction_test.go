package keeper_test

import (
	"fmt"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/types"
)

func TestCalculateNominationConviction(t *testing.T) {
	// Default params: EpochBlocks=17280, NominationConvictionHalfLifeEpochs=3
	// halfLifeBlocks = 3 * 17280 = 51840
	// twoHalfLife = 2 * 51840 = 103680
	const halfLifeBlocks = 3 * 17280       // 51840
	const twoHalfLife = 2 * halfLifeBlocks // 103680

	t.Run("no stakes", func(t *testing.T) {
		f := initFixture(t)
		sdkCtx := sdk.UnwrapSDKContext(f.ctx)
		sdkCtx = sdkCtx.WithBlockHeight(1000)

		nomination := types.Nomination{
			Id:             1,
			Nominator:      "nominator1",
			ContentRef:     "blog/1",
			CreatedAtBlock: 0,
			Season:         1,
			TotalStaked:    math.LegacyZeroDec(),
			Conviction:     math.LegacyZeroDec(),
			RewardAmount:   math.LegacyZeroDec(),
		}

		conviction, err := f.keeper.CalculateNominationConviction(sdkCtx, nomination)
		require.NoError(t, err)
		require.True(t, conviction.IsZero(), "conviction should be zero with no stakes, got %s", conviction)
	})

	t.Run("single stake, zero elapsed", func(t *testing.T) {
		f := initFixture(t)
		sdkCtx := sdk.UnwrapSDKContext(f.ctx)
		sdkCtx = sdkCtx.WithBlockHeight(500)

		nominationId := uint64(1)
		nomination := types.Nomination{
			Id:             nominationId,
			Nominator:      "nominator1",
			ContentRef:     "blog/1",
			CreatedAtBlock: 500,
			Season:         1,
			TotalStaked:    math.LegacyNewDec(100),
			Conviction:     math.LegacyZeroDec(),
			RewardAmount:   math.LegacyZeroDec(),
		}

		// Stake at the same block as current height -> elapsed = 0 -> timeFactor = 0
		stakeKey := fmt.Sprintf("%d/%s", nominationId, "staker1")
		stake := types.NominationStake{
			NominationId:  nominationId,
			Staker:        "staker1",
			Amount:        math.LegacyNewDec(100),
			StakedAtBlock: 500,
		}
		err := f.keeper.NominationStake.Set(sdkCtx, stakeKey, stake)
		require.NoError(t, err)

		conviction, err := f.keeper.CalculateNominationConviction(sdkCtx, nomination)
		require.NoError(t, err)
		require.True(t, conviction.IsZero(), "conviction should be zero when elapsed is 0, got %s", conviction)
	})

	t.Run("single stake, partial elapsed", func(t *testing.T) {
		f := initFixture(t)
		sdkCtx := sdk.UnwrapSDKContext(f.ctx)
		// Stake at block 0, current block = halfLifeBlocks (51840)
		// elapsed = 51840, timeFactor = 51840 / 103680 = 0.5
		// conviction = 100 * 0.5 = 50
		sdkCtx = sdkCtx.WithBlockHeight(int64(halfLifeBlocks))

		nominationId := uint64(1)
		nomination := types.Nomination{
			Id:             nominationId,
			Nominator:      "nominator1",
			ContentRef:     "blog/1",
			CreatedAtBlock: 0,
			Season:         1,
			TotalStaked:    math.LegacyNewDec(100),
			Conviction:     math.LegacyZeroDec(),
			RewardAmount:   math.LegacyZeroDec(),
		}

		stakeKey := fmt.Sprintf("%d/%s", nominationId, "staker1")
		stake := types.NominationStake{
			NominationId:  nominationId,
			Staker:        "staker1",
			Amount:        math.LegacyNewDec(100),
			StakedAtBlock: 0,
		}
		err := f.keeper.NominationStake.Set(sdkCtx, stakeKey, stake)
		require.NoError(t, err)

		conviction, err := f.keeper.CalculateNominationConviction(sdkCtx, nomination)
		require.NoError(t, err)

		expected := math.LegacyNewDec(50) // 100 * 0.5
		require.True(t, conviction.Equal(expected),
			"conviction should be 50, got %s", conviction)
	})

	t.Run("single stake, fully matured", func(t *testing.T) {
		f := initFixture(t)
		sdkCtx := sdk.UnwrapSDKContext(f.ctx)
		// Stake at block 0, current block = twoHalfLife (103680)
		// elapsed = 103680, timeFactor = min(1.0, 103680/103680) = 1.0
		// conviction = 100 * 1.0 = 100
		sdkCtx = sdkCtx.WithBlockHeight(int64(twoHalfLife))

		nominationId := uint64(1)
		nomination := types.Nomination{
			Id:             nominationId,
			Nominator:      "nominator1",
			ContentRef:     "blog/1",
			CreatedAtBlock: 0,
			Season:         1,
			TotalStaked:    math.LegacyNewDec(100),
			Conviction:     math.LegacyZeroDec(),
			RewardAmount:   math.LegacyZeroDec(),
		}

		stakeKey := fmt.Sprintf("%d/%s", nominationId, "staker1")
		stake := types.NominationStake{
			NominationId:  nominationId,
			Staker:        "staker1",
			Amount:        math.LegacyNewDec(100),
			StakedAtBlock: 0,
		}
		err := f.keeper.NominationStake.Set(sdkCtx, stakeKey, stake)
		require.NoError(t, err)

		conviction, err := f.keeper.CalculateNominationConviction(sdkCtx, nomination)
		require.NoError(t, err)

		expected := math.LegacyNewDec(100) // 100 * 1.0
		require.True(t, conviction.Equal(expected),
			"conviction should be 100, got %s", conviction)
	})

	t.Run("single stake, beyond fully matured", func(t *testing.T) {
		f := initFixture(t)
		sdkCtx := sdk.UnwrapSDKContext(f.ctx)
		// Stake at block 0, current block = twoHalfLife * 2 (207360)
		// elapsed = 207360, timeFactor = min(1.0, 207360/103680) = 1.0 (capped)
		// conviction = 200 * 1.0 = 200
		sdkCtx = sdkCtx.WithBlockHeight(int64(twoHalfLife * 2))

		nominationId := uint64(1)
		nomination := types.Nomination{
			Id:             nominationId,
			Nominator:      "nominator1",
			ContentRef:     "blog/1",
			CreatedAtBlock: 0,
			Season:         1,
			TotalStaked:    math.LegacyNewDec(200),
			Conviction:     math.LegacyZeroDec(),
			RewardAmount:   math.LegacyZeroDec(),
		}

		stakeKey := fmt.Sprintf("%d/%s", nominationId, "staker1")
		stake := types.NominationStake{
			NominationId:  nominationId,
			Staker:        "staker1",
			Amount:        math.LegacyNewDec(200),
			StakedAtBlock: 0,
		}
		err := f.keeper.NominationStake.Set(sdkCtx, stakeKey, stake)
		require.NoError(t, err)

		conviction, err := f.keeper.CalculateNominationConviction(sdkCtx, nomination)
		require.NoError(t, err)

		expected := math.LegacyNewDec(200) // 200 * 1.0 (capped at 1.0)
		require.True(t, conviction.Equal(expected),
			"conviction should be 200 (timeFactor capped at 1.0), got %s", conviction)
	})

	t.Run("multiple stakes, different ages", func(t *testing.T) {
		f := initFixture(t)
		sdkCtx := sdk.UnwrapSDKContext(f.ctx)
		// Current block = 103680 (twoHalfLife)
		sdkCtx = sdkCtx.WithBlockHeight(int64(twoHalfLife))

		nominationId := uint64(1)
		nomination := types.Nomination{
			Id:             nominationId,
			Nominator:      "nominator1",
			ContentRef:     "blog/1",
			CreatedAtBlock: 0,
			Season:         1,
			TotalStaked:    math.LegacyNewDec(300),
			Conviction:     math.LegacyZeroDec(),
			RewardAmount:   math.LegacyZeroDec(),
		}

		// Stake 1: staked at block 0, elapsed = 103680
		// timeFactor = min(1.0, 103680/103680) = 1.0
		// conviction1 = 100 * 1.0 = 100
		stakeKey1 := fmt.Sprintf("%d/%s", nominationId, "staker1")
		stake1 := types.NominationStake{
			NominationId:  nominationId,
			Staker:        "staker1",
			Amount:        math.LegacyNewDec(100),
			StakedAtBlock: 0,
		}
		err := f.keeper.NominationStake.Set(sdkCtx, stakeKey1, stake1)
		require.NoError(t, err)

		// Stake 2: staked at block 51840 (halfLifeBlocks), elapsed = 103680 - 51840 = 51840
		// timeFactor = min(1.0, 51840/103680) = 0.5
		// conviction2 = 200 * 0.5 = 100
		stakeKey2 := fmt.Sprintf("%d/%s", nominationId, "staker2")
		stake2 := types.NominationStake{
			NominationId:  nominationId,
			Staker:        "staker2",
			Amount:        math.LegacyNewDec(200),
			StakedAtBlock: int64(halfLifeBlocks),
		}
		err = f.keeper.NominationStake.Set(sdkCtx, stakeKey2, stake2)
		require.NoError(t, err)

		conviction, err := f.keeper.CalculateNominationConviction(sdkCtx, nomination)
		require.NoError(t, err)

		// Total conviction = 100 + 100 = 200
		expected := math.LegacyNewDec(200)
		require.True(t, conviction.Equal(expected),
			"conviction should be 200 (100 + 100), got %s", conviction)
	})

	t.Run("stakes for different nominations", func(t *testing.T) {
		f := initFixture(t)
		sdkCtx := sdk.UnwrapSDKContext(f.ctx)
		sdkCtx = sdkCtx.WithBlockHeight(int64(twoHalfLife))

		// We calculate conviction for nomination 1, but there are also stakes for nomination 2
		nomination1 := types.Nomination{
			Id:             1,
			Nominator:      "nominator1",
			ContentRef:     "blog/1",
			CreatedAtBlock: 0,
			Season:         1,
			TotalStaked:    math.LegacyNewDec(100),
			Conviction:     math.LegacyZeroDec(),
			RewardAmount:   math.LegacyZeroDec(),
		}

		// Stake for nomination 1 - should be counted
		stakeKey1 := fmt.Sprintf("%d/%s", uint64(1), "staker1")
		stake1 := types.NominationStake{
			NominationId:  1,
			Staker:        "staker1",
			Amount:        math.LegacyNewDec(100),
			StakedAtBlock: 0,
		}
		err := f.keeper.NominationStake.Set(sdkCtx, stakeKey1, stake1)
		require.NoError(t, err)

		// Stake for nomination 2 - should NOT be counted
		stakeKey2 := fmt.Sprintf("%d/%s", uint64(2), "staker1")
		stake2 := types.NominationStake{
			NominationId:  2,
			Staker:        "staker1",
			Amount:        math.LegacyNewDec(500),
			StakedAtBlock: 0,
		}
		err = f.keeper.NominationStake.Set(sdkCtx, stakeKey2, stake2)
		require.NoError(t, err)

		conviction, err := f.keeper.CalculateNominationConviction(sdkCtx, nomination1)
		require.NoError(t, err)

		// Only nomination 1's stake should be counted:
		// elapsed = 103680, timeFactor = 1.0, conviction = 100 * 1.0 = 100
		expected := math.LegacyNewDec(100)
		require.True(t, conviction.Equal(expected),
			"conviction should be 100 (only nomination 1 stakes counted), got %s", conviction)
	})
}
