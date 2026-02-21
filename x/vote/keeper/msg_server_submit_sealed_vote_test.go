package keeper_test

import (
	"testing"

	"sparkdream/x/vote/keeper"
	"sparkdream/x/vote/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestSealedVote_HappyPath_SealedProposal(t *testing.T) {
	f := initTestFixture(t)

	f.registerVoter(t, f.member, genZkPubKey(1))
	proposalID := f.createSealedProposal(t, f.member)

	salt := make([]byte, 32)
	for i := range salt {
		salt[i] = byte(i)
	}
	commitment := keeper.ComputeCommitmentHashForTest(0, salt)

	_, err := f.msgServer.SealedVote(f.ctx, &types.MsgSealedVote{
		Submitter:       f.member,
		ProposalId:      proposalID,
		Nullifier:       genNullifier(1),
		VoteCommitment:  commitment,
		Proof:           []byte("fake-proof"),
		EncryptedReveal: []byte("encrypted-data"),
	})
	require.NoError(t, err)

	// Verify sealed vote is stored.
	voteKey := keeper.NullifierKeyForTest(proposalID, genNullifier(1))
	sv, err := f.keeper.SealedVote.Get(f.ctx, voteKey)
	require.NoError(t, err)
	require.Equal(t, proposalID, sv.ProposalId)
	require.Equal(t, commitment, sv.VoteCommitment)
	require.False(t, sv.Revealed)
}

func TestSealedVote_HappyPath_PrivateProposal(t *testing.T) {
	f := initTestFixture(t)

	f.registerVoter(t, f.member, genZkPubKey(1))

	// Create PRIVATE proposal via CreateAnonymousProposal.
	// seasonKeeper.GetCurrentEpoch returns 10 by default.
	resp, err := f.msgServer.CreateAnonymousProposal(f.ctx, &types.MsgCreateAnonymousProposal{
		Submitter:    f.member,
		Title:        "Private Proposal",
		Options:      f.standardOptions(),
		Visibility:   types.VisibilityLevel_VISIBILITY_PRIVATE,
		ClaimedEpoch: 10,
		Nullifier:    genNullifier(100),
		Proof:        []byte("fake-proof"),
	})
	require.NoError(t, err)
	proposalID := resp.ProposalId

	salt := make([]byte, 32)
	for i := range salt {
		salt[i] = byte(i + 10)
	}
	commitment := keeper.ComputeCommitmentHashForTest(1, salt)

	_, err = f.msgServer.SealedVote(f.ctx, &types.MsgSealedVote{
		Submitter:       f.member,
		ProposalId:      proposalID,
		Nullifier:       genNullifier(101),
		VoteCommitment:  commitment,
		Proof:           []byte("fake-proof"),
		EncryptedReveal: []byte("encrypted-data"),
	})
	require.NoError(t, err)
}

