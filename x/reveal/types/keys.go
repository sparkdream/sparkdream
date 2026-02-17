package types

import "cosmossdk.io/collections"

const (
	// ModuleName defines the module name
	ModuleName = "reveal"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// GovModuleName duplicates the gov module's name to avoid a dependency with x/gov.
	// It should be synced with the gov module's name if it is ever changed.
	// See: https://github.com/cosmos/cosmos-sdk/blob/v0.52.0-beta.2/x/gov/types/keys.go#L9
	GovModuleName = "gov"
)

// ParamsKey is the prefix to retrieve all Params
var ParamsKey = collections.NewPrefix("p_reveal")

// Primary storage keys
var (
	ContributionKey    = collections.NewPrefix("contribution/value/")
	ContributionSeqKey = collections.NewPrefix("contribution/seq/")

	RevealStakeKey    = collections.NewPrefix("revealstake/value/")
	RevealStakeSeqKey = collections.NewPrefix("revealstake/seq/")

	VoteKey = collections.NewPrefix("vote/value/")
)

// Secondary index keys
var (
	// ContributionsByStatusKey: (status, contributionID) - for querying contributions by status
	ContributionsByStatusKey = collections.NewPrefix("contribution/by_status/")

	// ContributionsByContributorKey: (contributor, contributionID) - for querying by contributor
	ContributionsByContributorKey = collections.NewPrefix("contribution/by_contributor/")

	// StakesByTrancheKey: ("{contributionID}/{trancheID}", stakeID) - for querying stakes per tranche
	StakesByTrancheKey = collections.NewPrefix("revealstake/by_tranche/")

	// StakesByStakerKey: (staker, stakeID) - for querying all stakes by a staker
	StakesByStakerKey = collections.NewPrefix("revealstake/by_staker/")

	// VotesByTrancheKey: ("{contributionID}/{trancheID}", voteKey) - for iterating votes per tranche
	VotesByTrancheKey = collections.NewPrefix("vote/by_tranche/")

	// VotesByVoterKey: (voter, voteKey) - for querying all votes by a voter
	VotesByVoterKey = collections.NewPrefix("vote/by_voter/")
)
