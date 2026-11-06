package keeper_test

import (
	"testing"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// A policy change (veto, config change, narrowed permissions, renewal) ends
// the council's in-flight proposals VETOED where the change is noticed:
// votes on them are refused, the deadline tally closes them VETOED instead
// of ACCEPTED/REJECTED, and an accepted-but-unexecuted one closes VETOED at
// its execution deadline instead of EXPIRED.

func TestVoteProposal_RefusedAfterPolicyChange(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	policyAddr := sdk.AccAddress([]byte("policy_inval_vote___")).String()
	voter := sdk.AccAddress([]byte("voter_inval_vote____")).String()
	setupProposalState(t, k, ctx, "InvalVoteCouncil", policyAddr, voter)
	require.NoError(t, k.DecisionPolicies.Set(ctx, policyAddr, types.DecisionPolicy{
		PolicyType: "threshold", Threshold: "1", VotingPeriod: 3600,
	}))

	proposalID := uint64(1)
	require.NoError(t, k.Proposals.Set(ctx, proposalID, types.Proposal{
		Id: proposalID, CouncilName: "InvalVoteCouncil", PolicyAddress: policyAddr,
		Status: types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED, VotingDeadline: ctx.BlockTime().Unix() + 3600,
	}))
	_, err := k.BumpPolicyVersion(ctx, policyAddr)
	require.NoError(t, err)

	// Without the guard this YES vote would meet the threshold and accept
	// the invalidated proposal early.
	_, err = msgServer.VoteProposal(ctx, &types.MsgVoteProposal{
		Voter: voter, ProposalId: proposalID, Option: types.VoteOption_VOTE_OPTION_YES,
	})
	require.ErrorContains(t, err, "invalidated")

	proposal, err := k.Proposals.Get(ctx, proposalID)
	require.NoError(t, err)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED, proposal.Status)
}

func TestAnonymousVoteProposal_RefusedAfterPolicyChange(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	pid := submitAnonProposal(t, k, ctx)
	proposal, err := k.Proposals.Get(ctx, pid)
	require.NoError(t, err)
	proposal.SecretBallot = true
	require.NoError(t, k.Proposals.Set(ctx, pid, proposal))
	_, err = k.BumpPolicyVersion(ctx, proposal.PolicyAddress)
	require.NoError(t, err)

	_, err = msgServer.AnonymousVoteProposal(ctx, &types.MsgAnonymousVoteProposal{
		Voter: shieldModuleAddr(), ProposalId: pid, Option: types.VoteOption_VOTE_OPTION_YES,
	})
	require.ErrorContains(t, err, "invalidated")

	_, err = k.AnonVoteTallies.Get(ctx, pid)
	require.ErrorIs(t, err, collections.ErrNotFound)
}

func TestEndBlockProposals_InvalidatedSubmittedEndsVetoed(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	policyAddr := sdk.AccAddress([]byte("policy_inval_tally__")).String()
	voter := sdk.AccAddress([]byte("voter_inval_tally___")).String()
	setupProposalState(t, k, ctx, "InvalTallyCouncil", policyAddr, voter)

	proposalID := uint64(1)
	require.NoError(t, k.SetProposalQueued(ctx, proposalID, types.Proposal{
		Id: proposalID, CouncilName: "InvalTallyCouncil", PolicyAddress: policyAddr,
		Status:         types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED,
		VotingDeadline: ctx.BlockTime().Unix() - 10,
	}))
	// A YES vote cast before the veto meets the threshold on its own.
	require.NoError(t, k.Votes.Set(ctx, collections.Join(proposalID, voter), types.Vote{
		Voter: voter, Option: types.VoteOption_VOTE_OPTION_YES,
	}))
	_, err := k.BumpPolicyVersion(ctx, policyAddr)
	require.NoError(t, err)

	require.NoError(t, k.EndBlockProposals(ctx))

	proposal, err := k.Proposals.Get(ctx, proposalID)
	require.NoError(t, err)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_VETOED, proposal.Status)
	require.Contains(t, proposal.FailedReason, "vetoed")
}

func TestEndBlockProposals_InvalidatedAcceptedEndsVetoed(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	policyAddr := sdk.AccAddress([]byte("policy_inval_accept_")).String()
	voter := sdk.AccAddress([]byte("voter_inval_accept__")).String()
	setupProposalState(t, k, ctx, "InvalAcceptCouncil", policyAddr, voter)

	// Accepted before the veto, never executed, execution window now closed.
	proposalID := uint64(1)
	require.NoError(t, k.SetProposalQueued(ctx, proposalID, types.Proposal{
		Id: proposalID, CouncilName: "InvalAcceptCouncil", PolicyAddress: policyAddr,
		Status:        types.ProposalStatus_PROPOSAL_STATUS_ACCEPTED,
		ExecutionTime: ctx.BlockTime().Unix() - keeper.ProposalExecutionWindow - 10,
	}))
	_, err := k.BumpPolicyVersion(ctx, policyAddr)
	require.NoError(t, err)

	require.NoError(t, k.EndBlockProposals(ctx))

	proposal, err := k.Proposals.Get(ctx, proposalID)
	require.NoError(t, err)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_VETOED, proposal.Status)
}
