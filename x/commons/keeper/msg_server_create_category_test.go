package keeper_test

import (
	"strings"
	"testing"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestCreateCategory_Success_AsAuthority(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	resp, err := msgServer.CreateCategory(ctx, &types.MsgCreateCategory{
		Creator:          k.GetAuthorityString(),
		Title:            "General",
		Description:      "general discussion",
		MembersOnlyWrite: true,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	stored, ok := k.GetCategory(ctx, resp.CategoryId)
	require.True(t, ok)
	require.Equal(t, "General", stored.Title)
	require.Equal(t, "general discussion", stored.Description)
	require.True(t, stored.MembersOnlyWrite)
	require.False(t, stored.AdminOnlyWrite)
}

func TestCreateCategory_Success_AsOperationsCommittee(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	member := sdk.AccAddress([]byte("ops_committee_mbr___")).String()
	require.NoError(t, k.AddMember(ctx, "Commons Operations Committee", types.Member{
		Address: member, Weight: "1",
	}))

	resp, err := msgServer.CreateCategory(ctx, &types.MsgCreateCategory{
		Creator: member,
		Title:   "Ops",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestCreateCategory_InvalidCreator(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	_, err := msgServer.CreateCategory(ctx, &types.MsgCreateCategory{
		Creator: "not-a-bech32-address",
		Title:   "x",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid creator address")
}

func TestCreateCategory_Unauthorized(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	random := sdk.AccAddress([]byte("random_creator______")).String()

	_, err := msgServer.CreateCategory(ctx, &types.MsgCreateCategory{
		Creator: random,
		Title:   "x",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "only governance")
}

func TestCreateCategory_EmptyTitle(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	_, err := msgServer.CreateCategory(ctx, &types.MsgCreateCategory{
		Creator: k.GetAuthorityString(),
		Title:   "",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "title cannot be empty")
}

func TestCreateCategory_TitleTooLong(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	_, err := msgServer.CreateCategory(ctx, &types.MsgCreateCategory{
		Creator: k.GetAuthorityString(),
		Title:   strings.Repeat("x", 257),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "title exceeds 256 characters")
}

func TestCreateCategory_DescriptionTooLong(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	_, err := msgServer.CreateCategory(ctx, &types.MsgCreateCategory{
		Creator:     k.GetAuthorityString(),
		Title:       "Ok",
		Description: strings.Repeat("x", 2049),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "description exceeds 2048 characters")
}

func TestCreateCategory_SequentialIDs(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	authority := k.GetAuthorityString()

	first, err := msgServer.CreateCategory(ctx, &types.MsgCreateCategory{
		Creator: authority, Title: "A",
	})
	require.NoError(t, err)

	second, err := msgServer.CreateCategory(ctx, &types.MsgCreateCategory{
		Creator: authority, Title: "B",
	})
	require.NoError(t, err)

	require.Equal(t, first.CategoryId+1, second.CategoryId)
}
