package keeper_test

import (
	"fmt"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/vote/types"
)

func TestQueryVoterRegistrations(t *testing.T) {
	t.Run("empty: no registrations", func(t *testing.T) {
		f := initTestFixture(t)

		resp, err := f.queryServer.VoterRegistrations(f.ctx, &types.QueryVoterRegistrationsRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Empty(t, resp.Registrations)
	})

	t.Run("paginated: multiple registrations", func(t *testing.T) {
		f := initTestFixture(t)

		// Create 5 registrations directly in the store.
		for i := 0; i < 5; i++ {
			addr := fmt.Sprintf("voter_%d", i)
			reg := types.VoterRegistration{
				Address:      addr,
				ZkPublicKey:  genZkPubKey(i),
				RegisteredAt: int64(i * 100),
				Active:       true,
			}
			require.NoError(t, f.keeper.VoterRegistration.Set(f.ctx, addr, reg))
		}

		// Page 1: limit 2 with total count
		resp, err := f.queryServer.VoterRegistrations(f.ctx, &types.QueryVoterRegistrationsRequest{
			Pagination: &query.PageRequest{
				Limit:      2,
				CountTotal: true,
			},
		})
		require.NoError(t, err)
		require.Len(t, resp.Registrations, 2)
		require.Equal(t, uint64(5), resp.Pagination.Total)
		require.NotNil(t, resp.Pagination.NextKey)

		// Page 2: use NextKey
		resp2, err := f.queryServer.VoterRegistrations(f.ctx, &types.QueryVoterRegistrationsRequest{
			Pagination: &query.PageRequest{
				Key:   resp.Pagination.NextKey,
				Limit: 2,
			},
		})
		require.NoError(t, err)
		require.Len(t, resp2.Registrations, 2)

		// Page 3: last page
		resp3, err := f.queryServer.VoterRegistrations(f.ctx, &types.QueryVoterRegistrationsRequest{
			Pagination: &query.PageRequest{
				Key:   resp2.Pagination.NextKey,
				Limit: 2,
			},
		})
		require.NoError(t, err)
		require.Len(t, resp3.Registrations, 1)
	})

	t.Run("nil request", func(t *testing.T) {
		f := initTestFixture(t)

		_, err := f.queryServer.VoterRegistrations(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})
}
