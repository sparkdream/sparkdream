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

func TestQueryQuestChain(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper
	qs := keeper.NewQueryServerImpl(k)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.QuestChain(ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("empty quest chain", func(t *testing.T) {
		_, err := qs.QuestChain(ctx, &types.QueryQuestChainRequest{QuestChain: ""})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("chain not found", func(t *testing.T) {
		resp, err := qs.QuestChain(ctx, &types.QueryQuestChainRequest{QuestChain: "nonexistent"})
		require.NoError(t, err)
		require.Empty(t, resp.QuestId)
	})

	t.Run("chain with quests", func(t *testing.T) {
		// Create quests in a chain
		quest1 := types.Quest{
			QuestId:    "chain_quest_1",
			Name:       "First Quest",
			QuestChain: "tutorial_chain",
			XpReward:   25,
			Active:     true,
		}
		quest2 := types.Quest{
			QuestId:           "chain_quest_2",
			Name:              "Second Quest",
			QuestChain:        "tutorial_chain",
			PrerequisiteQuest: "chain_quest_1",
			XpReward:          50,
			Active:            true,
		}
		k.Quest.Set(ctx, "chain_quest_1", quest1)
		k.Quest.Set(ctx, "chain_quest_2", quest2)

		resp, err := qs.QuestChain(ctx, &types.QueryQuestChainRequest{QuestChain: "tutorial_chain"})
		require.NoError(t, err)
		require.NotEmpty(t, resp.QuestId)
	})
}
