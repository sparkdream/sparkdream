package keeper_test

import (
	"testing"

	"sparkdream/x/vote/types"

	"github.com/stretchr/testify/require"
)

func TestGenesis(t *testing.T) {
	genesisState := types.GenesisState{
		Params:               types.DefaultParams(),
		VotingProposalList:   []types.VotingProposal{{Id: 0}, {Id: 1}},
		VotingProposalCount:  2,
		VoterRegistrationMap: []types.VoterRegistration{{Address: "0"}, {Address: "1"}}, AnonymousVoteMap: []types.AnonymousVote{{Index: "0"}, {Index: "1"}}, SealedVoteMap: []types.SealedVote{{Index: "0"}, {Index: "1"}}, VoterTreeSnapshotMap: []types.VoterTreeSnapshot{{ProposalId: 0}, {ProposalId: 1}}, UsedNullifierMap: []types.UsedNullifier{{Index: "0"}, {Index: "1"}}, UsedProposalNullifierMap: []types.UsedProposalNullifier{{Index: "0"}, {Index: "1"}}, TleValidatorShareMap: []types.TleValidatorShare{{Validator: "0"}, {Validator: "1"}}, TleDecryptionShareMap: []types.TleDecryptionShare{{Index: "0"}, {Index: "1"}}, EpochDecryptionKeyMap: []types.EpochDecryptionKey{{Epoch: 0}, {Epoch: 1}}, SrsState: &types.SrsState{StoredAt: 58}}
	f := initFixture(t)
	err := f.keeper.InitGenesis(f.ctx, genesisState)
	require.NoError(t, err)
	got, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NotNil(t, got)

	require.EqualExportedValues(t, genesisState.Params, got.Params)
	require.EqualExportedValues(t, genesisState.VotingProposalList, got.VotingProposalList)
	require.Equal(t, genesisState.VotingProposalCount, got.VotingProposalCount)
	require.EqualExportedValues(t, genesisState.VoterRegistrationMap, got.VoterRegistrationMap)
	require.EqualExportedValues(t, genesisState.AnonymousVoteMap, got.AnonymousVoteMap)
	require.EqualExportedValues(t, genesisState.SealedVoteMap, got.SealedVoteMap)
	require.EqualExportedValues(t, genesisState.VoterTreeSnapshotMap, got.VoterTreeSnapshotMap)
	require.EqualExportedValues(t, genesisState.UsedNullifierMap, got.UsedNullifierMap)
	require.EqualExportedValues(t, genesisState.UsedProposalNullifierMap, got.UsedProposalNullifierMap)
	require.EqualExportedValues(t, genesisState.TleValidatorShareMap, got.TleValidatorShareMap)
	require.EqualExportedValues(t, genesisState.TleDecryptionShareMap, got.TleDecryptionShareMap)
	require.EqualExportedValues(t, genesisState.EpochDecryptionKeyMap, got.EpochDecryptionKeyMap)
	require.EqualExportedValues(t, genesisState.SrsState, got.SrsState)

}
