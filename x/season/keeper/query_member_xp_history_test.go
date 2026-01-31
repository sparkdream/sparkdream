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

func TestQueryMemberXpHistory(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper
	qs := keeper.NewQueryServerImpl(k)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.MemberXpHistory(ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("empty member address", func(t *testing.T) {
		_, err := qs.MemberXpHistory(ctx, &types.QueryMemberXpHistoryRequest{Address: ""})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("member with no xp history", func(t *testing.T) {
		member := TestAddrMember1
		memberStr, _ := f.addressCodec.BytesToString(member)

		resp, err := qs.MemberXpHistory(ctx, &types.QueryMemberXpHistoryRequest{Address: memberStr})
		require.NoError(t, err)
		require.Equal(t, int64(0), resp.Epoch)
		require.Equal(t, uint64(0), resp.XpEarned)
	})

	t.Run("member with xp history", func(t *testing.T) {
		member := TestAddrMember2
		memberStr, _ := f.addressCodec.BytesToString(member)

		// Create XP tracker record
		// Key format is "member:epoch"
		tracker := types.EpochXpTracker{
			MemberEpoch:   memberStr + ":5",
			VoteXpEarned:  100,
			ForumXpEarned: 50,
			QuestXpEarned: 100,
			OtherXpEarned: 0,
		}
		key := memberStr + ":5"
		k.EpochXpTracker.Set(ctx, key, tracker)

		resp, err := qs.MemberXpHistory(ctx, &types.QueryMemberXpHistoryRequest{Address: memberStr})
		require.NoError(t, err)
		require.Equal(t, int64(5), resp.Epoch)
		require.Equal(t, uint64(250), resp.XpEarned)
	})
}
