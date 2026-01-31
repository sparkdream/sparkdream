package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestQueryMemberByDisplayName(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper
	qs := keeper.NewQueryServerImpl(k)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.MemberByDisplayName(ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("empty display name", func(t *testing.T) {
		_, err := qs.MemberByDisplayName(ctx, &types.QueryMemberByDisplayNameRequest{DisplayName: ""})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("display name not found", func(t *testing.T) {
		_, err := qs.MemberByDisplayName(ctx, &types.QueryMemberByDisplayNameRequest{DisplayName: "NonExistent"})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.NotFound, st.Code())
	})

	t.Run("display name found", func(t *testing.T) {
		member := TestAddrMember1
		memberStr, _ := f.addressCodec.BytesToString(member)

		SetupMemberProfile(t, k, ctx, member, "UniqueDisplayName", "uniqueuser")

		resp, err := qs.MemberByDisplayName(ctx, &types.QueryMemberByDisplayNameRequest{DisplayName: "UniqueDisplayName"})
		require.NoError(t, err)
		require.Equal(t, memberStr, resp.Address)
	})
}
