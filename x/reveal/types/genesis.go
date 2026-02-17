package types

import "fmt"

// DefaultGenesis returns the default genesis state
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:             DefaultParams(),
		Contributions:      []Contribution{},
		Stakes:             []RevealStake{},
		Votes:              []VerificationVote{},
		NextContributionId: 1,
		NextStakeId:        1,
	}
}

// Validate performs basic genesis state validation returning an error upon any
// failure.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}

	// Validate contribution IDs are unique and sequential
	contribIDs := make(map[uint64]bool)
	for _, c := range gs.Contributions {
		if contribIDs[c.Id] {
			return fmt.Errorf("duplicate contribution ID: %d", c.Id)
		}
		contribIDs[c.Id] = true
		if c.Id >= gs.NextContributionId {
			return fmt.Errorf("contribution ID %d >= next_contribution_id %d", c.Id, gs.NextContributionId)
		}
	}

	// Validate stake IDs are unique and sequential
	stakeIDs := make(map[uint64]bool)
	for _, s := range gs.Stakes {
		if stakeIDs[s.Id] {
			return fmt.Errorf("duplicate stake ID: %d", s.Id)
		}
		stakeIDs[s.Id] = true
		if s.Id >= gs.NextStakeId {
			return fmt.Errorf("stake ID %d >= next_stake_id %d", s.Id, gs.NextStakeId)
		}
		// Verify stake references a valid contribution
		if !contribIDs[s.ContributionId] {
			return fmt.Errorf("stake %d references non-existent contribution %d", s.Id, s.ContributionId)
		}
	}

	// Validate votes reference valid contributions
	for _, v := range gs.Votes {
		if !contribIDs[v.ContributionId] {
			return fmt.Errorf("vote by %s references non-existent contribution %d", v.Voter, v.ContributionId)
		}
	}

	return nil
}
