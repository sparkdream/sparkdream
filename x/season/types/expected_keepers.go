package types

import (
	"context"

	commonstypes "sparkdream/x/commons/types"
	nametypes "sparkdream/x/name/types"
	reptypes "sparkdream/x/rep/types"

	"cosmossdk.io/core/address"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// AuthKeeper defines the expected interface for the Auth module.
type AuthKeeper interface {
	AddressCodec() address.Codec
	GetAccount(context.Context, sdk.AccAddress) sdk.AccountI // only used for simulation
	// Methods imported from account should be defined here
}

// BankKeeper defines the expected interface for the Bank module.
type BankKeeper interface {
	SpendableCoins(context.Context, sdk.AccAddress) sdk.Coins
	// Methods imported from bank should be defined here
}

// ParamSubspace defines the expected Subspace interface for parameters.
type ParamSubspace interface {
	Get(context.Context, []byte, interface{})
	Set(context.Context, []byte, interface{})
}

// RepKeeper defines the expected interface for the x/rep module.
// This enables cross-module integration for DREAM token operations and jury reviews.
type RepKeeper interface {
	// DREAM token operations
	GetBalance(ctx context.Context, addr sdk.AccAddress) (math.Int, error)
	BurnDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	LockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	UnlockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error

	// Membership verification
	IsMember(ctx context.Context, addr sdk.AccAddress) bool
	GetMember(ctx context.Context, addr sdk.AccAddress) (reptypes.Member, error)

	// Reputation operations for season transitions
	// GetReputationScores returns all reputation scores for a member (tag -> score string)
	GetReputationScores(ctx context.Context, addr string) (map[string]string, error)

	// ArchiveSeasonalReputation archives the member's seasonal reputation to lifetime
	// and resets the seasonal scores. Returns the archived reputation scores.
	ArchiveSeasonalReputation(ctx context.Context, addr string) (map[string]string, error)

	// GetCompletedInitiativesCount returns the cached count of completed initiatives for a member
	GetCompletedInitiativesCount(ctx context.Context, addr string) (uint64, error)

	// MintDREAM mints DREAM tokens to the given address.
	MintDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error

	// GetTrustLevel returns the trust level for a member address.
	GetTrustLevel(ctx context.Context, addr sdk.AccAddress) (reptypes.TrustLevel, error)

	// GetSeasonMinted returns total DREAM minted this season (for activity-based retro PGF budget).
	GetSeasonMinted(ctx context.Context) (math.Int, error)
}

// NameKeeper defines the expected interface for the x/name module.
// This enables cross-module integration for name reservation and release.
type NameKeeper interface {
	// Name operations
	GetName(ctx context.Context, name string) (nametypes.NameRecord, bool)
	SetName(ctx context.Context, record nametypes.NameRecord) error
	RemoveNameFromOwner(ctx context.Context, owner sdk.AccAddress, name string) error

	// Check if a name is available
	IsNameAvailable(ctx context.Context, name string) bool

	// ClaimName atomically checks availability and registers a name,
	// preventing TOCTOU races within the same block.
	ClaimName(ctx context.Context, name string, owner string, data string) error
}

// BlogKeeper defines the expected interface for the x/blog module.
// HasReply is included to disambiguate from x/forum's keeper in depinject.
type BlogKeeper interface {
	HasPost(ctx context.Context, id uint64) bool
	HasReply(ctx context.Context, id uint64) bool
}

// ForumKeeper defines the expected interface for the x/forum module.
// HasCategory is included to disambiguate from x/blog's keeper in depinject.
type ForumKeeper interface {
	HasPost(ctx context.Context, id uint64) bool
	HasCategory(ctx context.Context, id uint64) bool
}

// CollectKeeper defines the expected interface for the x/collect module.
type CollectKeeper interface {
	HasCollection(ctx context.Context, id uint64) bool
}

// CommonsKeeper defines the expected interface for the x/commons module.
// This enables cross-module integration for committee membership checks and council lookups.
type CommonsKeeper interface {
	// Committee membership checks
	IsCommitteeMember(ctx context.Context, address sdk.AccAddress, council string, committee string) (bool, error)

	// GetGroup retrieves a group by name (e.g., "Commons Council")
	GetGroup(ctx context.Context, name string) (commonstypes.Group, error)

	// IsCouncilAuthorized checks if addr is authorized via governance, council policy,
	// or committee membership.
	IsCouncilAuthorized(ctx context.Context, addr string, council string, committee string) bool
}
