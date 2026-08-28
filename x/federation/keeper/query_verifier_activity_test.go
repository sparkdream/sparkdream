package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
	reptypes "sparkdream/x/rep/types"
)

func TestQueryVerifierActivity_Basic(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	addr := testAddr(t, f, "verif-activity-1")

	// The response is a PROJECTION over two state owners, so seed both: the
	// slim federation-local record...
	require.NoError(t, f.keeper.VerifierActivity.Set(f.ctx, addr, types.VerifierActivity{
		Address:                   addr,
		UnchallengedVerifications: 2,
	}))
	// ...and the shared accountability record x/rep owns.
	require.NoError(t, f.repKeeper.SeedRoleActivity(
		reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER, addr, reptypes.RoleActivity{
			TotalActions:      map[string]uint64{reptypes.ActionKindFederationVerify: 10},
			UpheldActions:     map[string]uint64{reptypes.ActionKindFederationVerify: 7},
			OverturnedActions: map[string]uint64{reptypes.ActionKindFederationVerify: 1},
			ConsecutiveUpheld: 3,
		}))

	resp, err := qs.VerifierActivity(f.ctx, &types.QueryVerifierActivityRequest{Address: addr})
	require.NoError(t, err)
	require.Equal(t, addr, resp.Activity.Address)
	require.Equal(t, uint64(10), resp.Activity.TotalVerifications)
	require.Equal(t, uint64(7), resp.Activity.UpheldVerifications)
	require.Equal(t, uint64(1), resp.Activity.OverturnedVerifications)
	require.Equal(t, uint64(2), resp.Activity.UnchallengedVerifications)
	require.Equal(t, uint64(3), resp.Activity.ConsecutiveUpheld)
	// slash_count is derived from the overturned count, not stored.
	require.Equal(t, uint64(1), resp.Activity.SlashCount)
}

func TestQueryVerifierActivity_MissingReturnsZeroedRecord(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	addr := testAddr(t, f, "never-verified")

	// A verifier who has never submitted a verification: the query returns a
	// zero-valued record with the address populated (rather than NotFound).
	resp, err := qs.VerifierActivity(f.ctx, &types.QueryVerifierActivityRequest{Address: addr})
	require.NoError(t, err)
	require.Equal(t, addr, resp.Activity.Address)
	require.Zero(t, resp.Activity.TotalVerifications)
	require.Zero(t, resp.Activity.UpheldVerifications)
}

func TestQueryVerifierActivity_Validation(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	// nil request.
	_, err := qs.VerifierActivity(f.ctx, nil)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	// Empty address.
	_, err = qs.VerifierActivity(f.ctx, &types.QueryVerifierActivityRequest{Address: ""})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}
