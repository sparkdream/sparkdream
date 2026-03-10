package keeper_test

import (
	"testing"
	"time"

	"sparkdream/x/commons/types"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestEndBlockProposals_ExpiresAndAccepts(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	councilName := "EndBlockCouncil"
	policyAddr := sdk.AccAddress([]byte("policy_endblock_____")).String()
	voter1 := sdk.AccAddress([]byte("voter1_endblock_____")).String()
	voter2 := sdk.AccAddress([]byte("voter2_endblock_____")).String()

	setupProposalState(t, k, ctx, councilName, policyAddr, voter1)
	require.NoError(t, k.AddMember(ctx, councilName, types.Member{Address: voter2, Weight: "1"}))

	// Percentage 0.5: 1 YES / 2 total = 0.5 >= 0.5 → accept
	require.NoError(t, k.DecisionPolicies.Set(ctx, policyAddr, types.DecisionPolicy{
		PolicyType: "percentage", Threshold: "0.5", VotingPeriod: 100,
	}))

	// Create proposal with deadline in the past
	proposalID := uint64(1)
	require.NoError(t, k.Proposals.Set(ctx, proposalID, types.Proposal{
		Id: proposalID, CouncilName: councilName, PolicyAddress: policyAddr,
		Status:         types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED,
		VotingDeadline: ctx.BlockTime().Unix() - 10,
	}))

	// Add a YES vote from voter1
	require.NoError(t, k.Votes.Set(ctx, collections.Join(proposalID, voter1), types.Vote{
		Voter: voter1, Option: types.VoteOption_VOTE_OPTION_YES,
	}))

	err := k.EndBlockProposals(ctx)
	require.NoError(t, err)

	proposal, err := k.Proposals.Get(ctx, proposalID)
	require.NoError(t, err)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_ACCEPTED, proposal.Status)
}

func TestEndBlockProposals_ExpiresAndRejects(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	councilName := "RejectCouncil"
	policyAddr := sdk.AccAddress([]byte("policy_reject_eb____")).String()
	voter1 := sdk.AccAddress([]byte("voter1_reject_eb____")).String()
	voter2 := sdk.AccAddress([]byte("voter2_reject_eb____")).String()

	setupProposalState(t, k, ctx, councilName, policyAddr, voter1)
	require.NoError(t, k.AddMember(ctx, councilName, types.Member{Address: voter2, Weight: "1"}))

	require.NoError(t, k.DecisionPolicies.Set(ctx, policyAddr, types.DecisionPolicy{
		PolicyType: "percentage", Threshold: "0.51", VotingPeriod: 100,
	}))

	proposalID := uint64(1)
	require.NoError(t, k.Proposals.Set(ctx, proposalID, types.Proposal{
		Id: proposalID, CouncilName: councilName, PolicyAddress: policyAddr,
		Status:         types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED,
		VotingDeadline: ctx.BlockTime().Unix() - 10,
	}))

	// Only NO votes
	require.NoError(t, k.Votes.Set(ctx, collections.Join(proposalID, voter1), types.Vote{
		Voter: voter1, Option: types.VoteOption_VOTE_OPTION_NO,
	}))

	err := k.EndBlockProposals(ctx)
	require.NoError(t, err)

	proposal, err := k.Proposals.Get(ctx, proposalID)
	require.NoError(t, err)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_REJECTED, proposal.Status)
	require.Contains(t, proposal.FailedReason, "threshold not met")
}

func TestEndBlockProposals_SkipsActiveProposals(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	// Proposal still within voting period
	proposalID := uint64(1)
	require.NoError(t, k.Proposals.Set(ctx, proposalID, types.Proposal{
		Id: proposalID, Status: types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED,
		VotingDeadline: ctx.BlockTime().Unix() + 3600,
	}))

	err := k.EndBlockProposals(ctx)
	require.NoError(t, err)

	// Should remain SUBMITTED
	proposal, err := k.Proposals.Get(ctx, proposalID)
	require.NoError(t, err)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED, proposal.Status)
}

