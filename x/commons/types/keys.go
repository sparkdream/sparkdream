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

// New native governance collection keys (replacing x/group)
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
)
