package keeper_test

import (
	"testing"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"

	"cosmossdk.io/collections"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// packMsg creates a codectypes.Any from a sdk.Msg for use in proposal messages.
func packMsg(t *testing.T, msg sdk.Msg) *codectypes.Any {
	t.Helper()
	anyMsg, err := codectypes.NewAnyWithValue(msg)
	require.NoError(t, err)
	return anyMsg
}

// --- SubmitProposal Tests ---

func TestSubmitProposal_Success(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	councilName := "TestCouncil"
	policyAddr := sdk.AccAddress([]byte("policy_submit_test__")).String()
	proposer := sdk.AccAddress([]byte("proposer____________")).String()

	setupProposalState(t, k, ctx, councilName, policyAddr, proposer)

	anyMsg := packMsg(t, &types.MsgUpdateParams{
		Authority: sdk.AccAddress([]byte("authority")).String(),
		Params:    types.DefaultParams(),
	})

	resp, err := msgServer.SubmitProposal(ctx, &types.MsgSubmitProposal{
		Proposer:      proposer,
		PolicyAddress: policyAddr,
		Messages:      []*codectypes.Any{anyMsg},
		Metadata:      "test proposal",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify proposal stored correctly
	proposal, err := k.Proposals.Get(ctx, resp.ProposalId)
	require.NoError(t, err)
	require.Equal(t, councilName, proposal.CouncilName)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED, proposal.Status)
	require.Equal(t, proposer, proposal.Proposer)
	require.Equal(t, policyAddr, proposal.PolicyAddress)
	require.Equal(t, "test proposal", proposal.Metadata)
	require.Equal(t, ctx.BlockTime().Unix()+3600, proposal.VotingDeadline)
	require.Equal(t, ctx.BlockTime().Unix()+3600+600, proposal.ExecutionTime)

	// Verify indexed by council
	has, err := k.ProposalsByCouncil.Has(ctx, collections.Join(councilName, resp.ProposalId))
	require.NoError(t, err)
	require.True(t, has)
}

func TestSubmitProposal_UnknownPolicy(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	_, err := msgServer.SubmitProposal(ctx, &types.MsgSubmitProposal{
		Proposer:      sdk.AccAddress([]byte("proposer____________")).String(),
		PolicyAddress: "unknown_policy",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown policy address")
}

func TestSubmitProposal_NotMember(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	councilName := "TestCouncil"
	policyAddr := sdk.AccAddress([]byte("policy_notmember____")).String()
	member := sdk.AccAddress([]byte("actual_member_______")).String()

	setupProposalState(t, k, ctx, councilName, policyAddr, member)

	_, err := msgServer.SubmitProposal(ctx, &types.MsgSubmitProposal{
		Proposer:      sdk.AccAddress([]byte("nonmember___________")).String(),
		PolicyAddress: policyAddr,
		Messages:      []*codectypes.Any{packMsg(t, &types.MsgUpdateParams{Authority: "x", Params: types.DefaultParams()})},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "is not a member")
}

func TestSubmitProposal_EmptyMessages(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	councilName := "TestCouncil"
	policyAddr := sdk.AccAddress([]byte("policy_empty_msg____")).String()
	proposer := sdk.AccAddress([]byte("proposer_empty______")).String()

	setupProposalState(t, k, ctx, councilName, policyAddr, proposer)

	_, err := msgServer.SubmitProposal(ctx, &types.MsgSubmitProposal{
		Proposer:      proposer,
		PolicyAddress: policyAddr,
		Messages:      nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty proposal")
}

func TestSubmitProposal_UnauthorizedMessage(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	councilName := "TestCouncil"
	policyAddr := sdk.AccAddress([]byte("policy_unauth_msg___")).String()
	proposer := sdk.AccAddress([]byte("proposer_unauth_____")).String()

	setupProposalState(t, k, ctx, councilName, policyAddr, proposer)

	// Try to submit a message type that's not in AllowedMessages
	anyMsg := packMsg(t, &types.MsgDeleteGroup{Authority: "x", GroupName: "y"})

	_, err := msgServer.SubmitProposal(ctx, &types.MsgSubmitProposal{
		Proposer:      proposer,
		PolicyAddress: policyAddr,
		Messages:      []*codectypes.Any{anyMsg},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not allowed for policy")
}

func TestSubmitProposal_TermExpired_NonRenewBlocked(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	councilName := "ExpiredCouncil"
	policyAddr := sdk.AccAddress([]byte("policy_expired______")).String()
	proposer := sdk.AccAddress([]byte("proposer_expired____")).String()

	// Set up with expired term
	require.NoError(t, k.Groups.Set(ctx, councilName, types.Group{
		PolicyAddress:         policyAddr,
		CurrentTermExpiration: ctx.BlockTime().Unix() - 100,
	}))
	require.NoError(t, k.PolicyToName.Set(ctx, policyAddr, councilName))
	require.NoError(t, k.AddMember(ctx, councilName, types.Member{Address: proposer, Weight: "1"}))
	require.NoError(t, k.PolicyPermissions.Set(ctx, policyAddr, types.PolicyPermissions{
		PolicyAddress:   policyAddr,
		AllowedMessages: []string{"/sparkdream.commons.v1.MsgUpdateParams"},
	}))

	anyMsg := packMsg(t, &types.MsgUpdateParams{Authority: "x", Params: types.DefaultParams()})

	_, err := msgServer.SubmitProposal(ctx, &types.MsgSubmitProposal{
		Proposer:      proposer,
		PolicyAddress: policyAddr,
		Messages:      []*codectypes.Any{anyMsg},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "TERM EXPIRED")
}

func TestSubmitProposal_IncrementingIDs(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	councilName := "IDCouncil"
	policyAddr := sdk.AccAddress([]byte("policy_id_test______")).String()
	proposer := sdk.AccAddress([]byte("proposer_id_test____")).String()

	setupProposalState(t, k, ctx, councilName, policyAddr, proposer)
	anyMsg := packMsg(t, &types.MsgUpdateParams{Authority: "x", Params: types.DefaultParams()})

	resp1, err := msgServer.SubmitProposal(ctx, &types.MsgSubmitProposal{
		Proposer: proposer, PolicyAddress: policyAddr, Messages: []*codectypes.Any{anyMsg},
	})
	require.NoError(t, err)

	resp2, err := msgServer.SubmitProposal(ctx, &types.MsgSubmitProposal{
		Proposer: proposer, PolicyAddress: policyAddr, Messages: []*codectypes.Any{anyMsg},
	})
	require.NoError(t, err)

	require.Equal(t, resp1.ProposalId+1, resp2.ProposalId)
}

// --- VoteProposal Tests ---

func TestVoteProposal_Success(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	councilName := "VoteCouncil"
	policyAddr := sdk.AccAddress([]byte("policy_vote_test____")).String()
	voter := sdk.AccAddress([]byte("voter_______________")).String()

	setupProposalState(t, k, ctx, councilName, policyAddr, voter)

	proposalID := uint64(1)
	require.NoError(t, k.Proposals.Set(ctx, proposalID, types.Proposal{
		Id:             proposalID,
		CouncilName:    councilName,
		PolicyAddress:  policyAddr,
		Status:         types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED,
		VotingDeadline: ctx.BlockTime().Unix() + 3600,
	}))

	resp, err := msgServer.VoteProposal(ctx, &types.MsgVoteProposal{
		Voter:      voter,
		ProposalId: proposalID,
		Option:     types.VoteOption_VOTE_OPTION_YES,
		Metadata:   "I approve",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify vote stored
	vote, err := k.Votes.Get(ctx, collections.Join(proposalID, voter))
	require.NoError(t, err)
	require.Equal(t, types.VoteOption_VOTE_OPTION_YES, vote.Option)
	require.Equal(t, voter, vote.Voter)
	require.Equal(t, "I approve", vote.Metadata)
}

func TestVoteProposal_NotFound(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	_, err := msgServer.VoteProposal(ctx, &types.MsgVoteProposal{
		Voter:      sdk.AccAddress([]byte("voter_______________")).String(),
		ProposalId: 999,
		Option:     types.VoteOption_VOTE_OPTION_YES,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestVoteProposal_NotSubmitted(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	proposalID := uint64(1)
	require.NoError(t, k.Proposals.Set(ctx, proposalID, types.Proposal{
		Id:     proposalID,
		Status: types.ProposalStatus_PROPOSAL_STATUS_ACCEPTED,
	}))

	_, err := msgServer.VoteProposal(ctx, &types.MsgVoteProposal{
		Voter:      sdk.AccAddress([]byte("voter_______________")).String(),
		ProposalId: proposalID,
		Option:     types.VoteOption_VOTE_OPTION_YES,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not open for voting")
}

func TestVoteProposal_DeadlinePassed(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	councilName := "DeadlineCouncil"
	voter := sdk.AccAddress([]byte("voter_deadline______")).String()
	require.NoError(t, k.AddMember(ctx, councilName, types.Member{Address: voter, Weight: "1"}))

	proposalID := uint64(1)
	require.NoError(t, k.Proposals.Set(ctx, proposalID, types.Proposal{
		Id:             proposalID,
		CouncilName:    councilName,
		Status:         types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED,
		VotingDeadline: ctx.BlockTime().Unix() - 100,
	}))

	_, err := msgServer.VoteProposal(ctx, &types.MsgVoteProposal{
		Voter:      voter,
		ProposalId: proposalID,
		Option:     types.VoteOption_VOTE_OPTION_YES,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "voting period has ended")
}

func TestVoteProposal_NonMember(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	councilName := "VoteCouncil2"
	proposalID := uint64(1)
	require.NoError(t, k.Proposals.Set(ctx, proposalID, types.Proposal{
		Id:             proposalID,
		CouncilName:    councilName,
		Status:         types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED,
		VotingDeadline: ctx.BlockTime().Unix() + 3600,
	}))

	_, err := msgServer.VoteProposal(ctx, &types.MsgVoteProposal{
		Voter:      sdk.AccAddress([]byte("nonmember___________")).String(),
		ProposalId: proposalID,
		Option:     types.VoteOption_VOTE_OPTION_YES,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "is not a member")
}

func TestVoteProposal_EarlyAcceptance_Threshold(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	councilName := "EarlyAcceptCouncil"
	policyAddr := sdk.AccAddress([]byte("policy_early_accept_")).String()
	voter := sdk.AccAddress([]byte("voter_early_________")).String()

	setupProposalState(t, k, ctx, councilName, policyAddr, voter)
	// Threshold type: need yesWeight >= 1
	require.NoError(t, k.DecisionPolicies.Set(ctx, policyAddr, types.DecisionPolicy{
		PolicyType: "threshold", Threshold: "1", VotingPeriod: 3600,
	}))

	proposalID := uint64(1)
	require.NoError(t, k.Proposals.Set(ctx, proposalID, types.Proposal{
		Id: proposalID, CouncilName: councilName, PolicyAddress: policyAddr,
		Status: types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED, VotingDeadline: ctx.BlockTime().Unix() + 3600,
	}))

	// Vote YES (weight 1 >= threshold 1) → early acceptance
	_, err := msgServer.VoteProposal(ctx, &types.MsgVoteProposal{
		Voter: voter, ProposalId: proposalID, Option: types.VoteOption_VOTE_OPTION_YES,
	})
	require.NoError(t, err)

	proposal, err := k.Proposals.Get(ctx, proposalID)
	require.NoError(t, err)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_ACCEPTED, proposal.Status)
}

func TestVoteProposal_EarlyAcceptance_Percentage(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	councilName := "PercentCouncil"
	policyAddr := sdk.AccAddress([]byte("policy_percent______")).String()
	voter1 := sdk.AccAddress([]byte("voter1_pct__________")).String()
	voter2 := sdk.AccAddress([]byte("voter2_pct__________")).String()

	setupProposalState(t, k, ctx, councilName, policyAddr, voter1)
	require.NoError(t, k.AddMember(ctx, councilName, types.Member{Address: voter2, Weight: "1"}))
	// Percentage 0.51: need yesWeight/groupWeight >= 0.51
	require.NoError(t, k.DecisionPolicies.Set(ctx, policyAddr, types.DecisionPolicy{
		PolicyType: "percentage", Threshold: "0.5", VotingPeriod: 3600,
	}))

	proposalID := uint64(1)
	require.NoError(t, k.Proposals.Set(ctx, proposalID, types.Proposal{
		Id: proposalID, CouncilName: councilName, PolicyAddress: policyAddr,
		Status: types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED, VotingDeadline: ctx.BlockTime().Unix() + 3600,
	}))

	// Vote YES from voter1: 1/2 = 0.5 >= 0.5 → accepted
	_, err := msgServer.VoteProposal(ctx, &types.MsgVoteProposal{
		Voter: voter1, ProposalId: proposalID, Option: types.VoteOption_VOTE_OPTION_YES,
	})
	require.NoError(t, err)

	proposal, err := k.Proposals.Get(ctx, proposalID)
	require.NoError(t, err)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_ACCEPTED, proposal.Status)
}

func TestVoteProposal_NoEarlyAcceptance_InsufficientVotes(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	councilName := "NoEarlyCouncil"
	policyAddr := sdk.AccAddress([]byte("policy_noearly______")).String()
	voter1 := sdk.AccAddress([]byte("voter1_noearly______")).String()
	voter2 := sdk.AccAddress([]byte("voter2_noearly______")).String()

	setupProposalState(t, k, ctx, councilName, policyAddr, voter1)
	require.NoError(t, k.AddMember(ctx, councilName, types.Member{Address: voter2, Weight: "1"}))
	require.NoError(t, k.DecisionPolicies.Set(ctx, policyAddr, types.DecisionPolicy{
		PolicyType: "percentage", Threshold: "0.51", VotingPeriod: 3600,
	}))

	proposalID := uint64(1)
	require.NoError(t, k.Proposals.Set(ctx, proposalID, types.Proposal{
		Id: proposalID, CouncilName: councilName, PolicyAddress: policyAddr,
		Status: types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED, VotingDeadline: ctx.BlockTime().Unix() + 3600,
	}))

	// Vote NO from voter1: yesWeight=0 → no early acceptance
	_, err := msgServer.VoteProposal(ctx, &types.MsgVoteProposal{
		Voter: voter1, ProposalId: proposalID, Option: types.VoteOption_VOTE_OPTION_NO,
	})
	require.NoError(t, err)

	proposal, err := k.Proposals.Get(ctx, proposalID)
	require.NoError(t, err)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED, proposal.Status)
}

func TestVoteProposal_OverwriteVote(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	councilName := "OverwriteCouncil"
	policyAddr := sdk.AccAddress([]byte("policy_overwrite____")).String()
	voter := sdk.AccAddress([]byte("voter_overwrite_____")).String()

	setupProposalState(t, k, ctx, councilName, policyAddr, voter)
	require.NoError(t, k.DecisionPolicies.Set(ctx, policyAddr, types.DecisionPolicy{
		PolicyType: "threshold", Threshold: "100", VotingPeriod: 3600,
	}))

	proposalID := uint64(1)
	require.NoError(t, k.Proposals.Set(ctx, proposalID, types.Proposal{
		Id: proposalID, CouncilName: councilName, PolicyAddress: policyAddr,
		Status: types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED, VotingDeadline: ctx.BlockTime().Unix() + 3600,
	}))

	// Vote NO first
	_, err := msgServer.VoteProposal(ctx, &types.MsgVoteProposal{
		Voter: voter, ProposalId: proposalID, Option: types.VoteOption_VOTE_OPTION_NO,
	})
	require.NoError(t, err)

	// Overwrite with YES
	_, err = msgServer.VoteProposal(ctx, &types.MsgVoteProposal{
		Voter: voter, ProposalId: proposalID, Option: types.VoteOption_VOTE_OPTION_YES,
	})
	require.NoError(t, err)

	vote, err := k.Votes.Get(ctx, collections.Join(proposalID, voter))
	require.NoError(t, err)
	require.Equal(t, types.VoteOption_VOTE_OPTION_YES, vote.Option)
}

// --- ExecuteProposal Tests ---

func TestExecuteProposal_Success(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	councilName := "ExecCouncil"
	policyAddr := sdk.AccAddress([]byte("policy_exec_test____")).String()
	executor := sdk.AccAddress([]byte("executor____________")).String()

	require.NoError(t, k.Groups.Set(ctx, councilName, types.Group{
		PolicyAddress:         policyAddr,
		CurrentTermExpiration: ctx.BlockTime().Unix() + 86400,
	}))

	proposalID := uint64(1)
	require.NoError(t, k.Proposals.Set(ctx, proposalID, types.Proposal{
		Id:            proposalID,
		CouncilName:   councilName,
		PolicyAddress: policyAddr,
		Status:        types.ProposalStatus_PROPOSAL_STATUS_ACCEPTED,
		ExecutionTime: ctx.BlockTime().Unix() - 10,
		PolicyVersion: 0,
	}))

	resp, err := msgServer.ExecuteProposal(ctx, &types.MsgExecuteProposal{
		Executor:   executor,
		ProposalId: proposalID,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	proposal, err := k.Proposals.Get(ctx, proposalID)
	require.NoError(t, err)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_EXECUTED, proposal.Status)
}

func TestExecuteProposal_NotAccepted(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	proposalID := uint64(1)
	require.NoError(t, k.Proposals.Set(ctx, proposalID, types.Proposal{
		Id: proposalID, Status: types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED,
	}))

	_, err := msgServer.ExecuteProposal(ctx, &types.MsgExecuteProposal{
		Executor: sdk.AccAddress([]byte("executor____________")).String(), ProposalId: proposalID,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "is not accepted")
}

func TestExecuteProposal_NotFound(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	_, err := msgServer.ExecuteProposal(ctx, &types.MsgExecuteProposal{
		Executor: sdk.AccAddress([]byte("executor____________")).String(), ProposalId: 999,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestExecuteProposal_MinExecutionPeriodNotElapsed(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	proposalID := uint64(1)
	require.NoError(t, k.Proposals.Set(ctx, proposalID, types.Proposal{
		Id: proposalID, Status: types.ProposalStatus_PROPOSAL_STATUS_ACCEPTED,
		ExecutionTime: ctx.BlockTime().Unix() + 99999,
	}))

	_, err := msgServer.ExecuteProposal(ctx, &types.MsgExecuteProposal{
		Executor: sdk.AccAddress([]byte("executor____________")).String(), ProposalId: proposalID,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "min execution period has not elapsed")
}

func TestExecuteProposal_PolicyVersionChanged_Vetoed(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	policyAddr := sdk.AccAddress([]byte("policy_veto_version_")).String()
	councilName := "VetoCouncil"

	require.NoError(t, k.Groups.Set(ctx, councilName, types.Group{
		PolicyAddress:         policyAddr,
		CurrentTermExpiration: ctx.BlockTime().Unix() + 86400,
	}))
	// Set current policy version to 5
	require.NoError(t, k.PolicyVersion.Set(ctx, policyAddr, 5))

	proposalID := uint64(1)
	require.NoError(t, k.Proposals.Set(ctx, proposalID, types.Proposal{
		Id: proposalID, CouncilName: councilName, PolicyAddress: policyAddr,
		Status:        types.ProposalStatus_PROPOSAL_STATUS_ACCEPTED,
		ExecutionTime: ctx.BlockTime().Unix() - 10,
		PolicyVersion: 3, // Stale version
	}))

	_, err := msgServer.ExecuteProposal(ctx, &types.MsgExecuteProposal{
		Executor: sdk.AccAddress([]byte("executor____________")).String(), ProposalId: proposalID,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalidated by a policy version change")

	// Verify status is VETOED
	proposal, err := k.Proposals.Get(ctx, proposalID)
	require.NoError(t, err)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_VETOED, proposal.Status)
	require.Contains(t, proposal.FailedReason, "vetoed")
}

func TestExecuteProposal_TermExpired_NonRenewBlocked(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	policyAddr := sdk.AccAddress([]byte("policy_exec_expire__")).String()
	councilName := "ExpiredExecCouncil"

	require.NoError(t, k.Groups.Set(ctx, councilName, types.Group{
		PolicyAddress:         policyAddr,
		CurrentTermExpiration: ctx.BlockTime().Unix() - 100,
	}))

	// Create a proposal with non-renew messages
	anyMsg := packMsg(t, &types.MsgUpdateParams{Authority: "x", Params: types.DefaultParams()})
	proposalID := uint64(1)
	require.NoError(t, k.Proposals.Set(ctx, proposalID, types.Proposal{
		Id: proposalID, CouncilName: councilName, PolicyAddress: policyAddr,
		Status:        types.ProposalStatus_PROPOSAL_STATUS_ACCEPTED,
		ExecutionTime: ctx.BlockTime().Unix() - 10, PolicyVersion: 0,
		Messages: []*codectypes.Any{anyMsg},
	}))

	_, err := msgServer.ExecuteProposal(ctx, &types.MsgExecuteProposal{
		Executor: sdk.AccAddress([]byte("executor____________")).String(), ProposalId: proposalID,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "TERM EXPIRED")
}

// --- Helpers ---

func setupProposalState(t *testing.T, k keeper.Keeper, ctx sdk.Context, councilName, policyAddr, memberAddr string) {
	t.Helper()
	require.NoError(t, k.Groups.Set(ctx, councilName, types.Group{
		PolicyAddress:         policyAddr,
		CurrentTermExpiration: ctx.BlockTime().Unix() + 86400,
	}))
	require.NoError(t, k.PolicyToName.Set(ctx, policyAddr, councilName))
	require.NoError(t, k.AddMember(ctx, councilName, types.Member{Address: memberAddr, Weight: "1"}))
	require.NoError(t, k.PolicyPermissions.Set(ctx, policyAddr, types.PolicyPermissions{
		PolicyAddress:   policyAddr,
		AllowedMessages: []string{"/sparkdream.commons.v1.MsgUpdateParams"},
	}))
	require.NoError(t, k.DecisionPolicies.Set(ctx, policyAddr, types.DecisionPolicy{
		PolicyType:         "percentage",
		Threshold:          "0.51",
		VotingPeriod:       3600,
		MinExecutionPeriod: 600,
	}))
}