func TestEndBlockProposals_SkipsNonSubmitted(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	// Already accepted proposal past deadline — should NOT be re-processed
	proposalID := uint64(1)
	require.NoError(t, k.Proposals.Set(ctx, proposalID, types.Proposal{
		Id: proposalID, Status: types.ProposalStatus_PROPOSAL_STATUS_ACCEPTED,
		VotingDeadline: ctx.BlockTime().Unix() - 10,
	}))

	err := k.EndBlockProposals(ctx)
	require.NoError(t, err)

	proposal, err := k.Proposals.Get(ctx, proposalID)
	require.NoError(t, err)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_ACCEPTED, proposal.Status)
}

func TestEndBlockProposals_NoVotes_Rejected(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	councilName := "NoVotesCouncil"
	policyAddr := sdk.AccAddress([]byte("policy_novotes______")).String()
	member := sdk.AccAddress([]byte("member_novotes______")).String()

	setupProposalState(t, k, ctx, councilName, policyAddr, member)
	require.NoError(t, k.DecisionPolicies.Set(ctx, policyAddr, types.DecisionPolicy{
		PolicyType: "percentage", Threshold: "0.5", VotingPeriod: 100,
	}))

	proposalID := uint64(1)
	require.NoError(t, k.Proposals.Set(ctx, proposalID, types.Proposal{
		Id: proposalID, CouncilName: councilName, PolicyAddress: policyAddr,
		Status: types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED, VotingDeadline: ctx.BlockTime().Unix() - 10,
	}))

	err := k.EndBlockProposals(ctx)
	require.NoError(t, err)

	proposal, err := k.Proposals.Get(ctx, proposalID)
	require.NoError(t, err)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_REJECTED, proposal.Status)
}

func TestEndBlockProposals_MultipleProposals(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	councilName := "MultiCouncil"
	policyAddr := sdk.AccAddress([]byte("policy_multi_eb_____")).String()
	voter := sdk.AccAddress([]byte("voter_multi_eb______")).String()

	setupProposalState(t, k, ctx, councilName, policyAddr, voter)
	require.NoError(t, k.DecisionPolicies.Set(ctx, policyAddr, types.DecisionPolicy{
		PolicyType: "threshold", Threshold: "1", VotingPeriod: 100,
	}))

	// Proposal 1: expired, has YES vote → should be accepted
	require.NoError(t, k.Proposals.Set(ctx, 1, types.Proposal{
		Id: 1, CouncilName: councilName, PolicyAddress: policyAddr,
		Status: types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED, VotingDeadline: ctx.BlockTime().Unix() - 10,
	}))
	require.NoError(t, k.Votes.Set(ctx, collections.Join(uint64(1), voter), types.Vote{
		Voter: voter, Option: types.VoteOption_VOTE_OPTION_YES,
	}))

	// Proposal 2: expired, no votes → should be rejected
	require.NoError(t, k.Proposals.Set(ctx, 2, types.Proposal{
		Id: 2, CouncilName: councilName, PolicyAddress: policyAddr,
		Status: types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED, VotingDeadline: ctx.BlockTime().Unix() - 10,
	}))

	// Proposal 3: still active → should remain submitted
	require.NoError(t, k.Proposals.Set(ctx, 3, types.Proposal{
		Id: 3, CouncilName: councilName, PolicyAddress: policyAddr,
		Status: types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED, VotingDeadline: ctx.BlockTime().Unix() + 3600,
	}))

	err := k.EndBlockProposals(ctx)
	require.NoError(t, err)

	p1, _ := k.Proposals.Get(ctx, 1)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_ACCEPTED, p1.Status)

	p2, _ := k.Proposals.Get(ctx, 2)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_REJECTED, p2.Status)

	p3, _ := k.Proposals.Get(ctx, 3)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED, p3.Status)
}

// --- TallyProposal Tests ---

