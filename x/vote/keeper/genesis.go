package keeper

import (
	"context"
	"errors"

	"sparkdream/x/vote/types"

	"cosmossdk.io/collections"
)

// InitGenesis initializes the module's state from a provided genesis state.
func (k Keeper) InitGenesis(ctx context.Context, genState types.GenesisState) error {
	for _, elem := range genState.VotingProposalList {
		if err := k.VotingProposal.Set(ctx, elem.Id, elem); err != nil {
			return err
		}
	}

	if err := k.VotingProposalSeq.Set(ctx, genState.VotingProposalCount);

	// ExportGenesis returns the module's exported genesis.
	err != nil {
		return err
	}
	for _, elem := range genState.VoterRegistrationMap {
		if err := k.VoterRegistration.Set(ctx, elem.Address, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.AnonymousVoteMap {
		if err := k.AnonymousVote.Set(ctx, elem.Index, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.SealedVoteMap {
		if err := k.SealedVote.Set(ctx, elem.Index, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.VoterTreeSnapshotMap {
		if err := k.VoterTreeSnapshot.Set(ctx, elem.ProposalId, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.UsedNullifierMap {
		if err := k.UsedNullifier.Set(ctx, elem.Index, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.UsedProposalNullifierMap {
		if err := k.UsedProposalNullifier.Set(ctx, elem.Index, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.TleValidatorShareMap {
		if err := k.TleValidatorShare.Set(ctx, elem.Validator, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.TleDecryptionShareMap {
		if err := k.TleDecryptionShare.Set(ctx, elem.Index, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.EpochDecryptionKeyMap {
		if err := k.EpochDecryptionKey.Set(ctx, elem.Epoch, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.TleEpochParticipationMap {
		if err := k.TleEpochParticipation.Set(ctx, elem.Epoch, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.TleValidatorLivenessMap {
		if err := k.TleValidatorLiveness.Set(ctx, elem.Validator, elem); err != nil {
			return err
		}
	}
	if genState.SrsState != nil {
		if err := k.SrsState.Set(ctx, *genState.SrsState); err != nil {
			return err
		}
	}

	return k.Params.Set(ctx, genState.Params)
}

func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	var err error

	genesis := types.DefaultGenesis()
	genesis.Params, err = k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	err = k.VotingProposal.Walk(ctx, nil, func(key uint64, elem types.VotingProposal) (bool, error) {
		genesis.VotingProposalList = append(genesis.VotingProposalList, elem)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	genesis.VotingProposalCount, err = k.VotingProposalSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}
	if err := k.VoterRegistration.Walk(ctx, nil, func(_ string, val types.VoterRegistration) (stop bool, err error) {
		genesis.VoterRegistrationMap = append(genesis.VoterRegistrationMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.AnonymousVote.Walk(ctx, nil, func(_ string, val types.AnonymousVote) (stop bool, err error) {
		genesis.AnonymousVoteMap = append(genesis.AnonymousVoteMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.SealedVote.Walk(ctx, nil, func(_ string, val types.SealedVote) (stop bool, err error) {
		genesis.SealedVoteMap = append(genesis.SealedVoteMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.VoterTreeSnapshot.Walk(ctx, nil, func(_ uint64, val types.VoterTreeSnapshot) (stop bool, err error) {
		genesis.VoterTreeSnapshotMap = append(genesis.VoterTreeSnapshotMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.UsedNullifier.Walk(ctx, nil, func(_ string, val types.UsedNullifier) (stop bool, err error) {
		genesis.UsedNullifierMap = append(genesis.UsedNullifierMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.UsedProposalNullifier.Walk(ctx, nil, func(_ string, val types.UsedProposalNullifier) (stop bool, err error) {
		genesis.UsedProposalNullifierMap = append(genesis.UsedProposalNullifierMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.TleValidatorShare.Walk(ctx, nil, func(_ string, val types.TleValidatorShare) (stop bool, err error) {
		genesis.TleValidatorShareMap = append(genesis.TleValidatorShareMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.TleDecryptionShare.Walk(ctx, nil, func(_ string, val types.TleDecryptionShare) (stop bool, err error) {
		genesis.TleDecryptionShareMap = append(genesis.TleDecryptionShareMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.EpochDecryptionKey.Walk(ctx, nil, func(_ uint64, val types.EpochDecryptionKey) (stop bool, err error) {
		genesis.EpochDecryptionKeyMap = append(genesis.EpochDecryptionKeyMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.TleEpochParticipation.Walk(ctx, nil, func(_ uint64, val types.TleEpochParticipation) (stop bool, err error) {
		genesis.TleEpochParticipationMap = append(genesis.TleEpochParticipationMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.TleValidatorLiveness.Walk(ctx, nil, func(_ string, val types.TleValidatorLiveness) (stop bool, err error) {
		genesis.TleValidatorLivenessMap = append(genesis.TleValidatorLivenessMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	srsState, err := k.SrsState.Get(ctx)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return nil, err
	}
	genesis.SrsState = &srsState

	return genesis, nil
}
