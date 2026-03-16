package keeper_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

func TestQuerySessionsByGrantee(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	grantee := testAddr("grantee", f.addressCodec)
	otherGrantee := testAddr("other_grantee", f.addressCodec)

	// No sessions for grantee
	resp, err := qs.SessionsByGrantee(f.ctx, &types.QuerySessionsByGranteeRequest{
		Grantee: grantee,
	})
	require.NoError(t, err)
	require.Empty(t, resp.Sessions)

	// Create 2 sessions for grantee (from different granters), 1 for other grantee
	exp := sdkCtx.BlockTime().Add(24 * time.Hour)
	for i := 0; i < 2; i++ {
		granter := testAddr("granter"+string(rune('a'+i)), f.addressCodec)
		createTestSession(t, f, granter, grantee, types.DefaultAllowedMsgTypes[:1], exp)
	}
	createTestSession(t, f, testAddr("other_granter", f.addressCodec), otherGrantee, types.DefaultAllowedMsgTypes[:1], exp)

	// Query grantee's sessions
	resp, err = qs.SessionsByGrantee(f.ctx, &types.QuerySessionsByGranteeRequest{
		Grantee: grantee,
	})
	require.NoError(t, err)
	require.Len(t, resp.Sessions, 2)

	// All sessions should have the correct grantee
	for _, s := range resp.Sessions {
		require.Equal(t, grantee, s.Grantee)
	}

	// Query other grantee
	resp, err = qs.SessionsByGrantee(f.ctx, &types.QuerySessionsByGranteeRequest{
		Grantee: otherGrantee,
	})
	require.NoError(t, err)
	require.Len(t, resp.Sessions, 1)
}

func TestQuerySessionsByGranteeNilRequest(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	_, err := qs.SessionsByGrantee(f.ctx, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid request")
}
