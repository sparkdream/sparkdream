package types

import "fmt"

// validatePolls checks the poll section: unique IDs below next_poll_id, a
// tally per option, votes and voter snapshots that reference stored polls
// (and open-ballot proposals), no duplicate entries, and one opening bucket
// per creator.
func (gs GenesisState) validatePolls() error {
	polls := make(map[uint64]Poll, len(gs.Polls))
	for _, p := range gs.Polls {
		if _, ok := polls[p.Id]; ok {
			return fmt.Errorf("duplicated poll %d", p.Id)
		}
		if p.Id == 0 || p.Id >= gs.NextPollId {
			return fmt.Errorf("poll %d outside [1, next_poll_id %d)", p.Id, gs.NextPollId)
		}
		if p.CouncilName == "" || len(p.Options) == 0 || len(p.Tallies) != len(p.Options) {
			return fmt.Errorf("poll %d is malformed", p.Id)
		}
		if _, ok := PollStatus_name[int32(p.Status)]; !ok || p.Status == PollStatus_POLL_STATUS_UNSPECIFIED {
			return fmt.Errorf("poll %d has an invalid status", p.Id)
		}
		polls[p.Id] = p
	}
	type voteKey struct {
		poll  uint64
		voter string
	}
	votes := make(map[voteKey]struct{}, len(gs.PollVotes))
	for _, v := range gs.PollVotes {
		p, ok := polls[v.PollId]
		if !ok {
			return fmt.Errorf("vote on unknown poll %d", v.PollId)
		}
		if p.SecretBallot {
			return fmt.Errorf("open vote on secret-ballot poll %d", v.PollId)
		}
		if v.Option >= uint64(len(p.Options)) {
			return fmt.Errorf("vote on poll %d has option %d out of range", v.PollId, v.Option)
		}
		key := voteKey{v.PollId, v.Voter}
		if _, ok := votes[key]; ok {
			return fmt.Errorf("duplicated vote by %s on poll %d", v.Voter, v.PollId)
		}
		votes[key] = struct{}{}
	}
	seen := make(map[uint64]struct{}, len(gs.PollVoterTrees))
	for _, e := range gs.PollVoterTrees {
		p, ok := polls[e.PollId]
		if !ok || !p.SecretBallot || p.Status != PollStatus_POLL_STATUS_OPEN {
			return fmt.Errorf("voter tree for poll %d, which is not an open secret ballot", e.PollId)
		}
		if _, ok := seen[e.PollId]; ok {
			return fmt.Errorf("duplicated voter tree for poll %d", e.PollId)
		}
		seen[e.PollId] = struct{}{}
	}
	proposals := make(map[uint64]Proposal, len(gs.Proposals))
	for _, p := range gs.Proposals {
		proposals[p.Id] = p
	}
	seen = make(map[uint64]struct{}, len(gs.ProposalVoterTrees))
	for _, e := range gs.ProposalVoterTrees {
		p, ok := proposals[e.ProposalId]
		if !ok || !p.SecretBallot {
			return fmt.Errorf("voter tree for proposal %d, which is not a secret ballot", e.ProposalId)
		}
		if _, ok := seen[e.ProposalId]; ok {
			return fmt.Errorf("duplicated voter tree for proposal %d", e.ProposalId)
		}
		seen[e.ProposalId] = struct{}{}
	}
	creators := make(map[string]struct{}, len(gs.PollOpenCounts))
	for _, c := range gs.PollOpenCounts {
		if c.Creator == "" {
			return fmt.Errorf("poll open count with an empty creator")
		}
		if c.UtcDay < 0 {
			return fmt.Errorf("poll open count for %s has a negative day", c.Creator)
		}
		if _, ok := creators[c.Creator]; ok {
			return fmt.Errorf("duplicated poll open count for %s", c.Creator)
		}
		creators[c.Creator] = struct{}{}
	}
	return nil
}
