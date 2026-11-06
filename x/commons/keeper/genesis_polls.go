package keeper

import (
	"context"

	"sparkdream/x/commons/types"

	"cosmossdk.io/collections"
)

// initPollsGenesis imports polls, their open votes and the secret-ballot
// voter snapshots, and rebuilds the derived poll indexes and queue.
func (k Keeper) initPollsGenesis(ctx context.Context, gs types.GenesisState) error {
	for _, p := range gs.Polls {
		if err := k.Polls.Set(ctx, p.Id, p); err != nil {
			return err
		}
		if err := k.PollsByCouncil.Set(ctx, collections.Join(p.CouncilName, p.Id)); err != nil {
			return err
		}
		if p.Status == types.PollStatus_POLL_STATUS_OPEN {
			if err := k.OpenPollsByCouncil.Set(ctx, collections.Join(p.CouncilName, p.Id)); err != nil {
				return err
			}
		}
		if err := k.PollQueue.Set(ctx, collections.Join(pollQueueDue(p), p.Id)); err != nil {
			return err
		}
	}
	for _, v := range gs.PollVotes {
		if err := k.PollVotes.Set(ctx, collections.Join(v.PollId, v.Voter), v); err != nil {
			return err
		}
	}
	// Poll IDs start at 1 (0 reads as "no poll").
	next := gs.NextPollId
	if next == 0 {
		next = 1
	}
	if err := k.PollSeq.Set(ctx, next); err != nil {
		return err
	}
	for _, e := range gs.ProposalVoterTrees {
		if err := k.ProposalVoterTrees.Set(ctx, e.ProposalId, e.Tree); err != nil {
			return err
		}
	}
	for _, e := range gs.PollVoterTrees {
		if err := k.PollVoterTrees.Set(ctx, e.PollId, e.Tree); err != nil {
			return err
		}
	}
	for _, c := range gs.PollOpenCounts {
		if err := k.PollOpenCounts.Set(ctx, c.Creator, c); err != nil {
			return err
		}
	}
	return nil
}

// exportPollsGenesis exports polls, open votes, the poll sequence, the
// voter snapshots of ballots still open and the per-creator opening buckets.
func (k Keeper) exportPollsGenesis(ctx context.Context, gs *types.GenesisState) error {
	if err := k.Polls.Walk(ctx, nil, func(_ uint64, p types.Poll) (bool, error) {
		gs.Polls = append(gs.Polls, p)
		return false, nil
	}); err != nil {
		return err
	}
	if err := k.PollVotes.Walk(ctx, nil, func(_ collections.Pair[uint64, string], v types.PollVote) (bool, error) {
		gs.PollVotes = append(gs.PollVotes, v)
		return false, nil
	}); err != nil {
		return err
	}
	next, err := k.PollSeq.Peek(ctx)
	if err != nil {
		return err
	}
	gs.NextPollId = next
	if err := k.ProposalVoterTrees.Walk(ctx, nil, func(id uint64, t types.VoterTree) (bool, error) {
		gs.ProposalVoterTrees = append(gs.ProposalVoterTrees, types.ProposalVoterTreeEntry{ProposalId: id, Tree: t})
		return false, nil
	}); err != nil {
		return err
	}
	if err := k.PollVoterTrees.Walk(ctx, nil, func(id uint64, t types.VoterTree) (bool, error) {
		gs.PollVoterTrees = append(gs.PollVoterTrees, types.PollVoterTreeEntry{PollId: id, Tree: t})
		return false, nil
	}); err != nil {
		return err
	}
	return k.PollOpenCounts.Walk(ctx, nil, func(_ string, c types.PollOpenCount) (bool, error) {
		gs.PollOpenCounts = append(gs.PollOpenCounts, c)
		return false, nil
	})
}
