package types

import "cosmossdk.io/collections"

const (
	// ModuleName defines the module name
	ModuleName = "rep"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// GovModuleName duplicates the gov module's name to avoid a dependency with x/gov.
	// It should be synced with the gov module's name if it is ever changed.
	// See: https://github.com/cosmos/cosmos-sdk/blob/v0.52.0-beta.2/x/gov/types/keys.go#L9
	GovModuleName = "gov"
)

// ParamsKey is the prefix to retrieve all Params
var ParamsKey = collections.NewPrefix("p_rep")

var (
	InvitationKey      = collections.NewPrefix("invitation/value/")
	InvitationCountKey = collections.NewPrefix("invitation/count/")
)

var (
	ProjectKey      = collections.NewPrefix("project/value/")
	ProjectCountKey = collections.NewPrefix("project/count/")
)

var (
	InitiativeKey      = collections.NewPrefix("initiative/value/")
	InitiativeCountKey = collections.NewPrefix("initiative/count/")
)

var (
	StakeKey      = collections.NewPrefix("stake/value/")
	StakeCountKey = collections.NewPrefix("stake/count/")
)

var (
	ChallengeKey      = collections.NewPrefix("challenge/value/")
	ChallengeCountKey = collections.NewPrefix("challenge/count/")
)

var (
	JuryReviewKey      = collections.NewPrefix("juryreview/value/")
	JuryReviewCountKey = collections.NewPrefix("juryreview/count/")
)

var (
	InterimKey      = collections.NewPrefix("interim/value/")
	InterimCountKey = collections.NewPrefix("interim/count/")
)
