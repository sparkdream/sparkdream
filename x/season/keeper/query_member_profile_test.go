package keeper_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func createNMemberProfile(keeper keeper.Keeper, ctx context.Context, n int) []types.MemberProfile {
	items := make([]types.MemberProfile, n)
	for i := range items {
		items[i].Address = strconv.Itoa(i)
		items[i].DisplayName = strconv.Itoa(i)
		items[i].Username = strconv.Itoa(i)
		items[i].DisplayTitle = strconv.Itoa(i)
		items[i].SeasonXp = uint64(i)
		items[i].SeasonLevel = uint64(i)
		items[i].LifetimeXp = uint64(i)
		items[i].GuildId = uint64(i)
		items[i].LastDisplayNameChangeEpoch = int64(i)
		items[i].LastUsernameChangeEpoch = int64(i)
		items[i].ChallengesWon = uint64(i)
		items[i].JuryDutiesCompleted = uint64(i)
		items[i].VotesCast = uint64(i)
		items[i].ForumHelpfulCount = uint64(i)
		items[i].InvitationsSuccessful = uint64(i)
		items[i].LastActiveEpoch = int64(i)
		_ = keeper.MemberProfile.Set(ctx, items[i].Address, items[i])
	}
	return items
}

func TestMemberProfileQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNMemberProfile(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetMemberProfileRequest
		response *types.QueryGetMemberProfileResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetMemberProfileRequest{
				Address: msgs[0].Address,
			},
			response: &types.QueryGetMemberProfileResponse{MemberProfile: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetMemberProfileRequest{
				Address: msgs[1].Address,
			},
			response: &types.QueryGetMemberProfileResponse{MemberProfile: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetMemberProfileRequest{
				Address: strconv.Itoa(100000),
			},
			err: status.Error(codes.NotFound, "not found"),
		},
		{
			desc: "InvalidRequest",
			err:  status.Error(codes.InvalidArgument, "invalid request"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			response, err := qs.GetMemberProfile(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestMemberProfileQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNMemberProfile(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllMemberProfileRequest {
		return &types.QueryAllMemberProfileRequest{
			Pagination: &query.PageRequest{
				Key:        next,
				Offset:     offset,
				Limit:      limit,
				CountTotal: total,
			},
		}
	}
	t.Run("ByOffset", func(t *testing.T) {
		step := 2
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListMemberProfile(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.MemberProfile), step)
			require.Subset(t, msgs, resp.MemberProfile)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListMemberProfile(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.MemberProfile), step)
			require.Subset(t, msgs, resp.MemberProfile)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListMemberProfile(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.MemberProfile)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListMemberProfile(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
