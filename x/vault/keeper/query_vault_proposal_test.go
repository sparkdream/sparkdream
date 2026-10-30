package keeper_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/vault/keeper"
	"sparkdream/x/vault/types"
)

func createNVaultProposal(keeper keeper.Keeper, ctx context.Context, n int) []types.VaultProposal {
	items := make([]types.VaultProposal, n)
	for i := range items {
		items[i].ProposalId = uint64(i)
		items[i].VaultId = uint64(i)
		items[i].Proposer = strconv.Itoa(i)
		items[i].ConfigVersion = uint64(i)
		items[i].ExpiresAt = int64(i)
		items[i].Memo = strconv.Itoa(i)
		if err := keeper.VaultProposal.Set(ctx, items[i].ProposalId, items[i]); err != nil {
			panic(err)
		}
	}
	return items
}

func TestVaultProposalQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNVaultProposal(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetVaultProposalRequest
		response *types.QueryGetVaultProposalResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetVaultProposalRequest{
				ProposalId: msgs[0].ProposalId,
			},
			response: &types.QueryGetVaultProposalResponse{VaultProposal: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetVaultProposalRequest{
				ProposalId: msgs[1].ProposalId,
			},
			response: &types.QueryGetVaultProposalResponse{VaultProposal: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetVaultProposalRequest{
				ProposalId: 100000,
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
			response, err := qs.GetVaultProposal(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestVaultProposalQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNVaultProposal(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllVaultProposalRequest {
		return &types.QueryAllVaultProposalRequest{
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
			resp, err := qs.ListVaultProposal(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.VaultProposal), step)
			require.Subset(t, msgs, resp.VaultProposal)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListVaultProposal(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.VaultProposal), step)
			require.Subset(t, msgs, resp.VaultProposal)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListVaultProposal(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.VaultProposal)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListVaultProposal(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