func TestSealedVote_ProposalNotFound(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.msgServer.SealedVote(f.ctx, &types.MsgSealedVote{
		Submitter:      f.member,
		ProposalId:     999,
		Nullifier:      genNullifier(1),
		VoteCommitment: []byte("commitment"),
		Proof:          []byte("fake-proof"),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrProposalNotFound)
}

func TestSealedVote_ProposalNotActive(t *testing.T) {
	f := initTestFixture(t)

	f.registerVoter(t, f.member, genZkPubKey(1))
	proposalID := f.createSealedProposal(t, f.member)

	// Set proposal to FINALIZED.
	proposal, err := f.keeper.VotingProposal.Get(f.ctx, proposalID)
	require.NoError(t, err)
	proposal.Status = types.ProposalStatus_PROPOSAL_STATUS_FINALIZED
	require.NoError(t, f.keeper.VotingProposal.Set(f.ctx, proposalID, proposal))

	_, err = f.msgServer.SealedVote(f.ctx, &types.MsgSealedVote{
		Submitter:      f.member,
		ProposalId:     proposalID,
		Nullifier:      genNullifier(1),
		VoteCommitment: []byte("commitment"),
		Proof:          []byte("fake-proof"),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrProposalNotActive)
}

func TestSealedVote_WrongVisibility_PublicProposal(t *testing.T) {
	f := initTestFixture(t)

	f.registerVoter(t, f.member, genZkPubKey(1))
	proposalID := f.createPublicProposal(t, f.member)

	_, err := f.msgServer.SealedVote(f.ctx, &types.MsgSealedVote{
		Submitter:      f.member,
		ProposalId:     proposalID,
		Nullifier:      genNullifier(1),
		VoteCommitment: []byte("commitment"),
		Proof:          []byte("fake-proof"),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInvalidVisibility)
}

func TestSealedVote_EmptyCommitment(t *testing.T) {
	f := initTestFixture(t)

	f.registerVoter(t, f.member, genZkPubKey(1))
	proposalID := f.createSealedProposal(t, f.member)

	_, err := f.msgServer.SealedVote(f.ctx, &types.MsgSealedVote{
		Submitter:      f.member,
		ProposalId:     proposalID,
		Nullifier:      genNullifier(1),
		VoteCommitment: []byte{}, // empty
		Proof:          []byte("fake-proof"),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInvalidCommitment)
}

func TestSealedVote_NullifierAlreadyUsed(t *testing.T) {
	f := initTestFixture(t)

	f.registerVoter(t, f.member, genZkPubKey(1))
	proposalID := f.createSealedProposal(t, f.member)

	nullifier := genNullifier(1)
	commitment := keeper.ComputeCommitmentHashForTest(0, make([]byte, 32))

	// First sealed vote succeeds.
	_, err := f.msgServer.SealedVote(f.ctx, &types.MsgSealedVote{
		Submitter:      f.member,
		ProposalId:     proposalID,
		Nullifier:      nullifier,
		VoteCommitment: commitment,
		Proof:          []byte("fake-proof"),
	})
	require.NoError(t, err)

	// Second with same nullifier fails.
	_, err = f.msgServer.SealedVote(f.ctx, &types.MsgSealedVote{
		Submitter:      f.member,
		ProposalId:     proposalID,
		Nullifier:      nullifier,
		VoteCommitment: commitment,
		Proof:          []byte("fake-proof"),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrNullifierUsed)
}

func TestSealedVote_EncryptedRevealTooLarge(t *testing.T) {
	f := initTestFixture(t)

	f.registerVoter(t, f.member, genZkPubKey(1))
	proposalID := f.createSealedProposal(t, f.member)

	// Set MaxEncryptedRevealBytes to a small value.
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MaxEncryptedRevealBytes = 10
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	commitment := keeper.ComputeCommitmentHashForTest(0, make([]byte, 32))
	largeReveal := make([]byte, 50) // larger than 10

	_, err = f.msgServer.SealedVote(f.ctx, &types.MsgSealedVote{
		Submitter:       f.member,
		ProposalId:      proposalID,
		Nullifier:       genNullifier(1),
		VoteCommitment:  commitment,
		Proof:           []byte("fake-proof"),
		EncryptedReveal: largeReveal,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrEncryptedRevealTooLarge)
}

func TestSealedVote_EmitsEvent(t *testing.T) {
	f := initTestFixture(t)

	f.registerVoter(t, f.member, genZkPubKey(1))
	proposalID := f.createSealedProposal(t, f.member)

	commitment := keeper.ComputeCommitmentHashForTest(0, make([]byte, 32))

	_, err := f.msgServer.SealedVote(f.ctx, &types.MsgSealedVote{
		Submitter:       f.member,
		ProposalId:      proposalID,
		Nullifier:       genNullifier(1),
		VoteCommitment:  commitment,
		Proof:           []byte("fake-proof"),
		EncryptedReveal: []byte("encrypted"),
	})
	require.NoError(t, err)

	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	events := sdkCtx.EventManager().Events()

	found := false
	for _, e := range events {
		if e.Type == types.EventSealedVoteCast {
			found = true
			break
		}
	}
	require.True(t, found, "expected %s event to be emitted", types.EventSealedVoteCast)
}
