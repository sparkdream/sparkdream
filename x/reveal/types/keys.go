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

	// ContributionDeadlinesKey: (next_deadline, contributionID) - IN_PROGRESS
	// contributions ordered by their earliest pending tranche deadline, so the
	// EndBlocker walks only the contributions that are due.
	ContributionDeadlinesKey = collections.NewPrefix("contribution/by_deadline/")

	// ContributionNextDeadlineKey: contributionID -> the deadline currently
	// indexed under ContributionDeadlinesKey (reverse map for index upkeep).
	ContributionNextDeadlineKey = collections.NewPrefix("contribution/next_deadline/")

	// ProjectTransitionQueueKey: (next_attempt_height, contributionID) for
	// COMPLETED contributions whose x/rep project creation failed (REV-07).
	// ProjectTransitionAttemptsKey: contributionID -> failed attempts.
	// Derived state: InitGenesis re-queues every COMPLETED contribution
	// without a project.
	ProjectTransitionQueueKey    = collections.NewPrefix("contribution/project_retry/")
	ProjectTransitionAttemptsKey = collections.NewPrefix("contribution/project_retry_attempts/")
	// DeadlineFailuresKey: contributionID -> consecutive failed deadline steps
	// (transient operational state, like ProjectTransitionAttempts).
	DeadlineFailuresKey = collections.NewPrefix("contribution/deadline_failures/")
)
