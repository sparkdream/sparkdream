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

func TestQueryAvailableQuests(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper
	qs := keeper.NewQueryServerImpl(k)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.AvailableQuests(ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("empty member address", func(t *testing.T) {
		_, err := qs.AvailableQuests(ctx, &types.QueryAvailableQuestsRequest{Member: ""})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("no quests available", func(t *testing.T) {
		member := TestAddrMember1
		memberStr, _ := f.addressCodec.BytesToString(member)

		resp, err := qs.AvailableQuests(ctx, &types.QueryAvailableQuestsRequest{Member: memberStr})
		require.NoError(t, err)
		require.Empty(t, resp.Id)
	})

	t.Run("quest available for member", func(t *testing.T) {
		member := TestAddrMember2
		memberStr, _ := f.addressCodec.BytesToString(member)

		SetupBasicMemberProfile(t, k, ctx, member)
		SetupQuest(t, k, ctx, "available_quest", 75, true)

		resp, err := qs.AvailableQuests(ctx, &types.QueryAvailableQuestsRequest{Member: memberStr})
		require.NoError(t, err)
		require.Equal(t, "available_quest", resp.Id)
		require.Equal(t, uint64(75), resp.XpReward)
	})

	t.Run("inactive quest not available", func(t *testing.T) {
		member := TestAddrMember3
		memberStr, _ := f.addressCodec.BytesToString(member)

		SetupBasicMemberProfile(t, k, ctx, member)

		// Create an inactive quest
		quest := types.Quest{
			QuestId:  "inactive_test",
			Name:     "Inactive Quest",
			XpReward: 50,
			Active:   false, // Inactive
		}
		k.Quest.Set(ctx, "inactive_test", quest)

		resp, err := qs.AvailableQuests(ctx, &types.QueryAvailableQuestsRequest{Member: memberStr})
		require.NoError(t, err)
		// Should not return the inactive quest
		require.NotEqual(t, "inactive_test", resp.Id)
	})
}
