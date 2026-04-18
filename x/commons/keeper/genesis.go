package keeper

import (
	"context"

	"sparkdream/x/commons/types"

	"cosmossdk.io/collections"
)

// InitGenesis initializes the module's state from a provided genesis state.
func (k Keeper) InitGenesis(ctx context.Context, genState types.GenesisState) error {
	// 1. Set Params
	if err := k.Params.Set(ctx, genState.Params); err != nil {
		panic(err)
	}

	// 2. Bootstrap the governance groups
	k.BootstrapGovernance(ctx)

	// 3. Restore existing collections
	for _, elem := range genState.PolicyPermissionsMap {
		if err := k.PolicyPermissions.Set(ctx, elem.PolicyAddress, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.GroupMap {
		if err := k.Groups.Set(ctx, elem.Index, elem); err != nil {
			return err
		}
	}

	// 4. Restore members
	for _, cm := range genState.CouncilMembers {
		for _, member := range cm.Members {
			if err := k.Members.Set(ctx, collections.Join(cm.CouncilName, member.Address), member); err != nil {
				return err
			}
		}
	}

	// 5. Restore decision policies
	for _, dp := range genState.DecisionPolicies {
		if err := k.DecisionPolicies.Set(ctx, dp.PolicyAddress, dp.DecisionPolicy); err != nil {
			return err
		}
	}

	// 6. Restore proposals
	for _, proposal := range genState.Proposals {
		if err := k.Proposals.Set(ctx, proposal.Id, proposal); err != nil {
			return err
		}
		// Restore council index
		if err := k.ProposalsByCouncil.Set(ctx, collections.Join(proposal.CouncilName, proposal.Id)); err != nil {
			return err
		}
	}

	// 7. Set sequences
	if genState.NextProposalId > 0 {
		if err := k.ProposalSeq.Set(ctx, genState.NextProposalId); err != nil {
			return err
		}
	}
	if genState.NextCouncilId > 0 {
		if err := k.CouncilSeq.Set(ctx, genState.NextCouncilId); err != nil {
			return err
		}
	}

	// 8. Restore policy versions
	for _, pv := range genState.PolicyVersions {
		if err := k.PolicyVersion.Set(ctx, pv.PolicyAddress, pv.Version); err != nil {
			return err
		}
	}

	// 9. Restore votes
	for _, pv := range genState.ProposalVotes {
		for _, vote := range pv.Votes {
			if err := k.Votes.Set(ctx, collections.Join(pv.ProposalId, vote.Voter), vote); err != nil {
				return err
			}
		}
	}

	// 10. Restore categories
	for _, cat := range genState.CategoryMap {
		if err := k.Category.Set(ctx, cat.CategoryId, cat); err != nil {
			return err
		}
	}

	// Prime the sequence so the first runtime category starts at 1; ID 0 is
	// reserved as "no category".
	catSeqVal, err := k.CategorySeq.Peek(ctx)
	if err != nil {
		return err
	}
	if catSeqVal == 0 {
		if genState.NextCategoryId > 0 {
			if err := k.CategorySeq.Set(ctx, genState.NextCategoryId); err != nil {
				return err
			}
		} else if len(genState.CategoryMap) == 0 {
			if _, err := k.CategorySeq.Next(ctx); err != nil {
				return err
			}
		} else {
			var maxID uint64
			for _, cat := range genState.CategoryMap {
				if cat.CategoryId > maxID {
					maxID = cat.CategoryId
				}
			}
			if err := k.CategorySeq.Set(ctx, maxID+1); err != nil {
				return err
			}
		}
	}

	return nil
}

// ExportGenesis returns the module's exported genesis.
func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	var err error

	genesis := types.DefaultGenesis()
	genesis.Params, err = k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	// Export policy permissions
	if err := k.PolicyPermissions.Walk(ctx, nil, func(_ string, val types.PolicyPermissions) (stop bool, err error) {
		genesis.PolicyPermissionsMap = append(genesis.PolicyPermissionsMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}

	// Export groups
	if err := k.Groups.Walk(ctx, nil, func(_ string, val types.Group) (stop bool, err error) {
		genesis.GroupMap = append(genesis.GroupMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}

	// Export members grouped by council
	councilMembers := make(map[string][]types.Member)
	if err := k.Members.Walk(ctx, nil, func(key collections.Pair[string, string], member types.Member) (bool, error) {
		councilName := key.K1()
		councilMembers[councilName] = append(councilMembers[councilName], member)
		return false, nil
	}); err != nil {
		return nil, err
	}
	for name, members := range councilMembers {
		genesis.CouncilMembers = append(genesis.CouncilMembers, types.CouncilMembers{
			CouncilName: name,
			Members:     members,
		})
	}

	// Export decision policies
	if err := k.DecisionPolicies.Walk(ctx, nil, func(addr string, dp types.DecisionPolicy) (bool, error) {
		genesis.DecisionPolicies = append(genesis.DecisionPolicies, types.PolicyWithAddress{
			PolicyAddress:  addr,
			DecisionPolicy: dp,
		})
		return false, nil
	}); err != nil {
		return nil, err
	}

	// Export proposals
	if err := k.Proposals.Walk(ctx, nil, func(_ uint64, proposal types.Proposal) (bool, error) {
		genesis.Proposals = append(genesis.Proposals, proposal)
		return false, nil
	}); err != nil {
		return nil, err
	}

	// Export sequences
	genesis.NextProposalId, _ = k.ProposalSeq.Peek(ctx)
	genesis.NextCouncilId, _ = k.CouncilSeq.Peek(ctx)

	// Export policy versions
	if err := k.PolicyVersion.Walk(ctx, nil, func(addr string, version uint64) (bool, error) {
		genesis.PolicyVersions = append(genesis.PolicyVersions, types.PolicyVersionEntry{
			PolicyAddress: addr,
			Version:       version,
		})
		return false, nil
	}); err != nil {
		return nil, err
	}

	// Export votes grouped by proposal
	proposalVotes := make(map[uint64][]types.Vote)
	if err := k.Votes.Walk(ctx, nil, func(key collections.Pair[uint64, string], vote types.Vote) (bool, error) {
		pid := key.K1()
		proposalVotes[pid] = append(proposalVotes[pid], vote)
		return false, nil
	}); err != nil {
		return nil, err
	}
	for pid, votes := range proposalVotes {
		genesis.ProposalVotes = append(genesis.ProposalVotes, types.ProposalVotes{
			ProposalId: pid,
			Votes:      votes,
		})
	}

	// Export categories
	if err := k.Category.Walk(ctx, nil, func(_ uint64, cat types.Category) (bool, error) {
		genesis.CategoryMap = append(genesis.CategoryMap, cat)
		return false, nil
	}); err != nil {
		return nil, err
	}
	genesis.NextCategoryId, _ = k.CategorySeq.Peek(ctx)

	return genesis, nil
}
