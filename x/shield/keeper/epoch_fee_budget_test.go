package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/types"
)

// TestChargeEpochFee (SHD-12): fees the module pays are booked per shield
// epoch against max_fees_per_epoch; a fee that doesn't fit is refused and not
// booked, and the budget is fresh in the next epoch.
func TestChargeEpochFee(t *testing.T) {
	f := initFixture(t)
	budget := math.NewInt(100)
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 7, EpochStartHeight: 1}))

	require.NoError(t, f.keeper.ChargeEpochFee(f.ctx, math.NewInt(60), budget))
	require.NoError(t, f.keeper.ChargeEpochFee(f.ctx, math.NewInt(40), budget))
	require.Equal(t, math.NewInt(100), f.keeper.EpochFeesPaid(f.ctx))
	require.ErrorIs(t, f.keeper.ChargeEpochFee(f.ctx, math.NewInt(1), budget), types.ErrEpochFeeBudgetExhausted)
	require.Equal(t, math.NewInt(100), f.keeper.EpochFeesPaid(f.ctx), "a refused fee is not booked")
	require.NoError(t, f.keeper.ChargeEpochFee(f.ctx, math.ZeroInt(), budget), "zero fees cost nothing")

	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 8, EpochStartHeight: 51}))
	require.True(t, f.keeper.EpochFeesPaid(f.ctx).IsZero())
	require.NoError(t, f.keeper.ChargeEpochFee(f.ctx, math.NewInt(100), budget))
}

// TestMaxFeesPerEpochValidation: the epoch budget must exist and cover at
// least one max-fee exec.
func TestMaxFeesPerEpochValidation(t *testing.T) {
	p := types.DefaultParams()
	require.NoError(t, p.Validate())
	p.MaxFeesPerEpoch = p.MaxFeePerExec.SubRaw(1)
	require.Error(t, p.Validate())
	p.MaxFeesPerEpoch = math.Int{}
	require.Error(t, p.Validate())
}
