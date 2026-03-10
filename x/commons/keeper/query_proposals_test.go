package keeper_test

import (
	"testing"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestQueryGetProposal_Success(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	qs := keeper.NewQueryServerImpl(k)

	councilName := "QueryCouncil"
	voter := sdk.AccAddress([]byte("voter_query_________")).String()
	require.NoError(t, k.AddMember(ctx, councilName, types.Member{Address: voter, Weight: "3"}))

	proposalID := uint64(1)
	require.NoError(t, k.Proposals.Set(ctx, proposalID, types.Proposal{
		Id: proposalID, CouncilName: councilName, PolicyAddress: "policy1",
		Status: types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED, Metadata: "test",
	}))
	require.NoError(t, k.Votes.Set(ctx, collections.Join(proposalID, voter), types.Vote{
		Voter: voter, Option: types.VoteOption_VOTE_OPTION_YES,
	}))

	resp, err := qs.GetProposal(ctx, &types.QueryGetProposalRequest{ProposalId: proposalID})
	require.NoError(t, err)
	require.Equal(t, proposalID, resp.Proposal.Id)
	require.Equal(t, "test", resp.Proposal.Metadata)
	require.Len(t, resp.Votes, 1)
	require.Equal(t, voter, resp.Votes[0].Voter)
	require.Equal(t, "3.000000000000000000", resp.Tally.YesWeight)
}

func TestQueryGetProposal_NotFound(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	qs := keeper.NewQueryServerImpl(k)

	_, err := qs.GetProposal(ctx, &types.QueryGetProposalRequest{ProposalId: 999})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestQueryGetProposal_NilRequest(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	qs := keeper.NewQueryServerImpl(k)

	_, err := qs.GetProposal(ctx, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty request")
}

func TestQueryListProposals_All(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	qs := keeper.NewQueryServerImpl(k)

	require.NoError(t, k.Proposals.Set(ctx, 1, types.Proposal{Id: 1, CouncilName: "A"}))
	require.NoError(t, k.Proposals.Set(ctx, 2, types.Proposal{Id: 2, CouncilName: "B"}))
	require.NoError(t, k.Proposals.Set(ctx, 3, types.Proposal{Id: 3, CouncilName: "A"}))

	resp, err := qs.ListProposals(ctx, &types.QueryListProposalsRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Proposals, 3)
}

func TestQueryListProposals_FilterByCouncil(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	qs := keeper.NewQueryServerImpl(k)

	require.NoError(t, k.Proposals.Set(ctx, 1, types.Proposal{Id: 1, CouncilName: "Alpha"}))
	require.NoError(t, k.ProposalsByCouncil.Set(ctx, collections.Join("Alpha", uint64(1))))
	require.NoError(t, k.Proposals.Set(ctx, 2, types.Proposal{Id: 2, CouncilName: "Beta"}))
	require.NoError(t, k.ProposalsByCouncil.Set(ctx, collections.Join("Beta", uint64(2))))
	require.NoError(t, k.Proposals.Set(ctx, 3, types.Proposal{Id: 3, CouncilName: "Alpha"}))
	require.NoError(t, k.ProposalsByCouncil.Set(ctx, collections.Join("Alpha", uint64(3))))

	resp, err := qs.ListProposals(ctx, &types.QueryListProposalsRequest{CouncilName: "Alpha"})
	require.NoError(t, err)
	require.Len(t, resp.Proposals, 2)
	for _, p := range resp.Proposals {
		require.Equal(t, "Alpha", p.CouncilName)
	}
}

func TestQueryListProposals_NilRequest(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	qs := keeper.NewQueryServerImpl(k)

	_, err := qs.ListProposals(ctx, nil)
	require.Error(t, err)
}

func TestQueryGetProposalVotes_Success(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	qs := keeper.NewQueryServerImpl(k)

	councilName := "VotesQueryCouncil"
	voter1 := sdk.AccAddress([]byte("voter1_vq___________")).String()
	voter2 := sdk.AccAddress([]byte("voter2_vq___________")).String()

	require.NoError(t, k.AddMember(ctx, councilName, types.Member{Address: voter1, Weight: "2"}))
	require.NoError(t, k.AddMember(ctx, councilName, types.Member{Address: voter2, Weight: "1"}))

	proposalID := uint64(1)
	require.NoError(t, k.Proposals.Set(ctx, proposalID, types.Proposal{
		Id: proposalID, CouncilName: councilName,
	}))
	require.NoError(t, k.Votes.Set(ctx, collections.Join(proposalID, voter1), types.Vote{
		Voter: voter1, Option: types.VoteOption_VOTE_OPTION_YES,
	}))
	require.NoError(t, k.Votes.Set(ctx, collections.Join(proposalID, voter2), types.Vote{
		Voter: voter2, Option: types.VoteOption_VOTE_OPTION_NO,
	}))

	resp, err := qs.GetProposalVotes(ctx, &types.QueryGetProposalVotesRequest{ProposalId: proposalID})
	require.NoError(t, err)
	require.Len(t, resp.Votes, 2)
	require.Equal(t, "2.000000000000000000", resp.Tally.YesWeight)
	require.Equal(t, "1.000000000000000000", resp.Tally.NoWeight)
}

func TestQueryGetProposalVotes_NotFound(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	qs := keeper.NewQueryServerImpl(k)

	_, err := qs.GetProposalVotes(ctx, &types.QueryGetProposalVotesRequest{ProposalId: 999})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestQueryGetProposalVotes_NoVotes(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	qs := keeper.NewQueryServerImpl(k)

	proposalID := uint64(1)
	require.NoError(t, k.Proposals.Set(ctx, proposalID, types.Proposal{
		Id: proposalID, CouncilName: "EmptyVotes",
	}))

	resp, err := qs.GetProposalVotes(ctx, &types.QueryGetProposalVotesRequest{ProposalId: proposalID})
	require.NoError(t, err)
	require.Empty(t, resp.Votes)
}
