package keeper_test

import (
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

func signInGrant(granter, grantee, service string, expiresAt time.Time) *types.MsgCreateGrant {
	return &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   grantee,
		ExpiresAt: expiresAt,
		Note:      "post from my desktop",
		Payload:   &types.MsgCreateGrant_SignIn{SignIn: &types.SignInPayload{Service: service}},
	}
}

func TestCreateGrant_SignIn_HappyPath(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	qs := keeper.NewQueryServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	// past the session key's 7-day max_expiration: sign-in grants are
	// bounded by max_grant_lifetime_seconds only
	exp := sdkCtx.BlockTime().Add(90 * 24 * time.Hour)

	resp, err := ms.CreateGrant(f.ctx, signInGrant(granter, grantee, "mstdn.sparkdream.io", exp))
	require.NoError(t, err)

	g, err := f.keeper.Grants.Get(f.ctx, resp.GrantId)
	require.NoError(t, err)
	require.Equal(t, types.GrantType_GRANT_TYPE_SIGN_IN, g.Type)
	require.Equal(t, types.GrantStatus_GRANT_STATUS_ACTIVE, g.Status)
	require.Equal(t, "mstdn.sparkdream.io", g.GetSignIn().GetService())

	count, err := f.keeper.CountActiveGrants(f.ctx, granter, types.GrantType_GRANT_TYPE_SIGN_IN)
	require.NoError(t, err)
	require.Equal(t, uint32(1), count)

	// what a sign-in service asks: the grantee's SIGN_IN grants
	byGrantee, err := qs.GrantsByGrantee(f.ctx, &types.QueryGrantsByGranteeRequest{
		Grantee: grantee,
		Type:    types.GrantType_GRANT_TYPE_SIGN_IN,
	})
	require.NoError(t, err)
	require.Len(t, byGrantee.Grants, 1)
	require.Equal(t, granter, byGrantee.Grants[0].Granter)

	// no session-key side effects: the pair stays free for a session key
	has, err := f.keeper.SessionKeyByPair.Has(f.ctx, collections.Join(granter, grantee))
	require.NoError(t, err)
	require.False(t, has)
}

func TestCreateGrant_SignIn_InvalidService(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	exp := sdkCtx.BlockTime().Add(24 * time.Hour)

	for _, svc := range []string{
		"",
		"localhost",
		"Mstdn.sparkdream.io",
		"mstdn.sparkdream.io.",
		"https://mstdn.sparkdream.io",
		"mstdn.sparkdream.io:443",
		"-mstdn.sparkdream.io",
		"mstdn..sparkdream.io",
		"mstdn_x.sparkdream.io",
	} {
		_, err := ms.CreateGrant(f.ctx, signInGrant(granter, grantee, svc, exp))
		require.ErrorIs(t, err, types.ErrInvalidSignInService, "service %q", svc)
	}

	_, err := ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   grantee,
		ExpiresAt: exp,
		Payload:   &types.MsgCreateGrant_SignIn{},
	})
	require.ErrorIs(t, err, types.ErrInvalidPayload)
}

func TestCreateGrant_SignIn_Duplicates(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	other := testAddr("other", f.addressCodec)
	exp := sdkCtx.BlockTime().Add(24 * time.Hour)

	first, err := ms.CreateGrant(f.ctx, signInGrant(granter, grantee, "mstdn.sparkdream.io", exp))
	require.NoError(t, err)

	_, err = ms.CreateGrant(f.ctx, signInGrant(granter, grantee, "mstdn.sparkdream.io", exp))
	require.ErrorIs(t, err, types.ErrSignInGrantExists)

	// another service, or another grantee, is a separate grant
	_, err = ms.CreateGrant(f.ctx, signInGrant(granter, grantee, "social.sparkdream.io", exp))
	require.NoError(t, err)
	_, err = ms.CreateGrant(f.ctx, signInGrant(granter, other, "mstdn.sparkdream.io", exp))
	require.NoError(t, err)

	// once revoked, the same grant can be made again
	_, err = ms.RevokeGrant(f.ctx, &types.MsgRevokeGrant{Granter: granter, GrantId: first.GrantId})
	require.NoError(t, err)
	_, err = ms.CreateGrant(f.ctx, signInGrant(granter, grantee, "mstdn.sparkdream.io", exp))
	require.NoError(t, err)
}

func TestCreateGrant_SignIn_PerGranterCap(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	exp := sdkCtx.BlockTime().Add(24 * time.Hour)

	for i := 0; i < types.MaxSignInGrantsPerGranter; i++ {
		grantee := testAddr(fmt.Sprintf("grantee-%d", i), f.addressCodec)
		_, err := ms.CreateGrant(f.ctx, signInGrant(granter, grantee, "mstdn.sparkdream.io", exp))
		require.NoError(t, err)
	}
	_, err := ms.CreateGrant(f.ctx, signInGrant(granter, testAddr("one-more", f.addressCodec), "mstdn.sparkdream.io", exp))
	require.ErrorIs(t, err, types.ErrMaxSignInGrantsExceeded)
}

func TestCreateGrant_SignIn_LifetimeCap(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	tooLong := sdkCtx.BlockTime().Add(time.Duration(params.MaxGrantLifetimeSeconds+1) * time.Second)

	_, err = ms.CreateGrant(f.ctx, signInGrant(
		testAddr("granter", f.addressCodec), testAddr("grantee", f.addressCodec), "mstdn.sparkdream.io", tooLong))
	require.ErrorIs(t, err, types.ErrGrantLifetimeTooLong)
}

func TestSignInGrant_DeclineAndExpire(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	exp := sdkCtx.BlockTime().Add(24 * time.Hour)

	declined, err := ms.CreateGrant(f.ctx, signInGrant(granter, grantee, "mstdn.sparkdream.io", exp))
	require.NoError(t, err)
	_, err = ms.DeclineGrant(f.ctx, &types.MsgDeclineGrant{Grantee: grantee, GrantId: declined.GrantId})
	require.NoError(t, err)
	_, err = f.keeper.Grants.Get(f.ctx, declined.GrantId)
	require.Error(t, err)

	expiring, err := ms.CreateGrant(f.ctx, signInGrant(granter, grantee, "mstdn.sparkdream.io", exp))
	require.NoError(t, err)
	require.NoError(t, f.keeper.EndBlocker(sdkCtx.WithBlockTime(exp.Add(time.Second))))
	_, err = f.keeper.Grants.Get(f.ctx, expiring.GrantId)
	require.Error(t, err)

	count, err := f.keeper.CountActiveGrants(f.ctx, granter, types.GrantType_GRANT_TYPE_SIGN_IN)
	require.NoError(t, err)
	require.Equal(t, uint32(0), count)
}
