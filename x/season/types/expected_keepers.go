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
	BurnLockedDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	LockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	UnlockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error

	// Membership verification
	IsMember(ctx context.Context, addr sdk.AccAddress) bool
	// IsActiveMember is false for a ZEROED member: gates use it, never
	// IsMember (a zeroed member keeps its record).
	IsActiveMember(ctx context.Context, addr sdk.AccAddress) bool
	GetMember(ctx context.Context, addr sdk.AccAddress) (reptypes.Member, error)

	// Reputation operations for season transitions
	// GetReputationScores returns all reputation scores for a member (tag -> score string)
	GetReputationScores(ctx context.Context, addr string) (map[string]string, error)

	// ArchiveSeasonalReputation archives the member's seasonal reputation to lifetime
	// and resets the seasonal scores. Returns the archived reputation scores.
	ArchiveSeasonalReputation(ctx context.Context, addr string) (map[string]string, error)

	// MembersAfter pages x/rep's member records in address order: up to
	// limit addresses strictly after `after`, and whether the walk is done.
	MembersAfter(ctx context.Context, after string, limit int) ([]string, bool, error)

	// GetCompletedInitiativesCount returns the cached count of completed initiatives for a member
	GetCompletedInitiativesCount(ctx context.Context, addr string) (uint64, error)

	// MintDREAM mints DREAM tokens to the given address.
	MintDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error

	// PayRetroPgfReward pays a retroactive public-goods reward, draining the
	// x/rep treasury first when the TreasuryFundsRetroPgf operational param
	// is on and minting any shortfall. Returns the (treasuryPaid, minted)
	// split for event emission.
	PayRetroPgfReward(ctx context.Context, recipient sdk.AccAddress, amount math.Int) (treasuryPaid, minted math.Int, err error)

	// GetTrustLevel returns the trust level for a member address.
	GetTrustLevel(ctx context.Context, addr sdk.AccAddress) (reptypes.TrustLevel, error)

	// GetSeasonMinted returns total DREAM minted this season.
	GetSeasonMinted(ctx context.Context) (math.Int, error)

	// GetSeasonInitiativeRewardsMinted returns the DREAM minted this season
	// for completed initiatives: the activity base of the retro-PGF budget.
	GetSeasonInitiativeRewardsMinted(ctx context.Context) (math.Int, error)

	// GetInitiative returns an initiative, for validating rep/initiative
	// retro-PGF content refs.
	GetInitiative(ctx context.Context, initiativeID uint64) (reptypes.Initiative, error)

	// InitSeasonalPool refills the seasonal staking reward budget and resets
	// x/rep's per-season economic counters for the incoming season. Called once
	// at the end of a season transition.
	InitSeasonalPool(ctx context.Context, season uint64) error

	// Alias contract (member key migration). Stored addresses are
	// interpreted through these: CanActAs for authorization, SameMember for
	// self-dealing bans, RecentAddressesOf for in-tx dedup, AllAddressesOf
	// for by-address list queries.
	CanActAs(ctx context.Context, signer, stored string) bool
	SameMember(ctx context.Context, a, b string) bool
	RecentAddressesOf(ctx context.Context, member string) []string
	AllAddressesOf(ctx context.Context, member string) []string
}

// NameKeeper defines the expected interface for the x/name module.
// This enables cross-module integration for name reservation and release.
type NameKeeper interface {
	// Name operations
	GetName(ctx context.Context, name string) (nametypes.NameRecord, bool)
	SetName(ctx context.Context, record nametypes.NameRecord) error
	// ReleaseName frees a name owner reserved through ClaimName (the record
	// and its indexes are deleted); a name held by someone else is untouched.
	ReleaseName(ctx context.Context, name string, owner string) error

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
	// GetPostAuthor returns the post's creator (retro-PGF self-stake ban).
	GetPostAuthor(ctx context.Context, id uint64) (string, error)
	// LivePostCreatedAt: creation time (unix seconds) of a post that exists
	// and is ACTIVE (not hidden or deleted).
	LivePostCreatedAt(ctx context.Context, id uint64) (int64, bool)
}

// ForumKeeper defines the expected interface for the x/forum module.
// HasCategory is included to disambiguate from x/blog's keeper in depinject.
type ForumKeeper interface {
	HasPost(ctx context.Context, id uint64) bool
	HasCategory(ctx context.Context, id uint64) bool
	// GetPostAuthor returns the post's author (retro-PGF self-stake ban).
	GetPostAuthor(ctx context.Context, postID uint64) (string, error)
	// LivePostCreatedAt: creation time (unix seconds) of a post that exists
	// and is ACTIVE (not hidden, deleted or archived).
	LivePostCreatedAt(ctx context.Context, id uint64) (int64, bool)
}

// CollectKeeper defines the expected interface for the x/collect module.
type CollectKeeper interface {
	HasCollection(ctx context.Context, id uint64) bool
	// LiveCollection returns the owner and creation block height of an
	// ACTIVE collection.
	LiveCollection(ctx context.Context, id uint64) (string, int64, bool)
}

// ArtifactKeeper defines the expected interface for the x/artifact module:
// impact certificates (editions carrying an impact claim) can be nominated
// for retroactive rewards, which go to the nominator like any other
// content's, never to the certificate's holders.
type ArtifactKeeper interface {
	// ImpactCertificateInfo returns the creator and creation time (unix
	// seconds) of a visible impact certificate (SEA-30).
	ImpactCertificateInfo(ctx context.Context, classID, editionID uint64) (creator string, createdAt int64, ok bool)
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

	// IsCouncilOrCommitteePolicy accepts the gov authority, the council's
	// policy address or the named committee's policy address, but NOT an
	// individual committee member. Used for transition control, the season
	// clock and operational params, which must be a committee decision.
	IsCouncilOrCommitteePolicy(ctx context.Context, addr string, council string, committee string) bool
}