func TestTallyProposal_MixedVotes(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	councilName := "TallyCouncil"
	voter1 := sdk.AccAddress([]byte("voter1_tally________")).String()
	voter2 := sdk.AccAddress([]byte("voter2_tally________")).String()
	voter3 := sdk.AccAddress([]byte("voter3_tally________")).String()

	require.NoError(t, k.AddMember(ctx, councilName, types.Member{Address: voter1, Weight: "2"}))
	require.NoError(t, k.AddMember(ctx, councilName, types.Member{Address: voter2, Weight: "3"}))
	require.NoError(t, k.AddMember(ctx, councilName, types.Member{Address: voter3, Weight: "1"}))

	proposalID := uint64(1)
	proposal := types.Proposal{Id: proposalID, CouncilName: councilName}

	require.NoError(t, k.Votes.Set(ctx, collections.Join(proposalID, voter1), types.Vote{
		Voter: voter1, Option: types.VoteOption_VOTE_OPTION_YES,
	}))
	require.NoError(t, k.Votes.Set(ctx, collections.Join(proposalID, voter2), types.Vote{
		Voter: voter2, Option: types.VoteOption_VOTE_OPTION_NO,
	}))
	require.NoError(t, k.Votes.Set(ctx, collections.Join(proposalID, voter3), types.Vote{
		Voter: voter3, Option: types.VoteOption_VOTE_OPTION_ABSTAIN,
	}))

	tally, err := k.TallyProposal(ctx, proposal)
	require.NoError(t, err)
	require.Equal(t, "2.000000000000000000", tally.YesWeight)
	require.Equal(t, "3.000000000000000000", tally.NoWeight)
	require.Equal(t, "1.000000000000000000", tally.AbstainWeight)
}

func TestTallyProposal_NoVotes(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	proposal := types.Proposal{Id: 99, CouncilName: "EmptyCouncil"}

	tally, err := k.TallyProposal(ctx, proposal)
	require.NoError(t, err)
	require.Equal(t, "0.000000000000000000", tally.YesWeight)
	require.Equal(t, "0.000000000000000000", tally.NoWeight)
	require.Equal(t, "0.000000000000000000", tally.AbstainWeight)
}

func TestTallyProposal_RemovedMemberSkipped(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	councilName := "RemovedTallyCouncil"
	voter1 := sdk.AccAddress([]byte("voter1_rmtally______")).String()
	voter2 := sdk.AccAddress([]byte("voter2_rmtally______")).String()

	// Only voter1 is a member (voter2 was removed after voting)
	require.NoError(t, k.AddMember(ctx, councilName, types.Member{Address: voter1, Weight: "1"}))

	proposalID := uint64(1)
	proposal := types.Proposal{Id: proposalID, CouncilName: councilName}

	require.NoError(t, k.Votes.Set(ctx, collections.Join(proposalID, voter1), types.Vote{
		Voter: voter1, Option: types.VoteOption_VOTE_OPTION_YES,
	}))
	require.NoError(t, k.Votes.Set(ctx, collections.Join(proposalID, voter2), types.Vote{
		Voter: voter2, Option: types.VoteOption_VOTE_OPTION_YES,
	}))

	tally, err := k.TallyProposal(ctx, proposal)
	require.NoError(t, err)
	// Only voter1's weight should be counted (voter2 removed, skipped)
	require.Equal(t, "1.000000000000000000", tally.YesWeight)
}

// --- checkThreshold Tests (via EndBlock) ---

