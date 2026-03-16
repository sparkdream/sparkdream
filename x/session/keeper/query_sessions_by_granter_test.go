package keeper_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

func TestQuerySessionsByGranter(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	otherGranter := testAddr("other_granter", f.addressCodec)

	// No sessions for granter
	resp, err := qs.SessionsByGranter(f.ctx, &types.QuerySessionsByGranterRequest{
		Granter: granter,
	})
	require.NoError(t, err)
	require.Empty(t, resp.Sessions)

	// Create 3 sessions for granter, 1 for other granter
	exp := sdkCtx.BlockTime().Add(24 * time.Hour)
	for i := 0; i < 3; i++ {
		grantee := testAddr("grantee"+string(rune('a'+i)), f.addressCodec)
		createTestSession(t, f, granter, grantee, types.DefaultAllowedMsgTypes[:1], exp)
	}
	createTestSession(t, f, otherGranter, testAddr("other_grantee", f.addressCodec), types.DefaultAllowedMsgTypes[:1], exp)

	// Query granter's sessions
	resp, err = qs.SessionsByGranter(f.ctx, &types.QuerySessionsByGranterRequest{
		Granter: granter,
	})
	require.NoError(t, err)
	require.Len(t, resp.Sessions, 3)

	// All sessions should be from the correct granter
	for _, s := range resp.Sessions {
		require.Equal(t, granter, s.Granter)
	}

	// Query other granter
	resp, err = qs.SessionsByGranter(f.ctx, &types.QuerySessionsByGranterRequest{
		Granter: otherGranter,
	})
	require.NoError(t, err)
	require.Len(t, resp.Sessions, 1)
}

func TestQuerySessionsByGranterNilRequest(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	_, err := qs.SessionsByGranter(f.ctx, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid request")
}
