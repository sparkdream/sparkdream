package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

func TestTreasuryBalance_AddSpendEnforce(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	bal, err := k.GetTreasuryBalance(ctx)
	require.NoError(t, err)
	require.True(t, bal.IsZero(), "empty store should report zero balance")

	require.NoError(t, k.AddToTreasury(ctx, math.NewInt(1_000)))
	require.NoError(t, k.AddToTreasury(ctx, math.NewInt(500)))

	bal, err = k.GetTreasuryBalance(ctx)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(1_500), bal)

	// Partial spend.
	spent, err := k.SpendFromTreasury(ctx, math.NewInt(300))
	require.NoError(t, err)
	require.Equal(t, math.NewInt(300), spent)

	// Over-spend is capped at remaining balance.
	spent, err = k.SpendFromTreasury(ctx, math.NewInt(10_000))
	require.NoError(t, err)
	require.Equal(t, math.NewInt(1_200), spent)

	bal, err = k.GetTreasuryBalance(ctx)
	require.NoError(t, err)
	require.True(t, bal.IsZero())
}

func TestEnforceTreasuryBalance_BurnsExcess(t *testing.T) {
	params := types.DefaultParams()
	params.MaxTreasuryBalance = math.NewInt(1_000)
	f := initFixture(t, WithCustomParams(params))
	k := f.keeper
	ctx := f.ctx

	require.NoError(t, k.AddToTreasury(ctx, math.NewInt(1_750)))
	require.NoError(t, k.EnforceTreasuryBalance(ctx))

	bal, err := k.GetTreasuryBalance(ctx)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(1_000), bal, "balance should be capped at max")

	burned, err := k.GetSeasonBurned(ctx)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(750), burned, "excess should be tracked in SeasonBurned")
}

func TestEnforceTreasuryBalance_UnderCapIsNoop(t *testing.T) {
	params := types.DefaultParams()
	params.MaxTreasuryBalance = math.NewInt(1_000)
	f := initFixture(t, WithCustomParams(params))
	k := f.keeper
	ctx := f.ctx

	require.NoError(t, k.AddToTreasury(ctx, math.NewInt(500)))
	require.NoError(t, k.EnforceTreasuryBalance(ctx))

	bal, err := k.GetTreasuryBalance(ctx)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(500), bal)

	burned, err := k.GetSeasonBurned(ctx)
	require.NoError(t, err)
	require.True(t, burned.IsZero())
}

func TestSeasonCounters_TrackMintAndBurn(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	require.NoError(t, k.TrackMint(ctx, math.NewInt(100)))
	require.NoError(t, k.TrackMint(ctx, math.NewInt(250)))
	require.NoError(t, k.TrackBurn(ctx, math.NewInt(40)))
	require.NoError(t, k.TrackInitiativeRewardMint(ctx, math.NewInt(300)))

	minted, err := k.GetSeasonMinted(ctx)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(350), minted)

	burned, err := k.GetSeasonBurned(ctx)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(40), burned)

	initRewards, err := k.GetSeasonInitiativeRewardsMinted(ctx)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(300), initRewards)
}

func TestCheckAndTrackEpochMint_RejectsZeroCap(t *testing.T) {
	// REP-S2-14: MaxDreamMintPerEpoch=0 is rejected at Validate so it can no
	// longer be used as an "unbounded" sentinel.
	params := types.DefaultParams()
	params.MaxDreamMintPerEpoch = math.ZeroInt()
	require.Error(t, params.Validate())
}

func TestCheckAndTrackEpochMint_EnforcesCapWithinEpoch(t *testing.T) {
	params := types.DefaultParams()
	params.MaxDreamMintPerEpoch = math.NewInt(1_000)
	f := initFixture(t, WithCustomParams(params))
	k := f.keeper
	ctx := f.ctx

	// Three mints that sum to the cap should all succeed.
	require.NoError(t, k.CheckAndTrackEpochMint(ctx, math.NewInt(400)))
	require.NoError(t, k.CheckAndTrackEpochMint(ctx, math.NewInt(500)))
	require.NoError(t, k.CheckAndTrackEpochMint(ctx, math.NewInt(100)))

	// The next mint, even by 1, must fail.
	err := k.CheckAndTrackEpochMint(ctx, math.NewInt(1))
	require.ErrorIs(t, err, types.ErrDreamMintCapExceeded)
}

func TestCheckAndTrackEpochMint_SingleMintExceedingCapFails(t *testing.T) {
	params := types.DefaultParams()
	params.MaxDreamMintPerEpoch = math.NewInt(100)
	f := initFixture(t, WithCustomParams(params))
	k := f.keeper
	ctx := f.ctx

	err := k.CheckAndTrackEpochMint(ctx, math.NewInt(101))
	require.ErrorIs(t, err, types.ErrDreamMintCapExceeded)
}

func TestCheckAndTrackEpochMint_CounterResetsOnNewEpoch(t *testing.T) {
	params := types.DefaultParams()
	params.MaxDreamMintPerEpoch = math.NewInt(1_000)
	params.EpochBlocks = 10 // small epoch for easy block-height manipulation
	f := initFixture(t, WithCustomParams(params))
	k := f.keeper
	ctx := f.ctx

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Epoch 0: mint up to cap.
	ctx0 := sdkCtx.WithBlockHeight(1)
	require.NoError(t, k.CheckAndTrackEpochMint(ctx0, math.NewInt(1_000)))
	require.ErrorIs(t, k.CheckAndTrackEpochMint(ctx0, math.NewInt(1)), types.ErrDreamMintCapExceeded)

	// Epoch 1: budget resets.
	ctx1 := sdkCtx.WithBlockHeight(15)
	require.NoError(t, k.CheckAndTrackEpochMint(ctx1, math.NewInt(700)))
	require.NoError(t, k.CheckAndTrackEpochMint(ctx1, math.NewInt(300)))
	require.ErrorIs(t, k.CheckAndTrackEpochMint(ctx1, math.NewInt(1)), types.ErrDreamMintCapExceeded)

	// Epoch 2: budget resets again.
	ctx2 := sdkCtx.WithBlockHeight(25)
	require.NoError(t, k.CheckAndTrackEpochMint(ctx2, math.NewInt(1_000)))
}
