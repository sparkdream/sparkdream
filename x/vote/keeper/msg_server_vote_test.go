package keeper_test

import (
	"testing"

	"sparkdream/x/vote/keeper"
	"sparkdream/x/vote/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestVote_HappyPath(t *testing.T) {
	f := initTestFixture(t)

	// Must register a voter before creating proposals.
	f.registerVoter(t, f.member, genZkPubKey(1))
	proposalID := f.createPublicProposal(t, f.member)

	nullifier := genNullifier(1)
	_, err := f.msgServer.Vote(f.ctx, &types.MsgVote{
		Submitter:  f.member,
		ProposalId: proposalID,
		Nullifier:  nullifier,
		VoteOption: 0,
		Proof:      []byte("fake-proof"),
	})
	require.NoError(t, err)

	// Verify tally updated.
	proposal, err := f.keeper.VotingProposal.Get(f.ctx, proposalID)
	require.NoError(t, err)
	require.Equal(t, uint64(1), proposal.Tally[0].VoteCount)
	require.Equal(t, uint64(0), proposal.Tally[1].VoteCount)
}

func TestVote_ProposalNotFound(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.msgServer.Vote(f.ctx, &types.MsgVote{
		Submitter:  f.member,
		ProposalId: 999,
		Nullifier:  genNullifier(1),
		VoteOption: 0,
		Proof:      []byte("fake-proof"),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrProposalNotFound)
}

func TestVote_ProposalNotActive(t *testing.T) {
	f := initTestFixture(t)

	f.registerVoter(t, f.member, genZkPubKey(1))
	proposalID := f.createPublicProposal(t, f.member)

	// Set proposal status to FINALIZED.
	proposal, err := f.keeper.VotingProposal.Get(f.ctx, proposalID)
	require.NoError(t, err)
	proposal.Status = types.ProposalStatus_PROPOSAL_STATUS_FINALIZED
	require.NoError(t, f.keeper.VotingProposal.Set(f.ctx, proposalID, proposal))

	_, err = f.msgServer.Vote(f.ctx, &types.MsgVote{
		Submitter:  f.member,
		ProposalId: proposalID,
		Nullifier:  genNullifier(1),
		VoteOption: 0,
		Proof:      []byte("fake-proof"),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrProposalNotActive)
}

func TestVote_WrongVisibility_Sealed(t *testing.T) {
	f := initTestFixture(t)

	f.registerVoter(t, f.member, genZkPubKey(1))
	proposalID := f.createSealedProposal(t, f.member)

	_, err := f.msgServer.Vote(f.ctx, &types.MsgVote{
		Submitter:  f.member,
		ProposalId: proposalID,
		Nullifier:  genNullifier(1),
		VoteOption: 0,
		Proof:      []byte("fake-proof"),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInvalidVisibility)
}

func TestVote_OptionOutOfRange(t *testing.T) {
	f := initTestFixture(t)

	f.registerVoter(t, f.member, genZkPubKey(1))
	proposalID := f.createPublicProposal(t, f.member)

	// Proposal has 2 options (0 and 1), so option 5 is out of range.
	_, err := f.msgServer.Vote(f.ctx, &types.MsgVote{
		Submitter:  f.member,
		ProposalId: proposalID,
		Nullifier:  genNullifier(1),
		VoteOption: 5,
		Proof:      []byte("fake-proof"),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrVoteOptionOutOfRange)
}

func TestVote_NullifierAlreadyUsed(t *testing.T) {
	f := initTestFixture(t)

	f.registerVoter(t, f.member, genZkPubKey(1))
	proposalID := f.createPublicProposal(t, f.member)

	nullifier := genNullifier(1)

	// First vote succeeds.
	_, err := f.msgServer.Vote(f.ctx, &types.MsgVote{
		Submitter:  f.member,
		ProposalId: proposalID,
		Nullifier:  nullifier,
		VoteOption: 0,
		Proof:      []byte("fake-proof"),
	})
	require.NoError(t, err)

	// Second vote with same nullifier fails.
	_, err = f.msgServer.Vote(f.ctx, &types.MsgVote{
		Submitter:  f.member,
		ProposalId: proposalID,
		Nullifier:  nullifier,
		VoteOption: 1,
		Proof:      []byte("fake-proof"),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrNullifierUsed)
}

func TestVote_TallyIncrements(t *testing.T) {
	f := initTestFixture(t)

	f.registerVoter(t, f.member, genZkPubKey(1))
	proposalID := f.createPublicProposal(t, f.member)

	// Cast a vote for option 0.
	_, err := f.msgServer.Vote(f.ctx, &types.MsgVote{
		Submitter:  f.member,
		ProposalId: proposalID,
		Nullifier:  genNullifier(1),
		VoteOption: 0,
		Proof:      []byte("fake-proof"),
	})
	require.NoError(t, err)

	proposal, err := f.keeper.VotingProposal.Get(f.ctx, proposalID)
	require.NoError(t, err)
	require.Equal(t, uint64(1), proposal.Tally[0].VoteCount)
	require.Equal(t, uint64(0), proposal.Tally[1].VoteCount)

	// Cast another vote for option 1.
	_, err = f.msgServer.Vote(f.ctx, &types.MsgVote{
		Submitter:  f.member2,
		ProposalId: proposalID,
		Nullifier:  genNullifier(2),
		VoteOption: 1,
		Proof:      []byte("fake-proof"),
	})
	require.NoError(t, err)

	proposal, err = f.keeper.VotingProposal.Get(f.ctx, proposalID)
	require.NoError(t, err)
	require.Equal(t, uint64(1), proposal.Tally[0].VoteCount)
	require.Equal(t, uint64(1), proposal.Tally[1].VoteCount)
}

func TestVote_EmitsEvent(t *testing.T) {
	f := initTestFixture(t)

	f.registerVoter(t, f.member, genZkPubKey(1))
	proposalID := f.createPublicProposal(t, f.member)

	_, err := f.msgServer.Vote(f.ctx, &types.MsgVote{
		Submitter:  f.member,
		ProposalId: proposalID,
		Nullifier:  genNullifier(1),
		VoteOption: 0,
		Proof:      []byte("fake-proof"),
	})
	require.NoError(t, err)

	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	events := sdkCtx.EventManager().Events()

	found := false
	for _, e := range events {
		if e.Type == types.EventVoteCast {
			found = true
			break
		}
	}
	require.True(t, found, "expected %s event to be emitted", types.EventVoteCast)
}

func TestVote_AnonymousVoteStored(t *testing.T) {
	f := initTestFixture(t)

	f.registerVoter(t, f.member, genZkPubKey(1))
	proposalID := f.createPublicProposal(t, f.member)

	nullifier := genNullifier(1)
	_, err := f.msgServer.Vote(f.ctx, &types.MsgVote{
		Submitter:  f.member,
		ProposalId: proposalID,
		Nullifier:  nullifier,
		VoteOption: 0,
		Proof:      []byte("fake-proof"),
	})
	require.NoError(t, err)

	// Verify the anonymous vote is stored.
	voteKey := keeper.NullifierKeyForTest(proposalID, nullifier)
	vote, err := f.keeper.AnonymousVote.Get(f.ctx, voteKey)
	require.NoError(t, err)
	require.Equal(t, proposalID, vote.ProposalId)
	require.Equal(t, uint32(0), vote.VoteOption)
	require.Equal(t, nullifier, vote.Nullifier)
}
