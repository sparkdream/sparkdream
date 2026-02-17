package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/reveal/types"
)

// InitGenesis initializes the module's state from a provided genesis state.
func (k Keeper) InitGenesis(ctx context.Context, genState types.GenesisState) error {
	if err := k.Params.Set(ctx, genState.Params); err != nil {
		return err
	}

	// Set sequence counters
	if err := k.ContributionSeq.Set(ctx, genState.NextContributionId); err != nil {
		return err
	}
	if err := k.StakeSeq.Set(ctx, genState.NextStakeId); err != nil {
		return err
	}

	// Import contributions and their indexes
	for _, contrib := range genState.Contributions {
		if err := k.Contribution.Set(ctx, contrib.Id, contrib); err != nil {
			return err
		}
		if err := k.ContributionsByStatus.Set(ctx, collections.Join(int32(contrib.Status), contrib.Id)); err != nil {
			return err
		}
		if err := k.ContributionsByContributor.Set(ctx, collections.Join(contrib.Contributor, contrib.Id)); err != nil {
			return err
		}
	}

	// Import stakes and their indexes
	for _, stake := range genState.Stakes {
		if err := k.RevealStake.Set(ctx, stake.Id, stake); err != nil {
			return err
		}
		trancheKey := TrancheKey(stake.ContributionId, stake.TrancheId)
		if err := k.StakesByTranche.Set(ctx, collections.Join(trancheKey, stake.Id)); err != nil {
			return err
		}
		if err := k.StakesByStaker.Set(ctx, collections.Join(stake.Staker, stake.Id)); err != nil {
			return err
		}
	}

	// Import votes and their indexes
	for _, vote := range genState.Votes {
		vk := VoteKey(vote.ContributionId, vote.TrancheId, vote.Voter)
		if err := k.Vote.Set(ctx, vk, vote); err != nil {
			return err
		}
		trancheKey := TrancheKey(vote.ContributionId, vote.TrancheId)
		if err := k.VotesByTranche.Set(ctx, collections.Join(trancheKey, vk)); err != nil {
			return err
		}
		if err := k.VotesByVoter.Set(ctx, collections.Join(vote.Voter, vk)); err != nil {
			return err
		}
	}

	return nil
}

// ExportGenesis returns the module's exported genesis.
func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	genesis := &types.GenesisState{
		Params: params,
	}

	// Export contributions
	err = k.Contribution.Walk(ctx, nil, func(id uint64, contrib types.Contribution) (bool, error) {
		genesis.Contributions = append(genesis.Contributions, contrib)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	// Export stakes
	err = k.RevealStake.Walk(ctx, nil, func(id uint64, stake types.RevealStake) (bool, error) {
		genesis.Stakes = append(genesis.Stakes, stake)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	// Export votes
	err = k.Vote.Walk(ctx, nil, func(key string, vote types.VerificationVote) (bool, error) {
		genesis.Votes = append(genesis.Votes, vote)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	// Export sequence counters
	nextContribID, err := k.ContributionSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}
	genesis.NextContributionId = nextContribID

	nextStakeID, err := k.StakeSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}
	genesis.NextStakeId = nextStakeID

	return genesis, nil
}
