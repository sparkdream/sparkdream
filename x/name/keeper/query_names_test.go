package keeper_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"

	"cosmossdk.io/collections"
)

// Helper to populate names and the secondary index for a specific owner
func createNamesForOwner(k keeper.Keeper, ctx context.Context, owner string, n int) []types.NameRecord {
	items := make([]types.NameRecord, n)
	for i := range items {
		// Unique name format: "owner-i"
		name := owner + "-" + strconv.Itoa(i)
		items[i] = types.NameRecord{
			Name:  name,
			Owner: owner,
			Data:  "metadata-" + strconv.Itoa(i),
		}

		// 1. Set Primary Store
		_ = k.Names.Set(ctx, name, items[i])

		// 2. Set Secondary Index (Crucial for the query to work)
		_ = k.OwnerNames.Set(ctx, collections.Join(owner, name))
	}
	return items
}

func TestNamesQuery(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	ownerAlice := "cosmos1alice"
	ownerBob := "cosmos1bob"

	// Create 5 names for Alice
	aliceNames := createNamesForOwner(f.keeper, f.ctx, ownerAlice, 5)

	// Create 3 names for Bob (Noise data to ensure isolation)
	createNamesForOwner(f.keeper, f.ctx, ownerBob, 3)

	tests := []struct {
		desc      string
		request   *types.QueryNamesRequest
		checkResp func(t *testing.T, resp *types.QueryNamesResponse)
		err       error
	}{
		{
			desc: "Success - Fetch All Alice Names",
			request: &types.QueryNamesRequest{
				Address: ownerAlice,
			},
			checkResp: func(t *testing.T, resp *types.QueryNamesResponse) {
				require.Len(t, resp.Names, 5)
				require.ElementsMatch(t, aliceNames, resp.Names)
			},
		},
		{
			desc: "Success - Pagination (Limit 2)",
			request: &types.QueryNamesRequest{
				Address: ownerAlice,
				Pagination: &query.PageRequest{
					Limit: 2,
				},
			},
			checkResp: func(t *testing.T, resp *types.QueryNamesResponse) {
				require.Len(t, resp.Names, 2)
			},
		},
		{
			desc: "Success - User With No Names",
			request: &types.QueryNamesRequest{
				Address: "cosmos1empty",
			},
			checkResp: func(t *testing.T, resp *types.QueryNamesResponse) {
				require.Len(t, resp.Names, 0)
			},
		},
		{
			desc: "Failure - Empty Address",
			request: &types.QueryNamesRequest{
				Address: "",
			},
			err: status.Error(codes.InvalidArgument, "address cannot be empty"),
		},
		{
			desc:    "Failure - Nil Request",
			request: nil,
			err:     status.Error(codes.InvalidArgument, "invalid request"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			resp, err := qs.Names(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				tc.checkResp(t, resp)
			}
		})
	}
}
