package types_test

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/name/types"
)

func TestDefaultParams(t *testing.T) {
	p := types.DefaultParams()
	require.NoError(t, p.Validate())
	require.Equal(t, types.DefaultMinNameLength, p.MinNameLength)
	require.Equal(t, types.DefaultMaxNameLength, p.MaxNameLength)
	require.Equal(t, types.DefaultMaxNamesPerAddress, p.MaxNamesPerAddress)
	require.Equal(t, types.DefaultRegistrationFee, p.RegistrationFee)
	require.Equal(t, types.DefaultExpirationDuration, p.ExpirationDuration)
	require.Equal(t, types.DefaultDisputeStakeDream, p.DisputeStakeDream)
	require.Equal(t, types.DefaultDisputeTimeoutBlocks, p.DisputeTimeoutBlocks)
	require.Equal(t, types.DefaultContestStakeDream, p.ContestStakeDream)
	require.Equal(t, types.DefaultBlockedNames, p.BlockedNames)
}

func TestParamKeyTable(t *testing.T) {
	require.NotPanics(t, func() { _ = types.ParamKeyTable() })
}

func TestParamSetPairs(t *testing.T) {
	p := types.DefaultParams()
	pairs := p.ParamSetPairs()
	require.Len(t, pairs, 6)
}

func TestParams_Validate(t *testing.T) {
	validCoin := sdk.NewCoin("uspark", math.NewInt(10))

	testCases := []struct {
		name    string
		mutate  func(p *types.Params)
		wantErr string
	}{
		{"default ok", func(p *types.Params) {}, ""},
		{"empty blocked name", func(p *types.Params) { p.BlockedNames = []string{""} }, "blocked name cannot be empty"},
		{"zero min length", func(p *types.Params) { p.MinNameLength = 0 }, "min name length must be positive"},
		{"zero max length", func(p *types.Params) { p.MaxNameLength = 0 }, "max name length must be positive"},
		{"zero max names per address", func(p *types.Params) { p.MaxNamesPerAddress = 0 }, "max names per address must be positive"},
		{"zero expiration duration", func(p *types.Params) { p.ExpirationDuration = 0 }, "expiration duration must be positive"},
		{"invalid registration fee", func(p *types.Params) { p.RegistrationFee = sdk.Coin{Denom: "", Amount: math.NewInt(1)} }, "invalid registration fee coin"},
		{"negative dispute stake", func(p *types.Params) { p.DisputeStakeDream = math.NewInt(-1) }, "dispute stake must be non-negative"},
		{"negative contest stake", func(p *types.Params) { p.ContestStakeDream = math.NewInt(-1) }, "contest stake must be non-negative"},
		{"custom valid fee", func(p *types.Params) { p.RegistrationFee = validCoin }, ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			p := types.DefaultParams()
			tc.mutate(&p)
			err := p.Validate()
			if tc.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.wantErr)
			}
		})
	}
}

func TestDefaultNameOperationalParams(t *testing.T) {
	op := types.DefaultNameOperationalParams()
	require.NoError(t, op.Validate())
	require.Equal(t, types.DefaultExpirationDuration, op.ExpirationDuration)
	require.Equal(t, types.DefaultRegistrationFee, op.RegistrationFee)
	require.Equal(t, types.DefaultDisputeStakeDream, op.DisputeStakeDream)
	require.Equal(t, types.DefaultDisputeTimeoutBlocks, op.DisputeTimeoutBlocks)
	require.Equal(t, types.DefaultContestStakeDream, op.ContestStakeDream)
}

func TestNameOperationalParams_Validate(t *testing.T) {
	testCases := []struct {
		name    string
		mutate  func(op *types.NameOperationalParams)
		wantErr string
	}{
		{"default ok", func(op *types.NameOperationalParams) {}, ""},
		{"zero expiration", func(op *types.NameOperationalParams) { op.ExpirationDuration = 0 }, "expiration duration must be positive"},
		{"negative expiration", func(op *types.NameOperationalParams) { op.ExpirationDuration = -time.Hour }, "expiration duration must be positive"},
		{"invalid fee", func(op *types.NameOperationalParams) { op.RegistrationFee = sdk.Coin{Denom: "", Amount: math.NewInt(1)} }, "invalid registration fee coin"},
		{"negative dispute stake", func(op *types.NameOperationalParams) { op.DisputeStakeDream = math.NewInt(-1) }, "dispute stake must be non-negative"},
		{"negative contest stake", func(op *types.NameOperationalParams) { op.ContestStakeDream = math.NewInt(-1) }, "contest stake must be non-negative"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			op := types.DefaultNameOperationalParams()
			tc.mutate(&op)
			err := op.Validate()
			if tc.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.wantErr)
			}
		})
	}
}

func TestParams_ApplyAndExtractOperationalParams(t *testing.T) {
	p := types.DefaultParams()
	original := p

	customFee := sdk.NewCoin("uspark", math.NewInt(42))
	op := types.NameOperationalParams{
		ExpirationDuration:   2 * time.Hour,
		RegistrationFee:      customFee,
		DisputeStakeDream:    math.NewInt(7),
		DisputeTimeoutBlocks: 99,
		ContestStakeDream:    math.NewInt(8),
	}

	updated := p.ApplyOperationalParams(op)

	require.Equal(t, 2*time.Hour, updated.ExpirationDuration)
	require.Equal(t, customFee, updated.RegistrationFee)
	require.Equal(t, math.NewInt(7), updated.DisputeStakeDream)
	require.Equal(t, uint64(99), updated.DisputeTimeoutBlocks)
	require.Equal(t, math.NewInt(8), updated.ContestStakeDream)

	// Non-operational fields must remain unchanged.
	require.Equal(t, original.BlockedNames, updated.BlockedNames)
	require.Equal(t, original.MinNameLength, updated.MinNameLength)
	require.Equal(t, original.MaxNameLength, updated.MaxNameLength)
	require.Equal(t, original.MaxNamesPerAddress, updated.MaxNamesPerAddress)

	extracted := updated.ExtractOperationalParams()
	require.Equal(t, op, extracted)
}
