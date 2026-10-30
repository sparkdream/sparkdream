package keeper_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// FRM-32: the bounty and locked-thread list queries return whole pages and
// follow next_key to the end, each record exactly once.
func TestListQueriesPaginate(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	const n = 5
	want := map[uint64]bool{}
	for i := 0; i < n; i++ {
		post := f.createTestPost(t, testCreator, 0, 0)
		b := f.createTestBounty(t, testCreator, post.PostId, "1000000")
		want[b.Id] = true

		post.Locked = true
		post.LockedBy = testSentinel
		require.NoError(t, f.keeper.Post.Set(f.ctx, post.PostId, post))
		require.NoError(t, f.keeper.LockedRoots.Set(f.ctx, post.PostId))
	}

	drain := func(fetch func(page *query.PageRequest) ([]uint64, *query.PageResponse)) map[uint64]int {
		seen := map[uint64]int{}
		var key []byte
		for pages := 0; ; pages++ {
			require.Less(t, pages, n+1, "pagination does not terminate")
			ids, res := fetch(&query.PageRequest{Key: key, Limit: 2})
			require.LessOrEqual(t, len(ids), 2)
			for _, id := range ids {
				seen[id]++
			}
			if res == nil || len(res.NextKey) == 0 {
				return seen
			}
			key = res.NextKey
		}
	}
	bountyIDs := func(bs []types.Bounty) []uint64 {
		out := make([]uint64, 0, len(bs))
		for _, b := range bs {
			out = append(out, b.Id)
		}
		return out
	}
	assertAll := func(name string, seen map[uint64]int) {
		require.Len(t, seen, n, name)
		for id, c := range seen {
			require.Equal(t, 1, c, "%s: id %d returned %d times", name, id, c)
		}
	}

	active := drain(func(p *query.PageRequest) ([]uint64, *query.PageResponse) {
		resp, err := qs.ActiveBounties(f.ctx, &types.QueryActiveBountiesRequest{Pagination: p})
		require.NoError(t, err)
		if len(resp.Bounties) > 0 {
			require.Equal(t, resp.Bounties[0].Id, resp.BountyId)
		}
		return bountyIDs(resp.Bounties), resp.Pagination
	})
	assertAll("active", active)
	for id := range active {
		require.True(t, want[id])
	}

	assertAll("user", drain(func(p *query.PageRequest) ([]uint64, *query.PageResponse) {
		resp, err := qs.UserBounties(f.ctx, &types.QueryUserBountiesRequest{User: testCreator, Pagination: p})
		require.NoError(t, err)
		return bountyIDs(resp.Bounties), resp.Pagination
	}))

	assertAll("expiring", drain(func(p *query.PageRequest) ([]uint64, *query.PageResponse) {
		resp, err := qs.BountyExpiringSoon(f.ctx, &types.QueryBountyExpiringSoonRequest{WithinSeconds: 86400 * 8, Pagination: p})
		require.NoError(t, err)
		return bountyIDs(resp.Bounties), resp.Pagination
	}))
	// Outside the window: nothing.
	resp, err := qs.BountyExpiringSoon(f.ctx, &types.QueryBountyExpiringSoonRequest{WithinSeconds: 60})
	require.NoError(t, err)
	require.Empty(t, resp.Bounties)

	assertAll("locked", drain(func(p *query.PageRequest) ([]uint64, *query.PageResponse) {
		resp, err := qs.LockedThreads(f.ctx, &types.QueryLockedThreadsRequest{Pagination: p})
		require.NoError(t, err)
		ids := make([]uint64, 0, len(resp.Posts))
		for _, post := range resp.Posts {
			require.True(t, post.Locked)
			ids = append(ids, post.PostId)
		}
		return ids, resp.Pagination
	}))

	// CountTotal on the alias-walking query reports every bounty.
	ub, err := qs.UserBounties(f.ctx, &types.QueryUserBountiesRequest{User: testCreator,
		Pagination: &query.PageRequest{Limit: 2, CountTotal: true}})
	require.NoError(t, err)
	require.Equal(t, uint64(n), ub.Pagination.Total)
	require.Len(t, ub.Bounties, 2)
}
