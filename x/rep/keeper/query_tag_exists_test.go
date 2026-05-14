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

	// ExpirationTime is derived: last_used_at + DefaultTagExpiration.
	const lastUsed = int64(100_000)
	require.NoError(t, f.keeper.SetTag(f.ctx, types.Tag{Name: "sometag", LastUsedAt: lastUsed}))

	resp, err := qs.TagExists(f.ctx, &types.QueryTagExistsRequest{TagName: "sometag"})
	require.NoError(t, err)
	require.True(t, resp.Exists)
	require.Equal(t, lastUsed+types.DefaultTagExpiration, resp.ExpirationTime)
}

// A tag with LastUsedAt == 0 is treated as permanent — the query reports
// ExpirationTime = 0 to signal "no deadline".
func TestQueryTagExists_PermanentTag(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	require.NoError(t, f.keeper.SetTag(f.ctx, types.Tag{Name: "permatag"}))

	resp, err := qs.TagExists(f.ctx, &types.QueryTagExistsRequest{TagName: "permatag"})
	require.NoError(t, err)
	require.True(t, resp.Exists)
	require.Equal(t, int64(0), resp.ExpirationTime, "permanent tags report ExpirationTime = 0")
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
