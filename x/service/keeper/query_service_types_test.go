package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/service/types"
)

func TestQueryServiceTypes(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	// Insert a second one.
	other := types.ServiceTypeConfig{
		ServiceType:            "other-service",
		Description:            "second",
		MinBondAmount:                math.NewInt(1_000_000),
		UnbondingPeriodBlocks:  20,
		UnilateralSlashCapBps:  500,
		Tier1WindowBlocks:      1000,
		Tier1AggregateCapBps:   1500,
		Tier1CooldownBlocks:    10,
		UnderfundedGraceBlocks: 10,
		Enabled:                true,
	}
	require.NoError(t, f.keeper.ServiceTypes.Set(f.ctx, other.ServiceType, other))

	resp, err := f.queryServer.ServiceTypes(f.ctx, &types.QueryServiceTypesRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Configs, 2)
}

func TestQueryServiceTypes_Empty(t *testing.T) {
	f := initFixture(t)

	resp, err := f.queryServer.ServiceTypes(f.ctx, &types.QueryServiceTypesRequest{})
	require.NoError(t, err)
	require.Empty(t, resp.Configs)
}

func TestQueryServiceTypes_NilRequest(t *testing.T) {
	f := initFixture(t)

	_, err := f.queryServer.ServiceTypes(f.ctx, nil)
	require.Error(t, err)
}