func TestCheckThreshold_PercentageType(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	councilName := "ThresholdPctCouncil"
	policyAddr := sdk.AccAddress([]byte("policy_threshold_pct")).String()
	voter1 := sdk.AccAddress([]byte("voter1_thr_pct______")).String()
	voter2 := sdk.AccAddress([]byte("voter2_thr_pct______")).String()
	voter3 := sdk.AccAddress([]byte("voter3_thr_pct______")).String()

	setupProposalState(t, k, ctx, councilName, policyAddr, voter1)
	require.NoError(t, k.AddMember(ctx, councilName, types.Member{Address: voter2, Weight: "1"}))
	require.NoError(t, k.AddMember(ctx, councilName, types.Member{Address: voter3, Weight: "1"}))

	// 66% threshold: need 2/3 YES
	require.NoError(t, k.DecisionPolicies.Set(ctx, policyAddr, types.DecisionPolicy{
		PolicyType: "percentage", Threshold: "0.66", VotingPeriod: 100,
	}))

	// Expired proposal with 2 YES, 1 NO → 2/3 ≈ 0.666 >= 0.66 → accept
	proposalID := uint64(1)
	require.NoError(t, k.Proposals.Set(ctx, proposalID, types.Proposal{
		Id: proposalID, CouncilName: councilName, PolicyAddress: policyAddr,
		Status: types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED, VotingDeadline: ctx.BlockTime().Unix() - 1,
	}))
	require.NoError(t, k.Votes.Set(ctx, collections.Join(proposalID, voter1), types.Vote{Voter: voter1, Option: types.VoteOption_VOTE_OPTION_YES}))
	require.NoError(t, k.Votes.Set(ctx, collections.Join(proposalID, voter2), types.Vote{Voter: voter2, Option: types.VoteOption_VOTE_OPTION_YES}))
	require.NoError(t, k.Votes.Set(ctx, collections.Join(proposalID, voter3), types.Vote{Voter: voter3, Option: types.VoteOption_VOTE_OPTION_NO}))

	require.NoError(t, k.EndBlockProposals(ctx))

	p, _ := k.Proposals.Get(ctx, proposalID)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_ACCEPTED, p.Status)
}

func TestCheckThreshold_ThresholdType(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	councilName := "ThresholdNumCouncil"
	policyAddr := sdk.AccAddress([]byte("policy_threshold_num")).String()
	voter1 := sdk.AccAddress([]byte("voter1_thr_num______")).String()
	voter2 := sdk.AccAddress([]byte("voter2_thr_num______")).String()

	setupProposalState(t, k, ctx, councilName, policyAddr, voter1)
	require.NoError(t, k.AddMember(ctx, councilName, types.Member{Address: voter2, Weight: "1"}))

	// Threshold type: need yesWeight >= 2
	require.NoError(t, k.DecisionPolicies.Set(ctx, policyAddr, types.DecisionPolicy{
		PolicyType: "threshold", Threshold: "2", VotingPeriod: 100,
	}))

	// Only 1 YES → rejected
	proposalID := uint64(1)
	require.NoError(t, k.Proposals.Set(ctx, proposalID, types.Proposal{
		Id: proposalID, CouncilName: councilName, PolicyAddress: policyAddr,
		Status: types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED, VotingDeadline: ctx.BlockTime().Unix() - 1,
	}))
	require.NoError(t, k.Votes.Set(ctx, collections.Join(proposalID, voter1), types.Vote{Voter: voter1, Option: types.VoteOption_VOTE_OPTION_YES}))

	require.NoError(t, k.EndBlockProposals(ctx))

	p, _ := k.Proposals.Get(ctx, proposalID)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_REJECTED, p.Status)

	// 2 YES → accepted (new proposal)
	proposalID2 := uint64(2)
	// Need a fresh context time so the new proposal is also past deadline
	ctx = ctx.WithBlockTime(time.Now())
	require.NoError(t, k.Proposals.Set(ctx, proposalID2, types.Proposal{
		Id: proposalID2, CouncilName: councilName, PolicyAddress: policyAddr,
		Status: types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED, VotingDeadline: ctx.BlockTime().Unix() - 1,
	}))
	require.NoError(t, k.Votes.Set(ctx, collections.Join(proposalID2, voter1), types.Vote{Voter: voter1, Option: types.VoteOption_VOTE_OPTION_YES}))
	require.NoError(t, k.Votes.Set(ctx, collections.Join(proposalID2, voter2), types.Vote{Voter: voter2, Option: types.VoteOption_VOTE_OPTION_YES}))

	require.NoError(t, k.EndBlockProposals(ctx))

	p2, _ := k.Proposals.Get(ctx, proposalID2)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_ACCEPTED, p2.Status)
}

// setupProposalState is defined in msg_server_proposals_test.go and reused here.
