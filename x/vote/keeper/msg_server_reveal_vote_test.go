package keeper_test

import (
	"testing"

	"sparkdream/x/vote/keeper"
	"sparkdream/x/vote/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// setupSealedVoteForReveal creates a sealed proposal, submits a sealed vote with
// the given option and salt, and transitions the proposal to TALLYING status.
// Returns the proposalID and nullifier used.
func setupSealedVoteForReveal(t *testing.T, f *testFixture, voteOption uint32, salt []byte) (uint64, []byte) {
	t.Helper()

	f.registerVoter(t, f.member, genZkPubKey(1))
	proposalID := f.createSealedProposal(t, f.member)

	nullifier := genNullifier(1)
	commitment := keeper.ComputeCommitmentHashForTest(voteOption, salt)

	_, err := f.msgServer.SealedVote(f.ctx, &types.MsgSealedVote{
		Submitter:       f.member,
		ProposalId:      proposalID,
		Nullifier:       nullifier,
		VoteCommitment:  commitment,
		Proof:           []byte("fake-proof"),
		EncryptedReveal: []byte("encrypted"),
	})
	require.NoError(t, err)

	// Set proposal status to TALLYING.
	proposal, err := f.keeper.VotingProposal.Get(f.ctx, proposalID)
	require.NoError(t, err)
	proposal.Status = types.ProposalStatus_PROPOSAL_STATUS_TALLYING
	require.NoError(t, f.keeper.VotingProposal.Set(f.ctx, proposalID, proposal))

	return proposalID, nullifier
}

func TestRevealVote_HappyPath(t *testing.T) {
	f := initTestFixture(t)

	salt := make([]byte, 32)
	for i := range salt {
		salt[i] = byte(i)
	}
	proposalID, nullifier := setupSealedVoteForReveal(t, f, 0, salt)

	_, err := f.msgServer.RevealVote(f.ctx, &types.MsgRevealVote{
		Submitter:  f.member,
		ProposalId: proposalID,
		Nullifier:  nullifier,
		VoteOption: 0,
		RevealSalt: salt,
	})
	require.NoError(t, err)

	// Verify tally updated.
	proposal, err := f.keeper.VotingProposal.Get(f.ctx, proposalID)
	require.NoError(t, err)
	require.Equal(t, uint64(1), proposal.Tally[0].VoteCount)

	// Verify sealed vote marked as revealed.
	voteKey := keeper.NullifierKeyForTest(proposalID, nullifier)
	sv, err := f.keeper.SealedVote.Get(f.ctx, voteKey)
	require.NoError(t, err)
	require.True(t, sv.Revealed)
	require.Equal(t, uint32(0), sv.RevealedOption)
}

func TestRevealVote_ProposalNotFound(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.msgServer.RevealVote(f.ctx, &types.MsgRevealVote{
		Submitter:  f.member,
		ProposalId: 999,
		Nullifier:  genNullifier(1),
		VoteOption: 0,
		RevealSalt: make([]byte, 32),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrProposalNotFound)
}

func TestRevealVote_ProposalNotTallying(t *testing.T) {
	f := initTestFixture(t)

	f.registerVoter(t, f.member, genZkPubKey(1))
	proposalID := f.createSealedProposal(t, f.member)

	salt := make([]byte, 32)
	commitment := keeper.ComputeCommitmentHashForTest(0, salt)
	nullifier := genNullifier(1)

	_, err := f.msgServer.SealedVote(f.ctx, &types.MsgSealedVote{
		Submitter:      f.member,
		ProposalId:     proposalID,
		Nullifier:      nullifier,
		VoteCommitment: commitment,
		Proof:          []byte("fake-proof"),
	})
	require.NoError(t, err)

	// Proposal is still ACTIVE, not TALLYING.
	_, err = f.msgServer.RevealVote(f.ctx, &types.MsgRevealVote{
		Submitter:  f.member,
		ProposalId: proposalID,
		Nullifier:  nullifier,
		VoteOption: 0,
		RevealSalt: salt,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrProposalNotTallying)
}

func TestRevealVote_VoteNotFound(t *testing.T) {
	f := initTestFixture(t)

	salt := make([]byte, 32)
	proposalID, _ := setupSealedVoteForReveal(t, f, 0, salt)

	// Use a different nullifier that was not used for voting.
	wrongNullifier := genNullifier(99)
	_, err := f.msgServer.RevealVote(f.ctx, &types.MsgRevealVote{
		Submitter:  f.member,
		ProposalId: proposalID,
		Nullifier:  wrongNullifier,
		VoteOption: 0,
		RevealSalt: salt,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrVoteNotFound)
}

func TestRevealVote_AlreadyRevealed(t *testing.T) {
	f := initTestFixture(t)

	salt := make([]byte, 32)
	for i := range salt {
		salt[i] = byte(i)
	}
	proposalID, nullifier := setupSealedVoteForReveal(t, f, 0, salt)

	// Reveal once.
	_, err := f.msgServer.RevealVote(f.ctx, &types.MsgRevealVote{
		Submitter:  f.member,
		ProposalId: proposalID,
		Nullifier:  nullifier,
		VoteOption: 0,
		RevealSalt: salt,
	})
	require.NoError(t, err)

	// Reveal again.
	_, err = f.msgServer.RevealVote(f.ctx, &types.MsgRevealVote{
		Submitter:  f.member,
		ProposalId: proposalID,
		Nullifier:  nullifier,
		VoteOption: 0,
		RevealSalt: salt,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrAlreadyRevealed)
}

func TestRevealVote_CommitmentMismatch(t *testing.T) {
	f := initTestFixture(t)

	salt := make([]byte, 32)
	for i := range salt {
		salt[i] = byte(i)
	}
	proposalID, nullifier := setupSealedVoteForReveal(t, f, 0, salt)

	// Reveal with a wrong salt.
	wrongSalt := make([]byte, 32)
	for i := range wrongSalt {
		wrongSalt[i] = byte(i + 100)
	}

	_, err := f.msgServer.RevealVote(f.ctx, &types.MsgRevealVote{
		Submitter:  f.member,
		ProposalId: proposalID,
		Nullifier:  nullifier,
		VoteOption: 0,
		RevealSalt: wrongSalt,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrRevealMismatch)
}

func TestRevealVote_VoteOptionOutOfRange(t *testing.T) {
	f := initTestFixture(t)

	salt := make([]byte, 32)
	for i := range salt {
		salt[i] = byte(i)
	}

	// Commit to option 5 (out of range for 2-option proposal) with matching salt.
	// The commitment will be for option 5, and the reveal will match the commitment,
	// but the option is out of range for the proposal.
	f.registerVoter(t, f.member, genZkPubKey(1))
	proposalID := f.createSealedProposal(t, f.member)

	commitment := keeper.ComputeCommitmentHashForTest(5, salt)
	nullifier := genNullifier(1)

	_, err := f.msgServer.SealedVote(f.ctx, &types.MsgSealedVote{
		Submitter:      f.member,
		ProposalId:     proposalID,
		Nullifier:      nullifier,
		VoteCommitment: commitment,
		Proof:          []byte("fake-proof"),
	})
	require.NoError(t, err)

	// Transition to TALLYING.
	proposal, err := f.keeper.VotingProposal.Get(f.ctx, proposalID)
	require.NoError(t, err)
	proposal.Status = types.ProposalStatus_PROPOSAL_STATUS_TALLYING
	require.NoError(t, f.keeper.VotingProposal.Set(f.ctx, proposalID, proposal))

	_, err = f.msgServer.RevealVote(f.ctx, &types.MsgRevealVote{
		Submitter:  f.member,
		ProposalId: proposalID,
		Nullifier:  nullifier,
		VoteOption: 5,
		RevealSalt: salt,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrVoteOptionOutOfRange)
}

func TestRevealVote_EmitsEvent(t *testing.T) {
	f := initTestFixture(t)

	salt := make([]byte, 32)
	for i := range salt {
		salt[i] = byte(i)
	}
	proposalID, nullifier := setupSealedVoteForReveal(t, f, 0, salt)

	_, err := f.msgServer.RevealVote(f.ctx, &types.MsgRevealVote{
		Submitter:  f.member,
		ProposalId: proposalID,
		Nullifier:  nullifier,
		VoteOption: 0,
		RevealSalt: salt,
	})
	require.NoError(t, err)

	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	events := sdkCtx.EventManager().Events()

	found := false
	for _, e := range events {
		if e.Type == types.EventSealedVoteRevealed {
			found = true
			break
		}
	}
	require.True(t, found, "expected %s event to be emitted", types.EventSealedVoteRevealed)
}
