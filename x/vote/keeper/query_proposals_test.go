package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/vote/types"
)

func TestQueryProposals(t *testing.T) {
	t.Run("empty: no proposals returns empty list", func(t *testing.T) {
		f := initTestFixture(t)
		resp, err := f.queryServer.Proposals(f.ctx, &types.QueryProposalsRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Empty(t, resp.Proposals)
	})

	t.Run("paginated: multiple proposals", func(t *testing.T) {
		f := initTestFixture(t)

		// Create 5 proposals directly in the store.
		for i := uint64(0); i < 5; i++ {
			p := types.VotingProposal{
				Id:            i,
				Title:         "Proposal",
				Proposer:      f.member,
				Status:        types.ProposalStatus_PROPOSAL_STATUS_ACTIVE,
				Quorum:        math.LegacyNewDec(0),
				Threshold:     math.LegacyNewDec(0),
				VetoThreshold: math.LegacyNewDec(0),
			}
			err := f.keeper.VotingProposal.Set(f.ctx, i, p)
			require.NoError(t, err)
		}

		// Page 1: limit 2
		resp, err := f.queryServer.Proposals(f.ctx, &types.QueryProposalsRequest{
			Pagination: &query.PageRequest{
				Limit:      2,
				CountTotal: true,
			},
		})
		require.NoError(t, err)
		require.Len(t, resp.Proposals, 2)
		require.Equal(t, uint64(5), resp.Pagination.Total)
		require.NotNil(t, resp.Pagination.NextKey)

		// Page 2: use NextKey
		resp2, err := f.queryServer.Proposals(f.ctx, &types.QueryProposalsRequest{
			Pagination: &query.PageRequest{
				Key:   resp.Pagination.NextKey,
				Limit: 2,
			},
		})
		require.NoError(t, err)
		require.Len(t, resp2.Proposals, 2)

		// Page 3: last page
		resp3, err := f.queryServer.Proposals(f.ctx, &types.QueryProposalsRequest{
			Pagination: &query.PageRequest{
				Key:   resp2.Pagination.NextKey,
				Limit: 2,
			},
		})
		require.NoError(t, err)
		require.Len(t, resp3.Proposals, 1)
	})

	t.Run("nil request", func(t *testing.T) {
		f := initTestFixture(t)
		_, err := f.queryServer.Proposals(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})
}
