package keeper

import (
	"context"
	"errors"
	"fmt"

	"sparkdream/x/commons/types"

	"cosmossdk.io/collections"
)

// InitGenesis initializes the module's state from a provided genesis state.
func (k Keeper) InitGenesis(ctx context.Context, genState types.GenesisState) error {
	// 1. Set Params
	if err := k.Params.Set(ctx, genState.Params); err != nil {
		panic(err)
	}

	// 2. Bootstrap the governance groups (genesis founding_members override
	// the build's compiled-in founders when present). An imported genesis
	// that already carries groups is a restore, not a launch: re-running the
	// bootstrap would re-seat founders removed since genesis and re-create
	// permissions x/gov deleted, because the import below only adds rows.
	if len(genState.GroupMap) == 0 {
		k.BootstrapGovernance(ctx, genState.FoundingMembers)
	}

	// 3. Restore existing collections
	for _, elem := range genState.PolicyPermissionsMap {
		if err := k.PolicyPermissions.Set(ctx, elem.PolicyAddress, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.GroupMap {
		// The index is the group's name and its collection key; an empty one
		// would collapse every group onto "" (GenesisState.Validate also
		// rejects it).
		if elem.Index == "" {
			return fmt.Errorf("group with policy %q has an empty index", elem.PolicyAddress)
		}
		if err := k.Groups.Set(ctx, elem.Index, elem); err != nil {
			return err
		}
		// Rebuild the policy indexes from the group records: without them
		// an imported group is invisible to SubmitProposal, spend gates and
		// the session claim hook.
		if elem.PolicyAddress != "" {
			if err := k.PolicyToName.Set(ctx, elem.PolicyAddress, elem.Index); err != nil {
				return err
			}
		}
		if elem.VetoPolicyAddress != "" {
			if err := k.PolicyToName.Set(ctx, elem.VetoPolicyAddress, elem.Index); err != nil {
				return err
			}
			if err := k.VetoPolicies.Set(ctx, elem.Index, elem.VetoPolicyAddress); err != nil {
				return err
			}
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
		// Rebuild the EndBlock queue for open proposals.
		if err := k.enqueueProposal(ctx, proposal); err != nil {
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

	// 11. Per-epoch spend buckets, confidence-market links and schedule,
	// deleted-policy tombstones. (Recurring spends live in session.Grants
	// and are imported by x/session genesis.)
	for _, e := range genState.EpochSpending {
		if err := k.EpochSpending.Set(ctx, collections.Join(e.PolicyAddress, e.EpochDay), e.Amount); err != nil {
			return err
		}
	}
	for _, e := range genState.MarketToGroup {
		if err := k.MarketToGroup.Set(ctx, e.MarketId, e.GroupName); err != nil {
			return err
		}
	}
	for _, e := range genState.MarketTriggerQueue {
		if err := k.MarketTriggerQueue.Set(ctx, collections.Join(e.TriggerTime, e.GroupName)); err != nil {
			return err
		}
	}
	for _, addr := range genState.DeletedPolicies {
		if err := k.DeletedPolicies.Set(ctx, addr); err != nil {
			return err
		}
	}

	// 12. Anonymous vote tallies and the per-(council, UTC day) anonymous
	// proposal counters (a reset would grant a second daily allowance).
	for _, e := range genState.AnonVoteTallies {
		if err := k.AnonVoteTallies.Set(ctx, e.ProposalId, e.Tally); err != nil {
			return err
		}
	}
	for _, e := range genState.AnonProposalCounts {
		if err := k.AnonProposalCount.Set(ctx, collections.Join(e.CouncilName, e.UtcDay), e.Count); err != nil {
			return err
		}
	}

	// 13. Polls and secret-ballot voter snapshots.
	if err := k.initPollsGenesis(ctx, genState); err != nil {
		return err
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
	// The collection key is authoritative: older records were stored with
	// an empty Index, and InitGenesis keys groups and both policy indexes
	// on it.
	if err := k.Groups.Walk(ctx, nil, func(name string, val types.Group) (stop bool, err error) {
		val.Index = name
		genesis.GroupMap = append(genesis.GroupMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}

	// Export members grouped by council
	councilMembers := make(map[string][]types.Member)
	var councilOrder []string
	if err := k.Members.Walk(ctx, nil, func(key collections.Pair[string, string], member types.Member) (bool, error) {
		councilName := key.K1()
		if _, seen := councilMembers[councilName]; !seen {
			councilOrder = append(councilOrder, councilName)
		}
		councilMembers[councilName] = append(councilMembers[councilName], member)
		return false, nil
	}); err != nil {
		return nil, err
	}
	// Members.Walk yields keys in store order, so the first-seen order of
	// council names is already deterministic; iterate that, not the map.
	for _, name := range councilOrder {
		genesis.CouncilMembers = append(genesis.CouncilMembers, types.CouncilMembers{
			CouncilName: name,
			Members:     councilMembers[name],
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
	if v, getErr := k.ProposalSeq.Peek(ctx); getErr == nil {
		genesis.NextProposalId = v
	} else if !errors.Is(getErr, collections.ErrNotFound) {
		return nil, getErr
	}
	if v, getErr := k.CouncilSeq.Peek(ctx); getErr == nil {
		genesis.NextCouncilId = v
	} else if !errors.Is(getErr, collections.ErrNotFound) {
		return nil, getErr
	}

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
	var voteOrder []uint64
	if err := k.Votes.Walk(ctx, nil, func(key collections.Pair[uint64, string], vote types.Vote) (bool, error) {
		pid := key.K1()
		if _, seen := proposalVotes[pid]; !seen {
			voteOrder = append(voteOrder, pid)
		}
		proposalVotes[pid] = append(proposalVotes[pid], vote)
		return false, nil
	}); err != nil {
		return nil, err
	}
	for _, pid := range voteOrder {
		genesis.ProposalVotes = append(genesis.ProposalVotes, types.ProposalVotes{
			ProposalId: pid,
			Votes:      proposalVotes[pid],
		})
	}

	// Export categories
	if err := k.Category.Walk(ctx, nil, func(_ uint64, cat types.Category) (bool, error) {
		genesis.CategoryMap = append(genesis.CategoryMap, cat)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if v, getErr := k.CategorySeq.Peek(ctx); getErr == nil {
		genesis.NextCategoryId = v
	} else if !errors.Is(getErr, collections.ErrNotFound) {
		return nil, getErr
	}

	// Per-epoch spend buckets, confidence-market state, tombstones.
	if err := k.EpochSpending.Walk(ctx, nil, func(key collections.Pair[string, int64], amount string) (bool, error) {
		genesis.EpochSpending = append(genesis.EpochSpending, types.EpochSpendingEntry{
			PolicyAddress: key.K1(), EpochDay: key.K2(), Amount: amount,
		})
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.MarketToGroup.Walk(ctx, nil, func(id uint64, group string) (bool, error) {
		genesis.MarketToGroup = append(genesis.MarketToGroup, types.MarketToGroupEntry{MarketId: id, GroupName: group})
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.MarketTriggerQueue.Walk(ctx, nil, func(key collections.Pair[int64, string]) (bool, error) {
		genesis.MarketTriggerQueue = append(genesis.MarketTriggerQueue, types.MarketTriggerEntry{TriggerTime: key.K1(), GroupName: key.K2()})
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.DeletedPolicies.Walk(ctx, nil, func(addr string) (bool, error) {
		genesis.DeletedPolicies = append(genesis.DeletedPolicies, addr)
		return false, nil
	}); err != nil {
		return nil, err
	}

	// Anonymous vote tallies and daily anonymous-proposal counters.
	if err := k.AnonVoteTallies.Walk(ctx, nil, func(id uint64, tally types.AnonVoteTally) (bool, error) {
		genesis.AnonVoteTallies = append(genesis.AnonVoteTallies, types.AnonVoteTallyEntry{ProposalId: id, Tally: tally})
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.AnonProposalCount.Walk(ctx, nil, func(key collections.Pair[string, int64], count uint64) (bool, error) {
		genesis.AnonProposalCounts = append(genesis.AnonProposalCounts, types.AnonProposalCountEntry{
			CouncilName: key.K1(), UtcDay: key.K2(), Count: count,
		})
		return false, nil
	}); err != nil {
		return nil, err
	}

	// Polls and secret-ballot voter snapshots.
	if err := k.exportPollsGenesis(ctx, genesis); err != nil {
		return nil, err
	}

	// Recurring spends live in session.Grants (exported by x/session).

	return genesis, nil
}
