package types_test

import (
	"testing"

	"sparkdream/x/vote/types"

	"github.com/stretchr/testify/require"
)

func TestGenesisState_Validate(t *testing.T) {
	tests := []struct {
		desc     string
		genState *types.GenesisState
		valid    bool
	}{
		{
			desc:     "default is valid",
			genState: types.DefaultGenesis(),
			valid:    true,
		},
		{
			desc:     "valid genesis state",
			genState: &types.GenesisState{VotingProposalList: []types.VotingProposal{{Id: 0}, {Id: 1}}, VotingProposalCount: 2, VoterRegistrationMap: []types.VoterRegistration{{Address: "0"}, {Address: "1"}}, AnonymousVoteMap: []types.AnonymousVote{{Index: "0"}, {Index: "1"}}, SealedVoteMap: []types.SealedVote{{Index: "0"}, {Index: "1"}}, VoterTreeSnapshotMap: []types.VoterTreeSnapshot{{ProposalId: 0}, {ProposalId: 1}}, UsedNullifierMap: []types.UsedNullifier{{Index: "0"}, {Index: "1"}}, UsedProposalNullifierMap: []types.UsedProposalNullifier{{Index: "0"}, {Index: "1"}}, TleValidatorShareMap: []types.TleValidatorShare{{Validator: "0"}, {Validator: "1"}}, TleDecryptionShareMap: []types.TleDecryptionShare{{Index: "0"}, {Index: "1"}}, EpochDecryptionKeyMap: []types.EpochDecryptionKey{{Epoch: 0}, {Epoch: 1}}, SrsState: &types.SrsState{StoredAt: 54}}, valid: true,
		}, {
			desc: "duplicated votingProposal",
			genState: &types.GenesisState{
				VotingProposalList: []types.VotingProposal{
					{
						Id: 0,
					},
					{
						Id: 0,
					},
				},
				VoterRegistrationMap: []types.VoterRegistration{{Address: "0"}, {Address: "1"}}, AnonymousVoteMap: []types.AnonymousVote{{Index: "0"}, {Index: "1"}}, SealedVoteMap: []types.SealedVote{{Index: "0"}, {Index: "1"}}, VoterTreeSnapshotMap: []types.VoterTreeSnapshot{{ProposalId: 0}, {ProposalId: 1}}, UsedNullifierMap: []types.UsedNullifier{{Index: "0"}, {Index: "1"}}, UsedProposalNullifierMap: []types.UsedProposalNullifier{{Index: "0"}, {Index: "1"}}, TleValidatorShareMap: []types.TleValidatorShare{{Validator: "0"}, {Validator: "1"}}, TleDecryptionShareMap: []types.TleDecryptionShare{{Index: "0"}, {Index: "1"}}, EpochDecryptionKeyMap: []types.EpochDecryptionKey{{Epoch: 0}, {Epoch: 1}}, SrsState: &types.SrsState{StoredAt: 54}},
			valid: false,
		}, {
			desc: "invalid votingProposal count",
			genState: &types.GenesisState{
				VotingProposalList: []types.VotingProposal{
					{
						Id: 1,
					},
				},
				VotingProposalCount:  0,
				VoterRegistrationMap: []types.VoterRegistration{{Address: "0"}, {Address: "1"}}, AnonymousVoteMap: []types.AnonymousVote{{Index: "0"}, {Index: "1"}}, SealedVoteMap: []types.SealedVote{{Index: "0"}, {Index: "1"}}, VoterTreeSnapshotMap: []types.VoterTreeSnapshot{{ProposalId: 0}, {ProposalId: 1}}, UsedNullifierMap: []types.UsedNullifier{{Index: "0"}, {Index: "1"}}, UsedProposalNullifierMap: []types.UsedProposalNullifier{{Index: "0"}, {Index: "1"}}, TleValidatorShareMap: []types.TleValidatorShare{{Validator: "0"}, {Validator: "1"}}, TleDecryptionShareMap: []types.TleDecryptionShare{{Index: "0"}, {Index: "1"}}, EpochDecryptionKeyMap: []types.EpochDecryptionKey{{Epoch: 0}, {Epoch: 1}}, SrsState: &types.SrsState{StoredAt: 54}},
			valid: false,
		}, {
			desc: "duplicated voterRegistration",
			genState: &types.GenesisState{
				VoterRegistrationMap: []types.VoterRegistration{
					{
						Address: "0",
					},
					{
						Address: "0",
					},
				},
				AnonymousVoteMap: []types.AnonymousVote{{Index: "0"}, {Index: "1"}}, SealedVoteMap: []types.SealedVote{{Index: "0"}, {Index: "1"}}, VoterTreeSnapshotMap: []types.VoterTreeSnapshot{{ProposalId: 0}, {ProposalId: 1}}, UsedNullifierMap: []types.UsedNullifier{{Index: "0"}, {Index: "1"}}, UsedProposalNullifierMap: []types.UsedProposalNullifier{{Index: "0"}, {Index: "1"}}, TleValidatorShareMap: []types.TleValidatorShare{{Validator: "0"}, {Validator: "1"}}, TleDecryptionShareMap: []types.TleDecryptionShare{{Index: "0"}, {Index: "1"}}, EpochDecryptionKeyMap: []types.EpochDecryptionKey{{Epoch: 0}, {Epoch: 1}}, SrsState: &types.SrsState{StoredAt: 54}},
			valid: false,
		}, {
			desc: "duplicated anonymousVote",
			genState: &types.GenesisState{
				AnonymousVoteMap: []types.AnonymousVote{
					{
						Index: "0",
					},
					{
						Index: "0",
					},
				},
				SealedVoteMap: []types.SealedVote{{Index: "0"}, {Index: "1"}}, VoterTreeSnapshotMap: []types.VoterTreeSnapshot{{ProposalId: 0}, {ProposalId: 1}}, UsedNullifierMap: []types.UsedNullifier{{Index: "0"}, {Index: "1"}}, UsedProposalNullifierMap: []types.UsedProposalNullifier{{Index: "0"}, {Index: "1"}}, TleValidatorShareMap: []types.TleValidatorShare{{Validator: "0"}, {Validator: "1"}}, TleDecryptionShareMap: []types.TleDecryptionShare{{Index: "0"}, {Index: "1"}}, EpochDecryptionKeyMap: []types.EpochDecryptionKey{{Epoch: 0}, {Epoch: 1}}, SrsState: &types.SrsState{StoredAt: 54}},
			valid: false,
		}, {
			desc: "duplicated sealedVote",
			genState: &types.GenesisState{
				SealedVoteMap: []types.SealedVote{
					{
						Index: "0",
					},
					{
						Index: "0",
					},
				},
				VoterTreeSnapshotMap: []types.VoterTreeSnapshot{{ProposalId: 0}, {ProposalId: 1}}, UsedNullifierMap: []types.UsedNullifier{{Index: "0"}, {Index: "1"}}, UsedProposalNullifierMap: []types.UsedProposalNullifier{{Index: "0"}, {Index: "1"}}, TleValidatorShareMap: []types.TleValidatorShare{{Validator: "0"}, {Validator: "1"}}, TleDecryptionShareMap: []types.TleDecryptionShare{{Index: "0"}, {Index: "1"}}, EpochDecryptionKeyMap: []types.EpochDecryptionKey{{Epoch: 0}, {Epoch: 1}}, SrsState: &types.SrsState{StoredAt: 54}},
			valid: false,
		}, {
			desc: "duplicated voterTreeSnapshot",
			genState: &types.GenesisState{
				VoterTreeSnapshotMap: []types.VoterTreeSnapshot{
					{
						ProposalId: 0,
					},
					{
						ProposalId: 0,
					},
				},
				UsedNullifierMap: []types.UsedNullifier{{Index: "0"}, {Index: "1"}}, UsedProposalNullifierMap: []types.UsedProposalNullifier{{Index: "0"}, {Index: "1"}}, TleValidatorShareMap: []types.TleValidatorShare{{Validator: "0"}, {Validator: "1"}}, TleDecryptionShareMap: []types.TleDecryptionShare{{Index: "0"}, {Index: "1"}}, EpochDecryptionKeyMap: []types.EpochDecryptionKey{{Epoch: 0}, {Epoch: 1}}, SrsState: &types.SrsState{StoredAt: 54}},
			valid: false,
		}, {
			desc: "duplicated usedNullifier",
			genState: &types.GenesisState{
				UsedNullifierMap: []types.UsedNullifier{
					{
						Index: "0",
					},
					{
						Index: "0",
					},
				},
				UsedProposalNullifierMap: []types.UsedProposalNullifier{{Index: "0"}, {Index: "1"}}, TleValidatorShareMap: []types.TleValidatorShare{{Validator: "0"}, {Validator: "1"}}, TleDecryptionShareMap: []types.TleDecryptionShare{{Index: "0"}, {Index: "1"}}, EpochDecryptionKeyMap: []types.EpochDecryptionKey{{Epoch: 0}, {Epoch: 1}}, SrsState: &types.SrsState{StoredAt: 54}},
			valid: false,
		}, {
			desc: "duplicated usedProposalNullifier",
			genState: &types.GenesisState{
				UsedProposalNullifierMap: []types.UsedProposalNullifier{
					{
						Index: "0",
					},
					{
						Index: "0",
					},
				},
				TleValidatorShareMap: []types.TleValidatorShare{{Validator: "0"}, {Validator: "1"}}, TleDecryptionShareMap: []types.TleDecryptionShare{{Index: "0"}, {Index: "1"}}, EpochDecryptionKeyMap: []types.EpochDecryptionKey{{Epoch: 0}, {Epoch: 1}}, SrsState: &types.SrsState{StoredAt: 54}},
			valid: false,
		}, {
			desc: "duplicated tleValidatorShare",
			genState: &types.GenesisState{
				TleValidatorShareMap: []types.TleValidatorShare{
					{
						Validator: "0",
					},
					{
						Validator: "0",
					},
				},
				TleDecryptionShareMap: []types.TleDecryptionShare{{Index: "0"}, {Index: "1"}}, EpochDecryptionKeyMap: []types.EpochDecryptionKey{{Epoch: 0}, {Epoch: 1}}, SrsState: &types.SrsState{StoredAt: 54}},
			valid: false,
		}, {
			desc: "duplicated tleDecryptionShare",
			genState: &types.GenesisState{
				TleDecryptionShareMap: []types.TleDecryptionShare{
					{
						Index: "0",
					},
					{
						Index: "0",
					},
				},
				EpochDecryptionKeyMap: []types.EpochDecryptionKey{{Epoch: 0}, {Epoch: 1}}, SrsState: &types.SrsState{StoredAt: 54}},
			valid: false,
		}, {
			desc: "duplicated epochDecryptionKey",
			genState: &types.GenesisState{
				EpochDecryptionKeyMap: []types.EpochDecryptionKey{
					{
						Epoch: 0,
					},
					{
						Epoch: 0,
					},
				},
				SrsState: &types.SrsState{StoredAt: 54}},
			valid: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			err := tc.genState.Validate()
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
