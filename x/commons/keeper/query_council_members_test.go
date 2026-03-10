package keeper_test

import (
	"testing"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestQueryGetCouncilMembers_Success(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	qs := keeper.NewQueryServerImpl(k)

	councilName := "MembersCouncil"
	member1 := sdk.AccAddress([]byte("member1_query_______")).String()
	member2 := sdk.AccAddress([]byte("member2_query_______")).String()

	require.NoError(t, k.AddMember(ctx, councilName, types.Member{Address: member1, Weight: "1"}))
	require.NoError(t, k.AddMember(ctx, councilName, types.Member{Address: member2, Weight: "2"}))

	resp, err := qs.GetCouncilMembers(ctx, &types.QueryGetCouncilMembersRequest{CouncilName: councilName})
	require.NoError(t, err)
	require.Len(t, resp.Members, 2)

	// Verify member data
	weights := map[string]string{}
	for _, m := range resp.Members {
		weights[m.Address] = m.Weight
	}
	require.Equal(t, "1", weights[member1])
	require.Equal(t, "2", weights[member2])
}

func TestQueryGetCouncilMembers_Empty(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	qs := keeper.NewQueryServerImpl(k)

	resp, err := qs.GetCouncilMembers(ctx, &types.QueryGetCouncilMembersRequest{CouncilName: "nonexistent"})
	// Empty council returns empty members, not error (since Walk returns nil for no results)
	require.NoError(t, err)
	require.Empty(t, resp.Members)
}

func TestQueryGetCouncilMembers_NilRequest(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	qs := keeper.NewQueryServerImpl(k)

	_, err := qs.GetCouncilMembers(ctx, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty request")
}

func TestQueryGetCouncilMembers_IsolatedByCouncil(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	qs := keeper.NewQueryServerImpl(k)

	member1 := sdk.AccAddress([]byte("member1_isolated____")).String()
	member2 := sdk.AccAddress([]byte("member2_isolated____")).String()

	require.NoError(t, k.AddMember(ctx, "CouncilA", types.Member{Address: member1, Weight: "1"}))
	require.NoError(t, k.AddMember(ctx, "CouncilB", types.Member{Address: member2, Weight: "1"}))

	respA, err := qs.GetCouncilMembers(ctx, &types.QueryGetCouncilMembersRequest{CouncilName: "CouncilA"})
	require.NoError(t, err)
	require.Len(t, respA.Members, 1)
	require.Equal(t, member1, respA.Members[0].Address)

	respB, err := qs.GetCouncilMembers(ctx, &types.QueryGetCouncilMembersRequest{CouncilName: "CouncilB"})
	require.NoError(t, err)
	require.Len(t, respB.Members, 1)
	require.Equal(t, member2, respB.Members[0].Address)
}
