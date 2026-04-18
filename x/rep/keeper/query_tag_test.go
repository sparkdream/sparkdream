package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestQueryTag_ListAndGet(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	// Seeded via initFixture — pick a known name.
	got, err := qs.GetTag(f.ctx, &types.QueryGetTagRequest{Name: "backend"})
	require.NoError(t, err)
	require.Equal(t, "backend", got.Tag.Name)

	// ListTag should page through at least the seeded entries.
	listed, err := qs.ListTag(f.ctx, &types.QueryAllTagRequest{})
	require.NoError(t, err)
	require.NotEmpty(t, listed.Tag)
}

func TestQueryTag_GetNotFound(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	_, err := qs.GetTag(f.ctx, &types.QueryGetTagRequest{Name: "never_registered"})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.NotFound, st.Code())
}

func TestQueryTag_NilRequestRejected(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	_, err := qs.GetTag(f.ctx, nil)
	require.Error(t, err)
	_, err = qs.ListTag(f.ctx, nil)
	require.Error(t, err)
}
