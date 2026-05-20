package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/collections"
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

// sessionKeyGrant builds a SESSION_KEY-type Grant for genesis-roundtrip tests.
func sessionKeyGrant(id uint64, granter, grantee string, allowed []string, spendLimit, spent sdk.Coin, expiration, now time.Time, execCount, maxExec uint64) types.Grant {
	return types.Grant{
		Id:        id,
		Granter:   granter,
		Grantee:   grantee,
		Type:      types.GrantType_GRANT_TYPE_SESSION_KEY,
		Status:    types.GrantStatus_GRANT_STATUS_ACTIVE,
		CreatedAt: now,
		ExpiresAt: expiration,
		Payload: &types.Grant_SessionKey{
			SessionKey: &types.SessionKeyPayload{
				AllowedMsgTypes: allowed,
				SpendLimit:      spendLimit,
				Spent:           spent,
				LastUsedAt:      now,
				ExecCount:       execCount,
				MaxExecCount:    maxExec,
			},
		},
	}
}

func TestGenesisWithGrants(t *testing.T) {
	f := initFixture(t)

	granter := testAddr("granter", f.addressCodec)
	grantee1 := testAddr("grantee1", f.addressCodec)
	grantee2 := testAddr("grantee2", f.addressCodec)

	now := time.Now().UTC()
	exp := now.Add(24 * time.Hour)

	grants := []types.Grant{
		sessionKeyGrant(1, granter, grantee1, types.DefaultAllowedMsgTypes[:2],
			sdk.NewInt64Coin("uspark", 10_000_000), sdk.NewInt64Coin("uspark", 500_000),
			exp, now, 3, 100),
		sessionKeyGrant(2, granter, grantee2, types.DefaultAllowedMsgTypes[:1],
			sdk.NewInt64Coin("uspark", 5_000_000), sdk.NewInt64Coin("uspark", 0),
			exp, now, 0, 50),
	}

	genesisState := types.GenesisState{
		Params:   types.DefaultParams(),
		Grants:   grants,
		GrantSeq: 3,
	}

	err := f.keeper.InitGenesis(f.ctx, genesisState)
	require.NoError(t, err)

	// Verify sessions are visible via the back-compat GetSession path.
	s1, err := f.keeper.GetSession(f.ctx, granter, grantee1)
	require.NoError(t, err)
	require.Equal(t, uint64(3), s1.ExecCount)
	require.Equal(t, sdk.NewInt64Coin("uspark", 500_000), s1.Spent)

	s2, err := f.keeper.GetSession(f.ctx, granter, grantee2)
	require.NoError(t, err)
	require.Equal(t, uint64(0), s2.ExecCount)

	// Verify indexes were populated for grant id 1.
	hasGranter, err := f.keeper.GrantsByGranter.Has(f.ctx, collections.Join(granter, uint64(1)))
	require.NoError(t, err)
	require.True(t, hasGranter)

	hasGrantee, err := f.keeper.GrantsByGrantee.Has(f.ctx, collections.Join(grantee1, uint64(1)))
	require.NoError(t, err)
	require.True(t, hasGrantee)

	// Active-grant counter for (granter, SESSION_KEY) should be 2.
	count, err := f.keeper.CountActiveGrants(f.ctx, granter, types.GrantType_GRANT_TYPE_SESSION_KEY)
	require.NoError(t, err)
	require.Equal(t, uint32(2), count)

	// Export and verify round-trip
	got, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, got.Grants, 2)
	require.Equal(t, uint64(3), got.GrantSeq)
	require.Len(t, got.ActiveGrantCounts, 1)
	require.Equal(t, uint32(2), got.ActiveGrantCounts[0].Count)
}

func TestGenesisExportRoundTrip(t *testing.T) {
	f := initFixture(t)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	now := time.Now().UTC()
	exp := now.Add(24 * time.Hour)

	genesisState := types.GenesisState{
		Params: types.DefaultParams(),
		Grants: []types.Grant{
			sessionKeyGrant(1, granter, grantee, types.DefaultAllowedMsgTypes[:3],
				sdk.NewInt64Coin("uspark", 50_000_000), sdk.NewInt64Coin("uspark", 1_000_000),
				exp, now, 7, 50),
		},
		GrantSeq: 2,
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
	require.Len(t, got2.Grants, 1)
	require.Equal(t, got.Grants[0].Granter, got2.Grants[0].Granter)
	require.Equal(t, got.Grants[0].GetSessionKey().ExecCount, got2.Grants[0].GetSessionKey().ExecCount)
	require.Equal(t, got.GrantSeq, got2.GrantSeq)
}
