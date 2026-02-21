package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/vote/types"
)

func TestCancelProposal(t *testing.T) {
	t.Run("happy: proposer cancels ACTIVE proposal", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))
		proposalID := f.createPublicProposal(t, f.member)

		_, err := f.msgServer.CancelProposal(f.ctx, &types.MsgCancelProposal{
			Authority:  f.member,
			ProposalId: proposalID,
			Reason:     "changed my mind",
		})
		require.NoError(t, err)

		proposal, err := f.keeper.VotingProposal.Get(f.ctx, proposalID)
		require.NoError(t, err)
		require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_CANCELLED, proposal.Status)
	})

	t.Run("happy: governance cancels any proposal", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))
		proposalID := f.createPublicProposal(t, f.member)

		_, err := f.msgServer.CancelProposal(f.ctx, &types.MsgCancelProposal{
			Authority:  f.authority,
			ProposalId: proposalID,
			Reason:     "governance override",
		})
		require.NoError(t, err)

		proposal, err := f.keeper.VotingProposal.Get(f.ctx, proposalID)
		require.NoError(t, err)
		require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_CANCELLED, proposal.Status)
	})

	t.Run("happy: cancel TALLYING proposal", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))
		proposalID := f.createPublicProposal(t, f.member)

		// Manually set status to TALLYING.
		proposal, err := f.keeper.VotingProposal.Get(f.ctx, proposalID)
		require.NoError(t, err)
		proposal.Status = types.ProposalStatus_PROPOSAL_STATUS_TALLYING
		require.NoError(t, f.keeper.VotingProposal.Set(f.ctx, proposalID, proposal))

		_, err = f.msgServer.CancelProposal(f.ctx, &types.MsgCancelProposal{
			Authority:  f.member,
			ProposalId: proposalID,
			Reason:     "cancel during tally",
		})
		require.NoError(t, err)

		proposal, err = f.keeper.VotingProposal.Get(f.ctx, proposalID)
		require.NoError(t, err)
		require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_CANCELLED, proposal.Status)
	})

	t.Run("error: not found", func(t *testing.T) {
		f := initTestFixture(t)

		_, err := f.msgServer.CancelProposal(f.ctx, &types.MsgCancelProposal{
			Authority:  f.member,
			ProposalId: 999999,
			Reason:     "does not exist",
		})
		require.ErrorIs(t, err, types.ErrProposalNotFound)
	})

	t.Run("error: unauthorized (non-proposer, non-gov)", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))
		proposalID := f.createPublicProposal(t, f.member)

		// member2 is not the proposer and not governance authority.
		_, err := f.msgServer.CancelProposal(f.ctx, &types.MsgCancelProposal{
			Authority:  f.member2,
			ProposalId: proposalID,
			Reason:     "not my proposal",
		})
		require.ErrorIs(t, err, types.ErrCancelNotAuthorized)
	})

	t.Run("error: already FINALIZED", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))
		proposalID := f.createPublicProposal(t, f.member)

		// Directly set status to FINALIZED.
		proposal, err := f.keeper.VotingProposal.Get(f.ctx, proposalID)
		require.NoError(t, err)
		proposal.Status = types.ProposalStatus_PROPOSAL_STATUS_FINALIZED
		require.NoError(t, f.keeper.VotingProposal.Set(f.ctx, proposalID, proposal))

		_, err = f.msgServer.CancelProposal(f.ctx, &types.MsgCancelProposal{
			Authority:  f.member,
			ProposalId: proposalID,
			Reason:     "too late",
		})
		require.ErrorIs(t, err, types.ErrProposalNotCancellable)
	})

	t.Run("error: already CANCELLED", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))
		proposalID := f.createPublicProposal(t, f.member)

		// Directly set status to CANCELLED.
		proposal, err := f.keeper.VotingProposal.Get(f.ctx, proposalID)
		require.NoError(t, err)
		proposal.Status = types.ProposalStatus_PROPOSAL_STATUS_CANCELLED
		require.NoError(t, f.keeper.VotingProposal.Set(f.ctx, proposalID, proposal))

		_, err = f.msgServer.CancelProposal(f.ctx, &types.MsgCancelProposal{
			Authority:  f.member,
			ProposalId: proposalID,
			Reason:     "already cancelled",
		})
		require.ErrorIs(t, err, types.ErrProposalNotCancellable)
	})

	t.Run("event: proposal_cancelled emitted", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))
		proposalID := f.createPublicProposal(t, f.member)

		_, err := f.msgServer.CancelProposal(f.ctx, &types.MsgCancelProposal{
			Authority:  f.member,
			ProposalId: proposalID,
			Reason:     "test cancellation",
		})
		require.NoError(t, err)

		sdkCtx := sdk.UnwrapSDKContext(f.ctx)
		events := sdkCtx.EventManager().Events()
		found := false
		for _, e := range events {
			if e.Type == types.EventProposalCancelled {
				found = true
				for _, attr := range e.Attributes {
					if attr.Key == types.AttributeReason {
						require.Equal(t, "test cancellation", attr.Value)
					}
				}
			}
		}
		require.True(t, found, "expected proposal_cancelled event")
	})
}
