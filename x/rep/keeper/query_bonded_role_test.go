package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestQueryBondedRole_Basic(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("sentinelA")).String()

	// Missing record → NotFound.
	_, err := qs.BondedRole(f.ctx, &types.QueryBondedRoleRequest{
		RoleType: types.RoleType_ROLE_TYPE_FORUM_SENTINEL,
		Address:  addr,
	})
	require.Equal(t, codes.NotFound, status.Code(err))

	// Seed and fetch.
	require.NoError(t, f.keeper.BondedRoles.Set(f.ctx,
		collections.Join(int32(types.RoleType_ROLE_TYPE_FORUM_SENTINEL), addr),
		types.BondedRole{
			Address:     addr,
			RoleType:    types.RoleType_ROLE_TYPE_FORUM_SENTINEL,
			CurrentBond: "1000",
			BondStatus:  types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL,
		},
	))

	resp, err := qs.BondedRole(f.ctx, &types.QueryBondedRoleRequest{
		RoleType: types.RoleType_ROLE_TYPE_FORUM_SENTINEL,
		Address:  addr,
	})
	require.NoError(t, err)
	require.Equal(t, addr, resp.BondedRole.Address)
	require.Equal(t, "1000", resp.BondedRole.CurrentBond)
	require.Equal(t, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, resp.BondedRole.RoleType)
}

func TestQueryBondedRole_Validation(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	// nil request.
	_, err := qs.BondedRole(f.ctx, nil)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	// Unspecified role_type.
	_, err = qs.BondedRole(f.ctx, &types.QueryBondedRoleRequest{
		RoleType: types.RoleType_ROLE_TYPE_UNSPECIFIED,
		Address:  sdk.AccAddress([]byte("any")).String(),
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	// Empty address.
	_, err = qs.BondedRole(f.ctx, &types.QueryBondedRoleRequest{
		RoleType: types.RoleType_ROLE_TYPE_FORUM_SENTINEL,
		Address:  "",
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestQueryBondedRolesByType_PrefixIteration(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	// Seed: three sentinels, two curators. The query should return only the
	// role_type it was asked about.
	seed := func(role types.RoleType, addr string) {
		require.NoError(t, f.keeper.BondedRoles.Set(f.ctx,
			collections.Join(int32(role), addr),
			types.BondedRole{Address: addr, RoleType: role, CurrentBond: "100"},
		))
	}
	seed(types.RoleType_ROLE_TYPE_FORUM_SENTINEL, sdk.AccAddress([]byte("s1")).String())
	seed(types.RoleType_ROLE_TYPE_FORUM_SENTINEL, sdk.AccAddress([]byte("s2")).String())
	seed(types.RoleType_ROLE_TYPE_FORUM_SENTINEL, sdk.AccAddress([]byte("s3")).String())
	seed(types.RoleType_ROLE_TYPE_COLLECT_CURATOR, sdk.AccAddress([]byte("c1")).String())
	seed(types.RoleType_ROLE_TYPE_COLLECT_CURATOR, sdk.AccAddress([]byte("c2")).String())

	resp, err := qs.BondedRolesByType(f.ctx, &types.QueryBondedRolesByTypeRequest{
		RoleType: types.RoleType_ROLE_TYPE_FORUM_SENTINEL,
	})
	require.NoError(t, err)
	require.Len(t, resp.BondedRoles, 3)
	for _, br := range resp.BondedRoles {
		require.Equal(t, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, br.RoleType)
	}

	resp, err = qs.BondedRolesByType(f.ctx, &types.QueryBondedRolesByTypeRequest{
		RoleType: types.RoleType_ROLE_TYPE_COLLECT_CURATOR,
	})
	require.NoError(t, err)
	require.Len(t, resp.BondedRoles, 2)
}

func TestQueryBondedRolesByType_Validation(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	_, err := qs.BondedRolesByType(f.ctx, nil)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = qs.BondedRolesByType(f.ctx, &types.QueryBondedRolesByTypeRequest{
		RoleType: types.RoleType_ROLE_TYPE_UNSPECIFIED,
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestQueryBondedRoleConfig_Basic(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	// initFixture seeds FORUM_SENTINEL + COLLECT_CURATOR + FEDERATION_VERIFIER
	// configs from DefaultGenesis, so these should be found.
	resp, err := qs.BondedRoleConfig(f.ctx, &types.QueryBondedRoleConfigRequest{
		RoleType: types.RoleType_ROLE_TYPE_FORUM_SENTINEL,
	})
	require.NoError(t, err)
	require.Equal(t, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, resp.BondedRoleConfig.RoleType)
	require.NotEmpty(t, resp.BondedRoleConfig.MinBond)

	resp, err = qs.BondedRoleConfig(f.ctx, &types.QueryBondedRoleConfigRequest{
		RoleType: types.RoleType_ROLE_TYPE_COLLECT_CURATOR,
	})
	require.NoError(t, err)
	require.Equal(t, types.RoleType_ROLE_TYPE_COLLECT_CURATOR, resp.BondedRoleConfig.RoleType)

	resp, err = qs.BondedRoleConfig(f.ctx, &types.QueryBondedRoleConfigRequest{
		RoleType: types.RoleType_ROLE_TYPE_FEDERATION_VERIFIER,
	})
	require.NoError(t, err)
	require.Equal(t, types.RoleType_ROLE_TYPE_FEDERATION_VERIFIER, resp.BondedRoleConfig.RoleType)
}

func TestQueryBondedRoleConfig_Validation(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	_, err := qs.BondedRoleConfig(f.ctx, nil)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = qs.BondedRoleConfig(f.ctx, &types.QueryBondedRoleConfigRequest{
		RoleType: types.RoleType_ROLE_TYPE_UNSPECIFIED,
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}
