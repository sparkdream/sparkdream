package keeper_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/session/types"
)

func TestGenesis(t *testing.T) {
	genesisState := types.GenesisState{
		Params: types.DefaultParams(),
	}

	f := initFixture(t)
	err := f.keeper.InitGenesis(f.ctx, genesisState)
	require.NoError(t, err)
	got, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NotNil(t, got)

	require.EqualExportedValues(t, genesisState.Params, got.Params)
}

func TestGenesisWithSessions(t *testing.T) {
	f := initFixture(t)

	granter := testAddr("granter", f.addressCodec)
	grantee1 := testAddr("grantee1", f.addressCodec)
	grantee2 := testAddr("grantee2", f.addressCodec)

	now := time.Now().UTC()
	exp := now.Add(24 * time.Hour)

	sessions := []types.Session{
		{
			Granter:         granter,
			Grantee:         grantee1,
			AllowedMsgTypes: types.DefaultAllowedMsgTypes[:2],
			SpendLimit:      sdk.NewInt64Coin("uspark", 10_000_000),
			Spent:           sdk.NewInt64Coin("uspark", 500_000),
			Expiration:      exp,
			CreatedAt:       now,
			LastUsedAt:      now,
			ExecCount:       3,
			MaxExecCount:    100,
		},
		{
			Granter:         granter,
			Grantee:         grantee2,
			AllowedMsgTypes: types.DefaultAllowedMsgTypes[:1],
			SpendLimit:      sdk.NewInt64Coin("uspark", 5_000_000),
			Spent:           sdk.NewInt64Coin("uspark", 0),
			Expiration:      exp,
			CreatedAt:       now,
			LastUsedAt:      now,
			ExecCount:       0,
			MaxExecCount:    0,
		},
	}

	genesisState := types.GenesisState{
		Params:   types.DefaultParams(),
		Sessions: sessions,
	}

	err := f.keeper.InitGenesis(f.ctx, genesisState)
	require.NoError(t, err)

	// Verify sessions were loaded
	s1, err := f.keeper.GetSession(f.ctx, granter, grantee1)
	require.NoError(t, err)
	require.Equal(t, uint64(3), s1.ExecCount)
	require.Equal(t, sdk.NewInt64Coin("uspark", 500_000), s1.Spent)

	s2, err := f.keeper.GetSession(f.ctx, granter, grantee2)
	require.NoError(t, err)
	require.Equal(t, uint64(0), s2.ExecCount)

	// Verify indexes were populated
	has, err := f.keeper.SessionsByGranter.Has(f.ctx, makeGranterKey(granter, grantee1))
	require.NoError(t, err)
	require.True(t, has)

	has, err = f.keeper.SessionsByGrantee.Has(f.ctx, makeGranteeKey(grantee1, granter))
	require.NoError(t, err)
	require.True(t, has)

	// Export and verify round-trip
	got, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, got.Sessions, 2)
}

func TestGenesisExportRoundTrip(t *testing.T) {
	f := initFixture(t)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	now := time.Now().UTC()
	exp := now.Add(24 * time.Hour)

	genesisState := types.GenesisState{
		Params: types.DefaultParams(),
		Sessions: []types.Session{
			{
				Granter:         granter,
				Grantee:         grantee,
				AllowedMsgTypes: types.DefaultAllowedMsgTypes[:3],
				SpendLimit:      sdk.NewInt64Coin("uspark", 50_000_000),
				Spent:           sdk.NewInt64Coin("uspark", 1_000_000),
				Expiration:      exp,
				CreatedAt:       now,
				LastUsedAt:      now,
				ExecCount:       7,
				MaxExecCount:    50,
			},
		},
	}

	// Init
	require.NoError(t, f.keeper.InitGenesis(f.ctx, genesisState))

	// Export
	got, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)

	// Re-init with exported state in a fresh fixture
	f2 := initFixture(t)
	require.NoError(t, f2.keeper.InitGenesis(f2.ctx, *got))

	// Export again and compare
	got2, err := f2.keeper.ExportGenesis(f2.ctx)
	require.NoError(t, err)

	require.EqualExportedValues(t, got.Params, got2.Params)
	require.Len(t, got2.Sessions, 1)
	require.Equal(t, got.Sessions[0].Granter, got2.Sessions[0].Granter)
	require.Equal(t, got.Sessions[0].ExecCount, got2.Sessions[0].ExecCount)
}
