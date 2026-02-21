package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/vote/keeper"
	"sparkdream/x/vote/types"
)

func TestSrsStateQuery(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	item := types.SrsState{}
	err := f.keeper.SrsState.Set(f.ctx, item)
	require.NoError(t, err)

	tests := []struct {
		desc     string
		request  *types.QueryGetSrsStateRequest
		response *types.QueryGetSrsStateResponse
		err      error
	}{
		{
			desc:     "First",
			request:  &types.QueryGetSrsStateRequest{},
			response: &types.QueryGetSrsStateResponse{SrsState: item},
		},
		{
			desc: "InvalidRequest",
			err:  status.Error(codes.InvalidArgument, "invalid request"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			response, err := qs.GetSrsState(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}
