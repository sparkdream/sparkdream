package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestQueryReservedTag_GetAndList(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	require.NoError(t, f.keeper.SetReservedTag(f.ctx, types.ReservedTag{Name: "gov", Authority: "council"}))
	require.NoError(t, f.keeper.SetReservedTag(f.ctx, types.ReservedTag{Name: "admin", Authority: "council"}))

	got, err := qs.GetReservedTag(f.ctx, &types.QueryGetReservedTagRequest{Name: "gov"})
	require.NoError(t, err)
	require.Equal(t, "gov", got.ReservedTag.Name)

	listed, err := qs.ListReservedTag(f.ctx, &types.QueryAllReservedTagRequest{})
	require.NoError(t, err)
	require.Len(t, listed.ReservedTag, 2)
}

func TestQueryReservedTag_GetNotFound(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	_, err := qs.GetReservedTag(f.ctx, &types.QueryGetReservedTagRequest{Name: "unset"})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.NotFound, st.Code())
}

func TestQueryReservedTag_NilRequest(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	_, err := qs.GetReservedTag(f.ctx, nil)
	require.Error(t, err)
	_, err = qs.ListReservedTag(f.ctx, nil)
	require.Error(t, err)
}
