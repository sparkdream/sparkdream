package types

import (
	"cosmossdk.io/collections"
)

const (
	// ModuleName defines the module name
	ModuleName = "commons"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// GovModuleName duplicates the gov module's name to avoid a dependency with x/gov.
	// It should be synced with the gov module's name if it is ever changed.
	// See: https://github.com/cosmos/cosmos-sdk/blob/v0.52.0-beta.2/x/gov/types/keys.go#L9
	GovModuleName = "gov"
)

// ParamsKey is the prefix to retrieve all Params
var ParamsKey = collections.NewPrefix("p_commons")

var MarketToGroupKey = collections.NewPrefix("marketToGroup/value/")

var MarketTriggerQueueKey = collections.NewPrefix("marketTriggerQueue/value/")

var (
	KeyProposalFee = []byte("ProposalFee")
)

// Native governance collection keys.
var (
	// Members: (council_name, member_address) -> Member
	MembersKey = collections.NewPrefix("members/value/")
	// DecisionPolicies: policy_address -> DecisionPolicy
	DecisionPoliciesKey = collections.NewPrefix("decisionPolicies/value/")
	// Proposals: proposal_id -> Proposal
	ProposalsKey = collections.NewPrefix("proposals/value/")
	// ProposalSeq: auto-increment sequence for proposal IDs
	ProposalSeqKey = collections.NewPrefix("proposals/seq/")
	// CouncilSeq: auto-increment sequence for council IDs
	CouncilSeqKey = collections.NewPrefix("councils/seq/")
	// PolicyVersion: policy_address -> uint64 (for veto invalidation)
	PolicyVersionKey = collections.NewPrefix("policyVersion/value/")
	// Votes: (proposal_id, voter_address) -> Vote
	VotesKey = collections.NewPrefix("votes/value/")
	// ProposalsByCouncil: (council_name, proposal_id) -> empty (index for filtering)
	ProposalsByCouncilKey = collections.NewPrefix("proposalsByCouncil/value/")
	// VetoPolicies: council_name -> veto_policy_address (maps council to its veto policy)
	VetoPoliciesKey = collections.NewPrefix("vetoPolicies/value/")
	// AnonVoteTallies: proposal_id -> AnonVoteTally (anonymous vote counts)
	AnonVoteTalliesKey = collections.NewPrefix("anonVoteTallies/value/")
	// EpochSpending: (policy_address, epoch_day) -> cumulative bond-denom amount spent (string-encoded math.Int)
	EpochSpendingKey = collections.NewPrefix("epochSpending/value/")
	// DeletedPolicies: policy addresses of deleted groups. A grant whose
	// granter is a former group policy is refused by the session claim hook.
	DeletedPoliciesKey = collections.NewPrefix("deletedPolicies/value/")
	// ProposalQueue: (due_time, proposal_id) -> empty. A SUBMITTED proposal
	// is queued at its voting deadline, an ACCEPTED one at the end of its
	// execution window, so EndBlock walks only what is due.
	ProposalQueueKey = collections.NewPrefix("proposalQueue/value/")
	// AnonProposalCount: (council_name, utc_day) -> anonymous proposals
	// submitted to the council that day.
	AnonProposalCountKey = collections.NewPrefix("anonProposalCount/value/")
	// ProposalVoterTrees: proposal_id -> VoterTree, the voter snapshot of a
	// secret-ballot proposal, dropped once voting closes.
	ProposalVoterTreesKey = collections.NewPrefix("proposalVoterTrees/value/")

	// Polls (non-binding, single-choice questions to a group's members).
	PollsKey   = collections.NewPrefix("polls/value/")
	PollSeqKey = collections.NewPrefix("polls/seq/")
	// PollVotes: (poll_id, voter) -> PollVote, open votes only.
	PollVotesKey = collections.NewPrefix("pollVotes/value/")
	// PollsByCouncil: (council_name, poll_id), every stored poll.
	PollsByCouncilKey = collections.NewPrefix("pollsByCouncil/value/")
	// OpenPollsByCouncil: (council_name, poll_id), open polls only (caps).
	OpenPollsByCouncilKey = collections.NewPrefix("openPollsByCouncil/value/")
	// PollQueue: (due_time, poll_id). An open poll is queued at its
	// deadline, a closed or cancelled one at the end of its retention.
	PollQueueKey = collections.NewPrefix("pollQueue/value/")
	// PollVoterTrees: poll_id -> VoterTree, dropped when the poll closes.
	PollVoterTreesKey = collections.NewPrefix("pollVoterTrees/value/")

	// --- Recurring Spend collections (M10 removed) ---
	// The parallel commons.RecurringSpends storage was deleted by the
	// RecurringSpend migration. Schedules now live in session.Grants.
	// Key-prefix constants are kept reserved (not redefined) so a
	// future use of the `recurringSpends/` prefix is a conscious
	// decision rather than an accidental collision.
)
