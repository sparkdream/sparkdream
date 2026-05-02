package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

func TestGetOwnerInfo_ExistingRecord(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("alice_test_account_1")).String()
	require.NoError(t, f.keeper.Owners.Set(f.ctx, addr, types.OwnerInfo{
		Address:        addr,
		PrimaryName:    "alice",
		DisplayName:    "Alice the Great",
		LastActiveTime: 1234567890,
	}))

	resp, err := qs.GetOwnerInfo(f.ctx, &types.QueryGetOwnerInfoRequest{Address: addr})
	require.NoError(t, err)
	require.Equal(t, addr, resp.OwnerInfo.Address)
	require.Equal(t, "alice", resp.OwnerInfo.PrimaryName)
	require.Equal(t, "Alice the Great", resp.OwnerInfo.DisplayName)
	require.Equal(t, int64(1234567890), resp.OwnerInfo.LastActiveTime)
}

func TestGetOwnerInfo_NoRecordReturnsEmpty(t *testing.T) {
	// An address that has never registered a handle still gets a usable
	// response (echo address, all other fields zero) — frontend can render
	// "no display name" without branching on NotFound.
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("ghost_unknown_______")).String()

	resp, err := qs.GetOwnerInfo(f.ctx, &types.QueryGetOwnerInfoRequest{Address: addr})
	require.NoError(t, err)
	require.Equal(t, addr, resp.OwnerInfo.Address)
	require.Equal(t, "", resp.OwnerInfo.PrimaryName)
	require.Equal(t, "", resp.OwnerInfo.DisplayName)
	require.Equal(t, int64(0), resp.OwnerInfo.LastActiveTime)
}

func TestGetOwnerInfo_EmptyAddress(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	_, err := qs.GetOwnerInfo(f.ctx, &types.QueryGetOwnerInfoRequest{Address: ""})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestGetOwnerInfo_NilRequest(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	_, err := qs.GetOwnerInfo(f.ctx, nil)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}
