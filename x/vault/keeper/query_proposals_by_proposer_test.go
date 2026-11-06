package keeper_test

import (
	"cosmossdk.io/collections"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/vault/keeper"
	"sparkdream/x/vault/types"
)

func TestProposalsByProposer(t *testing.T) {
	f := initFixture(t)
	rep := f.withRep()
	qs := keeper.NewQueryServerImpl(f.keeper)
	alice, oldAlice, bob := addr("alice"), addr("old-alice"), addr("bob")
	rep.canonicalOf[oldAlice] = alice
	pending, executed := types.ProposalStatus_PROPOSAL_STATUS_PENDING, types.ProposalStatus_PROPOSAL_STATUS_EXECUTED
	for _, p := range []types.VaultProposal{
		{ProposalId: 1, VaultId: 1, Proposer: alice, Status: pending},
		{ProposalId: 2, VaultId: 2, Proposer: oldAlice, Status: executed},
		{ProposalId: 3, VaultId: 1, Proposer: bob, Status: pending},
	} {
		require.NoError(t, f.keeper.VaultProposal.Set(f.ctx, p.ProposalId, p))
		require.NoError(t, f.keeper.ProposalsByProposer.Set(f.ctx, collections.Join(p.Proposer, p.ProposalId)))
		require.NoError(t, f.keeper.ProposalsByVault.Set(f.ctx, collections.Join(p.VaultId, p.ProposalId)))
	}
	ids := func(ps []types.VaultProposal) []uint64 {
		out := []uint64{}
		for _, p := range ps {
			out = append(out, p.ProposalId)
		}
		return out
	}

	// Across vaults and aliases.
	resp, err := qs.ProposalsByProposer(f.ctx, &types.QueryProposalsByProposerRequest{Proposer: alice})
	require.NoError(t, err)
	require.ElementsMatch(t, []uint64{1, 2}, ids(resp.Proposals))

	resp, err = qs.ProposalsByProposer(f.ctx, &types.QueryProposalsByProposerRequest{Proposer: alice, Status: pending})
	require.NoError(t, err)
	require.Equal(t, []uint64{1}, ids(resp.Proposals))

	_, err = qs.ProposalsByProposer(f.ctx, &types.QueryProposalsByProposerRequest{Proposer: "not-an-address"})
	require.Error(t, err)
}
