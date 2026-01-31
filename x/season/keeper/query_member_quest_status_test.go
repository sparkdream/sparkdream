package keeper_test

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestQueryMemberQuestStatus(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper
	qs := keeper.NewQueryServerImpl(k)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.MemberQuestStatus(ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("empty member address", func(t *testing.T) {
		_, err := qs.MemberQuestStatus(ctx, &types.QueryMemberQuestStatusRequest{Member: "", QuestId: "quest1"})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("empty quest id", func(t *testing.T) {
		member := TestAddrMember1
		memberStr, _ := f.addressCodec.BytesToString(member)

		_, err := qs.MemberQuestStatus(ctx, &types.QueryMemberQuestStatusRequest{Member: memberStr, QuestId: ""})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("quest not found", func(t *testing.T) {
		member := TestAddrMember2
		memberStr, _ := f.addressCodec.BytesToString(member)

		_, err := qs.MemberQuestStatus(ctx, &types.QueryMemberQuestStatusRequest{Member: memberStr, QuestId: "nonexistent"})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.NotFound, st.Code())
	})

	t.Run("quest not started by member", func(t *testing.T) {
		member := TestAddrMember2
		memberStr, _ := f.addressCodec.BytesToString(member)

		// Setup the quest first
		SetupQuest(t, k, ctx, "unstarted_quest", 50, true)

		resp, err := qs.MemberQuestStatus(ctx, &types.QueryMemberQuestStatusRequest{Member: memberStr, QuestId: "unstarted_quest"})
		require.NoError(t, err)
		require.False(t, resp.Completed)
		require.Equal(t, int64(0), resp.CompletedBlock)
	})

	t.Run("quest in progress", func(t *testing.T) {
		member := TestAddrMember3
		memberStr, _ := f.addressCodec.BytesToString(member)

		// Setup the quest first
		SetupQuest(t, k, ctx, "progress_quest", 75, true)

		// Create quest progress
		key := fmt.Sprintf("%s:%s", memberStr, "progress_quest")
		progress := types.MemberQuestProgress{
			MemberQuest:       key,
			ObjectiveProgress: []uint64{5},
			Completed:         false,
		}
		k.MemberQuestProgress.Set(ctx, key, progress)

		resp, err := qs.MemberQuestStatus(ctx, &types.QueryMemberQuestStatusRequest{Member: memberStr, QuestId: "progress_quest"})
		require.NoError(t, err)
		require.False(t, resp.Completed)
	})

	t.Run("quest completed", func(t *testing.T) {
		member := TestAddrCreator
		memberStr, _ := f.addressCodec.BytesToString(member)

		// Setup the quest first
		SetupQuest(t, k, ctx, "completed_quest", 100, true)

		// Create completed quest progress
		key := fmt.Sprintf("%s:%s", memberStr, "completed_quest")
		progress := types.MemberQuestProgress{
			MemberQuest:    key,
			Completed:      true,
			CompletedBlock: 1000,
		}
		k.MemberQuestProgress.Set(ctx, key, progress)

		resp, err := qs.MemberQuestStatus(ctx, &types.QueryMemberQuestStatusRequest{Member: memberStr, QuestId: "completed_quest"})
		require.NoError(t, err)
		require.True(t, resp.Completed)
	})
}
