package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/identity/keeper"
	"sparkdream/x/identity/types"
)

func TestQueryChainIdentityNotInitialized(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	_, err := qs.ChainIdentity(f.ctx, &types.QueryChainIdentityRequest{})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.NotFound, st.Code())
}

func TestQueryChainIdentityAfterInit(t *testing.T) {
	f := initFixture(t)
	id := newValidIdentity()
	require.NoError(t, f.keeper.InitGenesis(f.ctx, types.GenesisState{Identity: id, AllowChainIdMismatch: true}))

	qs := keeper.NewQueryServerImpl(f.keeper)
	resp, err := qs.ChainIdentity(f.ctx, &types.QueryChainIdentityRequest{})
	require.NoError(t, err)
	require.True(t, resp.Identity.EqualCanonical(id))
}

func TestQueryBondAndDreamDenom(t *testing.T) {
	f := initFixture(t)
	id := newValidIdentity()
	require.NoError(t, f.keeper.InitGenesis(f.ctx, types.GenesisState{Identity: id, AllowChainIdMismatch: true}))

	qs := keeper.NewQueryServerImpl(f.keeper)
	b, err := qs.BondDenom(f.ctx, &types.QueryBondDenomRequest{})
	require.NoError(t, err)
	require.Equal(t, id.BondDenom, b.Denom)

	d, err := qs.DreamDenom(f.ctx, &types.QueryDreamDenomRequest{})
	require.NoError(t, err)
	require.Equal(t, id.DreamDenom, d.Denom)
}
