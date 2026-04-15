package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func TestQueryGetVerifier(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	qs := keeper.NewQueryServerImpl(f.keeper)

	addr := bondTestVerifier(t, f, ms, "query-verifier")

	resp, err := qs.GetVerifier(f.ctx, &types.QueryGetVerifierRequest{Address: addr})
	require.NoError(t, err)
	require.Equal(t, addr, resp.Verifier.Address)

	_, err = qs.GetVerifier(f.ctx, &types.QueryGetVerifierRequest{Address: "nonexistent"})
	require.Error(t, err)
}
