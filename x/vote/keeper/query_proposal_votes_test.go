package keeper_test

import (
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/vote/types"
)

func TestQueryProposalVotes(t *testing.T) {
	t.Run("anonymous votes only", func(t *testing.T) {
		f := initTestFixture(t)

		proposalID := uint64(1)
		nullifier1 := genNullifier(1)
		nullifier2 := genNullifier(2)
		key1 := fmt.Sprintf("%d/%s", proposalID, hex.EncodeToString(nullifier1))
		key2 := fmt.Sprintf("%d/%s", proposalID, hex.EncodeToString(nullifier2))

		vote1 := types.AnonymousVote{
			Index:       key1,
			ProposalId:  proposalID,
			Nullifier:   nullifier1,
			VoteOption:  0,
			SubmittedAt: 100,
		}
		vote2 := types.AnonymousVote{
			Index:       key2,
			ProposalId:  proposalID,
			Nullifier:   nullifier2,
			VoteOption:  1,
			SubmittedAt: 101,
		}
		require.NoError(t, f.keeper.AnonymousVote.Set(f.ctx, key1, vote1))
		require.NoError(t, f.keeper.AnonymousVote.Set(f.ctx, key2, vote2))

		resp, err := f.queryServer.ProposalVotes(f.ctx, &types.QueryProposalVotesRequest{
			ProposalId: proposalID,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Len(t, resp.Votes, 2)
		require.Empty(t, resp.SealedVotes)
	})

	t.Run("sealed votes only", func(t *testing.T) {
		f := initTestFixture(t)

		proposalID := uint64(2)
		nullifier1 := genNullifier(10)
		key1 := fmt.Sprintf("%d/%s", proposalID, hex.EncodeToString(nullifier1))

		sv := types.SealedVote{
			Index:          key1,
			ProposalId:     proposalID,
			Nullifier:      nullifier1,
			VoteCommitment: []byte("commitment"),
			SubmittedAt:    200,
		}
		require.NoError(t, f.keeper.SealedVote.Set(f.ctx, key1, sv))

		resp, err := f.queryServer.ProposalVotes(f.ctx, &types.QueryProposalVotesRequest{
			ProposalId: proposalID,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Empty(t, resp.Votes)
		require.Len(t, resp.SealedVotes, 1)
		require.Equal(t, proposalID, resp.SealedVotes[0].ProposalId)
	})

	t.Run("both anonymous and sealed votes", func(t *testing.T) {
		f := initTestFixture(t)

		proposalID := uint64(3)
		nullifierAnon := genNullifier(20)
		nullifierSealed := genNullifier(21)
		anonKey := fmt.Sprintf("%d/%s", proposalID, hex.EncodeToString(nullifierAnon))
		sealedKey := fmt.Sprintf("%d/%s", proposalID, hex.EncodeToString(nullifierSealed))

		anonVote := types.AnonymousVote{
			Index:       anonKey,
			ProposalId:  proposalID,
			Nullifier:   nullifierAnon,
			VoteOption:  0,
			SubmittedAt: 300,
		}
		sealedVote := types.SealedVote{
			Index:          sealedKey,
			ProposalId:     proposalID,
			Nullifier:      nullifierSealed,
			VoteCommitment: []byte("sealed-commitment"),
			SubmittedAt:    301,
		}
		require.NoError(t, f.keeper.AnonymousVote.Set(f.ctx, anonKey, anonVote))
		require.NoError(t, f.keeper.SealedVote.Set(f.ctx, sealedKey, sealedVote))

		resp, err := f.queryServer.ProposalVotes(f.ctx, &types.QueryProposalVotesRequest{
			ProposalId: proposalID,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Len(t, resp.Votes, 1)
		require.Len(t, resp.SealedVotes, 1)
	})

	t.Run("empty: proposal with no votes", func(t *testing.T) {
		f := initTestFixture(t)

		resp, err := f.queryServer.ProposalVotes(f.ctx, &types.QueryProposalVotesRequest{
			ProposalId: 999,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Empty(t, resp.Votes)
		require.Empty(t, resp.SealedVotes)
	})

	t.Run("nil request", func(t *testing.T) {
		f := initTestFixture(t)

		_, err := f.queryServer.ProposalVotes(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})
}
