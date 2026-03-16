package keeper_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

func TestQuerySession(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)

	// Not found
	_, err := qs.Session(f.ctx, &types.QuerySessionRequest{
		Granter: granter,
		Grantee: grantee,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no active session")

	// Create session
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	exp := sdkCtx.BlockTime().Add(24 * time.Hour)
	createTestSession(t, f, granter, grantee, types.DefaultAllowedMsgTypes[:2], exp)

	// Found
	resp, err := qs.Session(f.ctx, &types.QuerySessionRequest{
		Granter: granter,
		Grantee: grantee,
	})
	require.NoError(t, err)
	require.Equal(t, granter, resp.Session.Granter)
	require.Equal(t, grantee, resp.Session.Grantee)
	require.Equal(t, types.DefaultAllowedMsgTypes[:2], resp.Session.AllowedMsgTypes)
}

func TestQuerySessionNilRequest(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	_, err := qs.Session(f.ctx, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid request")
}
