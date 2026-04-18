package types

import "cosmossdk.io/collections"

const (
	// ModuleName defines the module name
	ModuleName = "forum"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// GovModuleName duplicates the gov module's name to avoid a dependency with x/gov.
	// It should be synced with the gov module's name if it is ever changed.
	// See: https://github.com/cosmos/cosmos-sdk/blob/v0.52.0-beta.2/x/gov/types/keys.go#L9
	GovModuleName = "gov"
)

// ParamsKey is the prefix to retrieve all Params
var ParamsKey = collections.NewPrefix("p_forum")

var (
	BountyKey      = collections.NewPrefix("bounty/value/")
	BountyCountKey = collections.NewPrefix("bounty/count/")
)

var (
	MemberWarningKey      = collections.NewPrefix("memberwarning/value/")
	MemberWarningCountKey = collections.NewPrefix("memberwarning/count/")
)

var (
	GovActionAppealKey      = collections.NewPrefix("govactionappeal/value/")
	GovActionAppealCountKey = collections.NewPrefix("govactionappeal/count/")
)

// Sequence keys for auto-incrementing IDs
var (
	PostSeqKey = collections.NewPrefix("post/seq/")
)

// ExpirationQueueKey is the prefix for the ephemeral post expiration queue
var ExpirationQueueKey = collections.NewPrefix("expiration_queue/")
