package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestQueryTagExists_KnownTag(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	require.NoError(t, f.keeper.SetTag(f.ctx, types.Tag{Name: "sometag", ExpirationIndex: 999}))

	resp, err := qs.TagExists(f.ctx, &types.QueryTagExistsRequest{TagName: "sometag"})
	require.NoError(t, err)
	require.True(t, resp.Exists)
	require.Equal(t, int64(999), resp.ExpirationTime)
}

func TestQueryTagExists_UnknownTag(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	resp, err := qs.TagExists(f.ctx, &types.QueryTagExistsRequest{TagName: "never_registered"})
	require.NoError(t, err, "missing tag is an Exists=false response, not an error")
	require.False(t, resp.Exists)
	require.Equal(t, int64(0), resp.ExpirationTime)
}

func TestQueryTagExists_InvalidRequests(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	_, err := qs.TagExists(f.ctx, nil)
	require.Error(t, err)

	_, err = qs.TagExists(f.ctx, &types.QueryTagExistsRequest{TagName: ""})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.InvalidArgument, st.Code())
}
