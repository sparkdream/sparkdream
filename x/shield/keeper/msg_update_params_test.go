package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/types"
)

func TestUpdateParamsAllFields(t *testing.T) {
	f, ms := initMsgServer(t)

	authority, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("gov"))
	require.NoError(t, err)

	newParams := types.DefaultParams()
	newParams.Enabled = false
	newParams.MaxGasPerExec = 1_000_000
	newParams.MaxExecsPerIdentityPerEpoch = 200
	newParams.MaxFundingPerDay = math.NewInt(500_000_000)
	newParams.MinGasReserve = math.NewInt(50_000_000)
	newParams.ShieldEpochInterval = 100

	resp, err := ms.UpdateParams(f.ctx, &types.MsgUpdateParams{
		Authority: authority,
		Params:    newParams,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	got, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	require.False(t, got.Enabled)
	require.Equal(t, uint64(1_000_000), got.MaxGasPerExec)
	require.Equal(t, uint64(200), got.MaxExecsPerIdentityPerEpoch)
	require.Equal(t, math.NewInt(500_000_000), got.MaxFundingPerDay)
	require.Equal(t, math.NewInt(50_000_000), got.MinGasReserve)
	require.Equal(t, uint64(100), got.ShieldEpochInterval)
}

func TestUpdateParamsInvalidAuthority(t *testing.T) {
	f, ms := initMsgServer(t)

	_, err := ms.UpdateParams(f.ctx, &types.MsgUpdateParams{
		Authority: "not_valid_bech32!!!",
		Params:    types.DefaultParams(),
	})
	require.Error(t, err)
}

func TestUpdateParamsZeroMaxGas(t *testing.T) {
	f, ms := initMsgServer(t)

	authority, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("gov"))
	require.NoError(t, err)

	badParams := types.DefaultParams()
	badParams.MaxGasPerExec = 0

	_, err = ms.UpdateParams(f.ctx, &types.MsgUpdateParams{
		Authority: authority,
		Params:    badParams,
	})
	require.Error(t, err)
}

func TestUpdateParamsZeroMaxExecs(t *testing.T) {
	f, ms := initMsgServer(t)

	authority, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("gov"))
	require.NoError(t, err)

	badParams := types.DefaultParams()
	badParams.MaxExecsPerIdentityPerEpoch = 0

	_, err = ms.UpdateParams(f.ctx, &types.MsgUpdateParams{
		Authority: authority,
		Params:    badParams,
	})
	require.Error(t, err)
}

func TestUpdateParamsMissToleranceExceedsWindow(t *testing.T) {
	f, ms := initMsgServer(t)

	authority, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("gov"))
	require.NoError(t, err)

	badParams := types.DefaultParams()
	badParams.TleMissTolerance = 200
	badParams.TleMissWindow = 100

	_, err = ms.UpdateParams(f.ctx, &types.MsgUpdateParams{
		Authority: authority,
		Params:    badParams,
	})
	require.Error(t, err)
}

func TestUpdateParamsNegativeFunding(t *testing.T) {
	f, ms := initMsgServer(t)

	authority, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("gov"))
	require.NoError(t, err)

	badParams := types.DefaultParams()
	badParams.MaxFundingPerDay = math.NewInt(-1)

	_, err = ms.UpdateParams(f.ctx, &types.MsgUpdateParams{
		Authority: authority,
		Params:    badParams,
	})
	require.Error(t, err)
}
