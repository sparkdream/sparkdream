package keeper_test

import (
	"testing"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/types"
)

func TestTriggerDKGCustomThresholds(t *testing.T) {
	f, ms := initMsgServer(t)

	authority, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("gov"))
	require.NoError(t, err)

	resp, err := ms.TriggerDKG(f.ctx, &types.MsgTriggerDkg{
		Authority:            authority,
		ThresholdNumerator:   3,
		ThresholdDenominator: 5,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	dkgState, found := f.keeper.GetDKGStateVal(f.ctx)
	require.True(t, found)
	require.Equal(t, uint64(3), dkgState.ThresholdNumerator)
	require.Equal(t, uint64(5), dkgState.ThresholdDenominator)
}

func TestTriggerDKGInvalidAuthority(t *testing.T) {
	f, ms := initMsgServer(t)

	_, err := ms.TriggerDKG(f.ctx, &types.MsgTriggerDkg{
		Authority: "not_valid_bech32!!!",
	})
	require.Error(t, err)
}

func TestTriggerDKGWhileRegistering(t *testing.T) {
	f, ms := initMsgServer(t)

	authority, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("gov"))
	require.NoError(t, err)

	// Set DKG to REGISTERING
	require.NoError(t, f.keeper.SetDKGStateVal(f.ctx, types.DKGState{
		Round: 1,
		Phase: types.DKGPhase_DKG_PHASE_REGISTERING,
	}))

	_, err = ms.TriggerDKG(f.ctx, &types.MsgTriggerDkg{
		Authority: authority,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrDKGInProgress)
}

func TestTriggerDKGWhileContributing(t *testing.T) {
	f, ms := initMsgServer(t)

	authority, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("gov"))
	require.NoError(t, err)

	// Set DKG to CONTRIBUTING
	require.NoError(t, f.keeper.SetDKGStateVal(f.ctx, types.DKGState{
		Round: 1,
		Phase: types.DKGPhase_DKG_PHASE_CONTRIBUTING,
	}))

	_, err = ms.TriggerDKG(f.ctx, &types.MsgTriggerDkg{
		Authority: authority,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrDKGInProgress)
}

func TestTriggerDKGAfterInactive(t *testing.T) {
	f, ms := initMsgServer(t)

	authority, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("gov"))
	require.NoError(t, err)

	// Set DKG to INACTIVE (previous round failed)
	require.NoError(t, f.keeper.SetDKGStateVal(f.ctx, types.DKGState{
		Round: 3,
		Phase: types.DKGPhase_DKG_PHASE_INACTIVE,
	}))

	resp, err := ms.TriggerDKG(f.ctx, &types.MsgTriggerDkg{
		Authority: authority,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	dkgState, found := f.keeper.GetDKGStateVal(f.ctx)
	require.True(t, found)
	require.Equal(t, uint64(4), dkgState.Round) // Should increment from 3
	require.Equal(t, types.DKGPhase_DKG_PHASE_REGISTERING, dkgState.Phase)
}

func TestTriggerDKGDisablesEncryptedBatch(t *testing.T) {
	f, ms := initMsgServer(t)

	authority, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("gov"))
	require.NoError(t, err)

	// Enable encrypted batch first
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	_, err = ms.TriggerDKG(f.ctx, &types.MsgTriggerDkg{
		Authority: authority,
	})
	require.NoError(t, err)

	// Encrypted batch should be disabled during DKG
	params, err = f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	require.False(t, params.EncryptedBatchEnabled)
}

func TestTriggerDKGSetsDeadlines(t *testing.T) {
	f, ms := initMsgServer(t)

	authority, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("gov"))
	require.NoError(t, err)

	// Set block height
	f.ctx = f.ctx.WithBlockHeight(100)

	_, err = ms.TriggerDKG(f.ctx, &types.MsgTriggerDkg{
		Authority: authority,
	})
	require.NoError(t, err)

	dkgState, found := f.keeper.GetDKGStateVal(f.ctx)
	require.True(t, found)
	require.Equal(t, int64(100), dkgState.OpenAtHeight)
	require.Greater(t, dkgState.RegistrationDeadline, int64(100))
	require.Greater(t, dkgState.ContributionDeadline, dkgState.RegistrationDeadline)
}

// TestTriggerDKGRefusedDuringComplaintPhases: a governance re-key must not
// silently discard a round that is between CONTRIBUTING and ACTIVE.
func TestTriggerDKGRefusedDuringComplaintPhases(t *testing.T) {
	f, ms := initMsgServer(t)
	authority, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("gov"))
	require.NoError(t, err)
	for _, phase := range []types.DKGPhase{types.DKGPhase_DKG_PHASE_COMPLAINING, types.DKGPhase_DKG_PHASE_JUSTIFYING} {
		require.NoError(t, f.keeper.SetDKGStateVal(f.ctx, types.DKGState{Round: 1, Phase: phase, ExpectedValidators: []string{"val1"}}))
		_, err := ms.TriggerDKG(f.ctx, &types.MsgTriggerDkg{Authority: authority})
		require.ErrorIs(t, err, types.ErrDKGInProgress, "phase %s", phase)
	}
}

// TestTriggerDKGRejectsUnreachableThreshold: a threshold above the
// participant count could never be met, so the round could only fail.
func TestTriggerDKGRejectsUnreachableThreshold(t *testing.T) {
	f, ms := initMsgServer(t)
	authority, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("gov"))
	require.NoError(t, err)

	for _, tc := range []struct{ num, den uint64 }{
		{4, 3},             // above one
		{5, 0},             // above the default denominator 3
		{1, 101},           // denominator over the bound
		{1 << 62, 1 << 63}, // overflow-scale
	} {
		_, err := ms.TriggerDKG(f.ctx, &types.MsgTriggerDkg{
			Authority:            authority,
			ThresholdNumerator:   tc.num,
			ThresholdDenominator: tc.den,
		})
		require.ErrorIs(t, err, types.ErrInvalidDKGThreshold, "%d/%d", tc.num, tc.den)
	}
	_, found := f.keeper.GetDKGStateVal(f.ctx)
	require.False(t, found, "no round opened")
}
