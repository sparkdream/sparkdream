package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

func TestReverseResolve(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	// Setup Test Data

	// Scenario 1: User with a valid primary name
	userWithPrimary := "cosmos1alice"
	primaryName := "alice"
	err := f.keeper.Owners.Set(f.ctx, userWithPrimary, types.OwnerInfo{
		Address:     userWithPrimary,
		PrimaryName: primaryName,
	})
	require.NoError(t, err)
	require.NoError(t, f.keeper.SetName(f.ctx, types.NameRecord{Name: primaryName, Owner: userWithPrimary}))

	// Scenario 1b: accepted resolver target displaying a name it does not own
	targetUser := "cosmos1aurora"
	require.NoError(t, f.keeper.SetName(f.ctx, types.NameRecord{
		Name: "aurora", Owner: "cosmos1zenith", Target: targetUser, TargetAccepted: true,
	}))
	require.NoError(t, f.keeper.Owners.Set(f.ctx, targetUser, types.OwnerInfo{Address: targetUser, PrimaryName: "aurora"}))

	// Scenario 1c: stale primary — the name now belongs to someone else and
	// does not target this address, so the forward mapping fails.
	staleUser := "cosmos1phoenix"
	require.NoError(t, f.keeper.SetName(f.ctx, types.NameRecord{Name: "phoenix", Owner: "cosmos1zenith"}))
	require.NoError(t, f.keeper.Owners.Set(f.ctx, staleUser, types.OwnerInfo{Address: staleUser, PrimaryName: "phoenix"}))

	// Scenario 1d: target set but not accepted
	unacceptedUser := "cosmos1nova"
	require.NoError(t, f.keeper.SetName(f.ctx, types.NameRecord{Name: "nova", Owner: "cosmos1zenith", Target: unacceptedUser}))
	require.NoError(t, f.keeper.Owners.Set(f.ctx, unacceptedUser, types.OwnerInfo{Address: unacceptedUser, PrimaryName: "nova"}))

	// Scenario 2: User exists but has NO primary name set
	userNoPrimary := "cosmos1bob"
	err = f.keeper.Owners.Set(f.ctx, userNoPrimary, types.OwnerInfo{
		Address:     userNoPrimary,
		PrimaryName: "", // Empty
	})
	require.NoError(t, err)

	// Scenario 3: Unknown user (never registered anything) is just a random string below

	tests := []struct {
		desc     string
		request  *types.QueryReverseResolveRequest
		response *types.QueryReverseResolveResponse
		err      error
	}{
		{
			desc: "Success - Found Primary Name",
			request: &types.QueryReverseResolveRequest{
				Address: userWithPrimary,
			},
			response: &types.QueryReverseResolveResponse{
				Name: primaryName,
			},
		},
		{
			desc:     "Success - Accepted Target",
			request:  &types.QueryReverseResolveRequest{Address: targetUser},
			response: &types.QueryReverseResolveResponse{Name: "aurora"},
		},
		{
			desc:    "Failure - Stale Primary (forward mapping gone)",
			request: &types.QueryReverseResolveRequest{Address: staleUser},
			err:     status.Error(codes.NotFound, "account has no primary name set"),
		},
		{
			desc:    "Failure - Target Not Accepted",
			request: &types.QueryReverseResolveRequest{Address: unacceptedUser},
			err:     status.Error(codes.NotFound, "account has no primary name set"),
		},
		{
			desc: "Failure - Account Not Found (No Owner Info)",
			request: &types.QueryReverseResolveRequest{
				Address: "cosmos1unknown",
			},
			err: status.Error(codes.NotFound, "account has no name information"),
		},
		{
			desc: "Failure - Account Found But No Primary Name Set",
			request: &types.QueryReverseResolveRequest{
				Address: userNoPrimary,
			},
			err: status.Error(codes.NotFound, "account has no primary name set"),
		},
		{
			desc: "Failure - Empty Address",
			request: &types.QueryReverseResolveRequest{
				Address: "",
			},
			err: status.Error(codes.InvalidArgument, "address cannot be empty"),
		},
		{
			desc:    "Failure - Nil Request",
			request: nil,
			err:     status.Error(codes.InvalidArgument, "invalid request"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			response, err := qs.ReverseResolve(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.response, response)
			}
		})
	}
}
