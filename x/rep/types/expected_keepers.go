package types

import (
	"context"

	"cosmossdk.io/core/address"
	sdk "github.com/cosmos/cosmos-sdk/types"

	commontypes "sparkdream/x/common/types"
)

// SeasonState is a minimal representation of season data needed by x/rep.
// Defined here (instead of importing seasontypes.Season) to break the
// import cycle: rep/types → season/types → rep/types.
type SeasonState struct {
	Number uint64
}

// AuthKeeper defines the expected interface for the Auth module.
type AuthKeeper interface {
	AddressCodec() address.Codec
	GetAccount(context.Context, sdk.AccAddress) sdk.AccountI // only used for simulation
	// Methods imported from account should be defined here
}

// BankKeeper defines the expected interface for the Bank module.
type BankKeeper interface {
	SpendableCoins(context.Context, sdk.AccAddress) sdk.Coins
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
	BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error
}

// ParamSubspace defines the expected Subspace interface for parameters.
type ParamSubspace interface {
	Get(context.Context, []byte, interface{})
	Set(context.Context, []byte, interface{})
}

// CommonsKeeper defines the expected interface for the Commons module.
type CommonsKeeper interface {
	// Check if an address is a member of a specific committee in a council
	IsCommitteeMember(ctx context.Context, address sdk.AccAddress, council string, committee string) (bool, error)

	// Get the group info for a committee
	GetCommitteeGroupInfo(ctx context.Context, council string, committee string) (interface{}, error)

	// IsCouncilAuthorized checks if addr is authorized via governance, council policy,
	// or committee membership.
	IsCouncilAuthorized(ctx context.Context, addr string, council string, committee string) bool
}

// VoteKeeper defines the expected interface for the x/vote module.
type VoteKeeper interface {
	// VerifyMembershipProof verifies a ZK proof of voter registration membership.
	VerifyMembershipProof(ctx context.Context, proof []byte, nullifier []byte) error
	// GetActiveVoterZkPublicKeys returns the addresses and ZK public keys of all active voter registrations.
	GetActiveVoterZkPublicKeys(ctx context.Context) (addresses []string, zkPubKeys [][]byte, err error)
	// GetVoterZkPublicKey returns the ZK public key for a single active voter registration.
	GetVoterZkPublicKey(ctx context.Context, address string) ([]byte, error)
}

// TagKeeper defines the expected interface for tag registry operations.
// Implemented by x/forum. Wired manually via SetTagKeeper in app.go
// to break the cyclic dependency: forum → rep → forum.
type TagKeeper = commontypes.TagKeeper

// SeasonKeeper defines the expected interface for the Season module.
type SeasonKeeper interface {
	// GetCurrentSeason returns the current season state as a SeasonState.
	// Uses SeasonState (defined above) instead of seasontypes.Season to break
	// the import cycle between rep/types and season/types.
	GetCurrentSeason(ctx context.Context) (SeasonState, error)
	// ResolveDisplayNameAppealInternal resolves a display name appeal after jury verdict
	ResolveDisplayNameAppealInternal(ctx context.Context, member string, appealSucceeded bool) error
}
