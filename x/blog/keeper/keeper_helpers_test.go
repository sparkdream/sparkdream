package keeper_test

import (
	"context"
	"testing"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"

	"github.com/stretchr/testify/require"
)

func TestIsGovAuthority_True(t *testing.T) {
	f := initFixture(t)

	authorityStr, err := f.addressCodec.BytesToString(f.keeper.GetAuthority())
	require.NoError(t, err)

	require.True(t, f.keeper.IsGovAuthority(authorityStr))
}

func TestIsGovAuthority_False(t *testing.T) {
	f := initFixture(t)

	randomAddr, err := f.addressCodec.BytesToString([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20})
	require.NoError(t, err)

	require.False(t, f.keeper.IsGovAuthority(randomAddr))
}

func TestIsGovAuthority_InvalidAddress(t *testing.T) {
	f := initFixture(t)

	require.False(t, f.keeper.IsGovAuthority("not-a-valid-address"))
}

func TestIsCouncilAuthorized_NilCommons_FallsBackToGov(t *testing.T) {
	// initFixture uses nil commonsKeeper
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	authorityStr, err := f.addressCodec.BytesToString(f.keeper.GetAuthority())
	require.NoError(t, err)

	// Gov authority should succeed for operational params (exercises isCouncilAuthorized internally)
	_, err = ms.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{
		Authority:         authorityStr,
		OperationalParams: types.DefaultBlogOperationalParams(),
	})
	require.NoError(t, err)
}

func TestIsCouncilAuthorized_WithCommons(t *testing.T) {
	mock := &mockCommonsKeeper{
		IsCouncilAuthorizedFn: func(ctx context.Context, addr string, council string, committee string) bool {
			return addr == "authorized-addr" && council == "commons" && committee == "operations"
		},
	}
	_, ms, ctx := setupMsgServerWithCommons(t, mock)

	randomAddr := "sprkdrm1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5tzsm5"

	// This address doesn't match the mock's "authorized-addr" check, so it should fail
	_, err := ms.UpdateOperationalParams(ctx, &types.MsgUpdateOperationalParams{
		Authority:         randomAddr,
		OperationalParams: types.DefaultBlogOperationalParams(),
	})
	require.Error(t, err)
}

func TestIsCouncilAuthorized_NilCommons_UnauthorizedFails(t *testing.T) {
	f := initFixture(t) // nil commonsKeeper → falls back to IsGovAuthority
	ms := keeper.NewMsgServerImpl(f.keeper)

	randomAddr, err := f.addressCodec.BytesToString([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20})
	require.NoError(t, err)

	// Random address is NOT gov authority, so should fail
	_, err = ms.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{
		Authority:         randomAddr,
		OperationalParams: types.DefaultBlogOperationalParams(),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not authorized")
}
