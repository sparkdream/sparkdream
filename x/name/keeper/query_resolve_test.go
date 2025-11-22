package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

func TestResolve(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	// Setup Test Data
	name := "alice"
	record := types.NameRecord{
		Name:  name,
		Owner: "cosmos1alice",
		Data:  "meta",
	}
	// Manually insert the record into the store
	err := f.keeper.Names.Set(f.ctx, name, record)
	require.NoError(t, err)

	tests := []struct {
		desc     string
		request  *types.QueryResolveRequest
		response *types.QueryResolveResponse
		err      error
	}{
		{
			desc: "Success - Found Name",
			request: &types.QueryResolveRequest{
				Name: name,
			},
			response: &types.QueryResolveResponse{
				NameRecord: &record,
			},
		},
		{
			desc: "Failure - Name Not Found",
			request: &types.QueryResolveRequest{
				Name: "bob",
			},
			err: status.Error(codes.NotFound, "name not found"),
		},
		{
			desc: "Failure - Empty Name",
			request: &types.QueryResolveRequest{
				Name: "",
			},
			err: status.Error(codes.InvalidArgument, "name cannot be empty"),
		},
		{
			desc:    "Failure - Nil Request",
			request: nil,
			err:     status.Error(codes.InvalidArgument, "invalid request"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			response, err := qs.Resolve(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.response, response)
			}
		})
	}
}
