package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/commons/types"
)

func TestGenesisState_ValidatePolls(t *testing.T) {
	poll := func(id uint64, status types.PollStatus, secret bool) types.Poll {
		return types.Poll{
			Id: id, CouncilName: "Phoenix Council", Creator: "creator", Question: "q",
			Options: []string{"aurora", "zenith"}, Tallies: []uint64{0, 0},
			Status: status, SecretBallot: secret,
		}
	}
	base := func() types.GenesisState {
		gs := *types.DefaultGenesis()
		gs.NextPollId = 3
		gs.Polls = []types.Poll{poll(1, types.PollStatus_POLL_STATUS_OPEN, false), poll(2, types.PollStatus_POLL_STATUS_OPEN, true)}
		gs.PollVotes = []types.PollVote{{PollId: 1, Voter: "v1", Option: 1}}
		gs.PollVoterTrees = []types.PollVoterTreeEntry{{PollId: 2}}
		gs.Proposals = []types.Proposal{{Id: 7, SecretBallot: true}, {Id: 8}}
		gs.ProposalVoterTrees = []types.ProposalVoterTreeEntry{{ProposalId: 7}}
		return gs
	}

	tests := []struct {
		desc   string
		mutate func(gs *types.GenesisState)
		valid  bool
	}{
		{"valid", func(*types.GenesisState) {}, true},
		{"duplicate poll", func(gs *types.GenesisState) { gs.Polls = append(gs.Polls, gs.Polls[0]) }, false},
		{"poll id 0", func(gs *types.GenesisState) { gs.Polls[0].Id = 0 }, false},
		{"poll id at next_poll_id", func(gs *types.GenesisState) { gs.NextPollId = 2 }, false},
		{"tally count differs from options", func(gs *types.GenesisState) { gs.Polls[0].Tallies = []uint64{0} }, false},
		{"unspecified status", func(gs *types.GenesisState) { gs.Polls[0].Status = types.PollStatus_POLL_STATUS_UNSPECIFIED }, false},
		{"vote on unknown poll", func(gs *types.GenesisState) { gs.PollVotes[0].PollId = 9 }, false},
		{"open vote on secret poll", func(gs *types.GenesisState) { gs.PollVotes[0].PollId = 2 }, false},
		{"vote option out of range", func(gs *types.GenesisState) { gs.PollVotes[0].Option = 2 }, false},
		{"duplicate vote", func(gs *types.GenesisState) { gs.PollVotes = append(gs.PollVotes, gs.PollVotes[0]) }, false},
		{"tree for an open poll", func(gs *types.GenesisState) { gs.PollVoterTrees[0].PollId = 1 }, false},
		{"tree for a closed secret poll", func(gs *types.GenesisState) { gs.Polls[1].Status = types.PollStatus_POLL_STATUS_CLOSED }, false},
		{"tree for an open proposal", func(gs *types.GenesisState) { gs.ProposalVoterTrees[0].ProposalId = 8 }, false},
		{"duplicate proposal tree", func(gs *types.GenesisState) {
			gs.ProposalVoterTrees = append(gs.ProposalVoterTrees, gs.ProposalVoterTrees[0])
		}, false},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			gs := base()
			tc.mutate(&gs)
			err := gs.Validate()
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
