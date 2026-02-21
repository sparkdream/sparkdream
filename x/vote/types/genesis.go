package types

import "fmt"

// DefaultGenesis returns the default genesis state
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:             DefaultParams(),
		VotingProposalList: []VotingProposal{}, VoterRegistrationMap: []VoterRegistration{}, AnonymousVoteMap: []AnonymousVote{}, SealedVoteMap: []SealedVote{}, VoterTreeSnapshotMap: []VoterTreeSnapshot{}, UsedNullifierMap: []UsedNullifier{}, UsedProposalNullifierMap: []UsedProposalNullifier{}, TleValidatorShareMap: []TleValidatorShare{}, TleDecryptionShareMap: []TleDecryptionShare{}, EpochDecryptionKeyMap: []EpochDecryptionKey{}, TleEpochParticipationMap: []TleEpochParticipation{}, TleValidatorLivenessMap: []TleValidatorLiveness{}, SrsState: nil}
}

// Validate performs basic genesis state validation returning an error upon any
// failure.
func (gs GenesisState) Validate() error {
	votingProposalIdMap := make(map[uint64]bool)
	votingProposalCount := gs.GetVotingProposalCount()
	for _, elem := range gs.VotingProposalList {
		if _, ok := votingProposalIdMap[elem.Id]; ok {
			return fmt.Errorf("duplicated id for votingProposal")
		}
		if elem.Id >= votingProposalCount {
			return fmt.Errorf("votingProposal id should be lower or equal than the last id")
		}
		votingProposalIdMap[elem.Id] = true
	}
	voterRegistrationIndexMap := make(map[string]struct{})

	for _, elem := range gs.VoterRegistrationMap {
		index := fmt.Sprint(elem.Address)
		if _, ok := voterRegistrationIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for voterRegistration")
		}
		voterRegistrationIndexMap[index] = struct{}{}
	}
	anonymousVoteIndexMap := make(map[string]struct{})

	for _, elem := range gs.AnonymousVoteMap {
		index := fmt.Sprint(elem.Index)
		if _, ok := anonymousVoteIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for anonymousVote")
		}
		anonymousVoteIndexMap[index] = struct{}{}
	}
	sealedVoteIndexMap := make(map[string]struct{})

	for _, elem := range gs.SealedVoteMap {
		index := fmt.Sprint(elem.Index)
		if _, ok := sealedVoteIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for sealedVote")
		}
		sealedVoteIndexMap[index] = struct{}{}
	}
	voterTreeSnapshotIndexMap := make(map[string]struct{})

	for _, elem := range gs.VoterTreeSnapshotMap {
		index := fmt.Sprint(elem.ProposalId)
		if _, ok := voterTreeSnapshotIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for voterTreeSnapshot")
		}
		voterTreeSnapshotIndexMap[index] = struct{}{}
	}
	usedNullifierIndexMap := make(map[string]struct{})

	for _, elem := range gs.UsedNullifierMap {
		index := fmt.Sprint(elem.Index)
		if _, ok := usedNullifierIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for usedNullifier")
		}
		usedNullifierIndexMap[index] = struct{}{}
	}
	usedProposalNullifierIndexMap := make(map[string]struct{})

	for _, elem := range gs.UsedProposalNullifierMap {
		index := fmt.Sprint(elem.Index)
		if _, ok := usedProposalNullifierIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for usedProposalNullifier")
		}
		usedProposalNullifierIndexMap[index] = struct{}{}
	}
	tleValidatorShareIndexMap := make(map[string]struct{})

	for _, elem := range gs.TleValidatorShareMap {
		index := fmt.Sprint(elem.Validator)
		if _, ok := tleValidatorShareIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for tleValidatorShare")
		}
		tleValidatorShareIndexMap[index] = struct{}{}
	}
	tleDecryptionShareIndexMap := make(map[string]struct{})

	for _, elem := range gs.TleDecryptionShareMap {
		index := fmt.Sprint(elem.Index)
		if _, ok := tleDecryptionShareIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for tleDecryptionShare")
		}
		tleDecryptionShareIndexMap[index] = struct{}{}
	}
	epochDecryptionKeyIndexMap := make(map[string]struct{})

	for _, elem := range gs.EpochDecryptionKeyMap {
		index := fmt.Sprint(elem.Epoch)
		if _, ok := epochDecryptionKeyIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for epochDecryptionKey")
		}
		epochDecryptionKeyIndexMap[index] = struct{}{}
	}
	tleEpochParticipationIndexMap := make(map[string]struct{})

	for _, elem := range gs.TleEpochParticipationMap {
		index := fmt.Sprint(elem.Epoch)
		if _, ok := tleEpochParticipationIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for tleEpochParticipation")
		}
		tleEpochParticipationIndexMap[index] = struct{}{}
	}
	tleValidatorLivenessIndexMap := make(map[string]struct{})

	for _, elem := range gs.TleValidatorLivenessMap {
		index := elem.Validator
		if _, ok := tleValidatorLivenessIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for tleValidatorLiveness")
		}
		tleValidatorLivenessIndexMap[index] = struct{}{}
	}

	return gs.Params.Validate()
}
