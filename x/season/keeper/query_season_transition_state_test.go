package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestSeasonTransitionStateQuery(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	item := types.SeasonTransitionState{}
	err := f.keeper.SeasonTransitionState.Set(f.ctx, item)
	require.NoError(t, err)

	tests := []struct {
		desc     string
		request  *types.QueryGetSeasonTransitionStateRequest
		response *types.QueryGetSeasonTransitionStateResponse
		err      error
	}{
		{
			desc:     "First",
			request:  &types.QueryGetSeasonTransitionStateRequest{},
			response: &types.QueryGetSeasonTransitionStateResponse{SeasonTransitionState: item},
		},
		{
			desc: "InvalidRequest",
			err:  status.Error(codes.InvalidArgument, "invalid request"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			response, err := qs.GetSeasonTransitionState(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}
