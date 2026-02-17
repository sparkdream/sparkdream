package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/reveal/types"
)

func TestMsgReveal_Success(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, contribID)
	f.stakeOnTranche(t, contribID, 0, f.staker, 1000)

	_, err := f.msgServer.Reveal(f.ctx, &types.MsgReveal{
		Contributor:    f.contributor,
		ContributionId: contribID,
		TrancheId:      0,
		CodeUri:        "ipfs://code",
		DocsUri:        "ipfs://docs",
		CommitHash:     "abc123",
	})
	require.NoError(t, err)

	contrib, err := f.keeper.Contribution.Get(f.ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, types.TrancheStatus_TRANCHE_STATUS_REVEALED, contrib.Tranches[0].Status)
	require.Equal(t, "ipfs://code", contrib.Tranches[0].CodeUri)
	require.Equal(t, "ipfs://docs", contrib.Tranches[0].DocsUri)
	require.Equal(t, "abc123", contrib.Tranches[0].CommitHash)
	require.True(t, contrib.Tranches[0].VerificationDeadline > 0)
}

func TestMsgReveal_NotContributor(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, contribID)
	f.stakeOnTranche(t, contribID, 0, f.staker, 1000)

	_, err := f.msgServer.Reveal(f.ctx, &types.MsgReveal{
		Contributor:    f.staker, // not the contributor
		ContributionId: contribID,
		TrancheId:      0,
		CodeUri:        "ipfs://code",
	})
	require.ErrorIs(t, err, types.ErrNotContributor)
}

func TestMsgReveal_NotBacked(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 10000)
	f.approveContribution(t, contribID)
	// Don't stake enough to reach BACKED

	_, err := f.msgServer.Reveal(f.ctx, &types.MsgReveal{
		Contributor:    f.contributor,
		ContributionId: contribID,
		TrancheId:      0,
		CodeUri:        "ipfs://code",
	})
	require.ErrorIs(t, err, types.ErrTrancheNotBacked)
}

func TestMsgReveal_NotInProgress(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 1000)
	// Still PROPOSED, not approved

	_, err := f.msgServer.Reveal(f.ctx, &types.MsgReveal{
		Contributor:    f.contributor,
		ContributionId: contribID,
		TrancheId:      0,
		CodeUri:        "ipfs://code",
	})
	require.ErrorIs(t, err, types.ErrNotInProgress)
}
