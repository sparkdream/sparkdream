package types

import (
	"context"

	"cosmossdk.io/core/address"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	reptypes "sparkdream/x/rep/types"
)

// AuthKeeper defines the expected interface for the Auth module.
type AuthKeeper interface {
	AddressCodec() address.Codec
	GetAccount(context.Context, sdk.AccAddress) sdk.AccountI // only used for simulation
}

// BankKeeper defines the expected interface for the Bank module.
type BankKeeper interface {
	SpendableCoins(context.Context, sdk.AccAddress) sdk.Coins
}

// RepKeeper defines the expected interface for the x/rep module.
// x/reveal uses x/rep for DREAM token management, membership checks,
// reputation updates, and project creation on transition.
type RepKeeper interface {
	// Membership and trust level
	IsMember(ctx context.Context, addr sdk.AccAddress) bool
	// IsActiveMember is false for a ZEROED member: gates use it, never
	// IsMember (a zeroed member keeps its record).
	IsActiveMember(ctx context.Context, addr sdk.AccAddress) bool
	GetTrustLevel(ctx context.Context, addr sdk.AccAddress) (reptypes.TrustLevel, error)

	// Member key migration alias contract (docs/x-rep-spec.md, "Member key
	// migration"). Records keep storing the address they were written with;
	// these resolve it against the member's current address.
	CanActAs(ctx context.Context, signer, stored string) bool
	SameMember(ctx context.Context, a, b string) bool
	RecentAddressesOf(ctx context.Context, member string) []string
	AllAddressesOf(ctx context.Context, member string) []string

	// DREAM token operations
	MintDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	BurnDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	LockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	UnlockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	// BurnLockedDREAM slashes locked DREAM: releases the lock and burns the
	// same amount in one step (a failed burn never leaves the lock released).
	BurnLockedDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error

	// Reputation management (absolute amounts, per-tag). The grant goes
	// through x/rep's per-epoch per-tag cap (max_reputation_gain_per_epoch)
	// and returns what was actually granted.
	AddReputationCapped(ctx context.Context, memberAddr sdk.AccAddress, tag string, amount math.LegacyDec) (math.LegacyDec, error)
	DeductReputation(ctx context.Context, memberAddr sdk.AccAddress, tag string, amount math.LegacyDec) error

	// Project creation for post-reveal transition
	CreateProject(ctx context.Context, creator sdk.AccAddress, name, description string, tags []string, category reptypes.ProjectCategory, council string, requestedBudget, requestedSpark math.Int, permissionless bool) (uint64, error)
}

// CommonsKeeper defines the expected interface for the x/commons module.
// Used to verify Operations Committee membership for approvals/rejections/disputes.
type CommonsKeeper interface {
	// IsCommitteeMember checks if an address is a member of a specific committee in a council
	IsCommitteeMember(ctx context.Context, address sdk.AccAddress, council string, committee string) (bool, error)
	// IsCouncilAuthorized checks if an address is the governance authority, a council policy address, or a committee member
	IsCouncilAuthorized(ctx context.Context, addr string, council string, committee string) bool
	// IsCouncilPolicyOrGov checks ONLY for the gov authority or the council's policy address.
	// Individual committee membership does NOT satisfy this check — required for actions that
	// must come through a council vote (e.g. reveal approve/reject/resolve-dispute).
	IsCouncilPolicyOrGov(ctx context.Context, addr string, council string) bool
	// IsCouncilOrCommitteePolicy accepts the gov authority, the council's policy
	// address, or the named committee's policy address (an executed committee
	// proposal) -- never an individual committee member. Used by the
	// committee Cancel path (REV-04).
	IsCouncilOrCommitteePolicy(ctx context.Context, addr string, council string, committee string) bool
}

// ParamSubspace defines the expected Subspace interface for parameters.
type ParamSubspace interface {
	Get(context.Context, []byte, interface{})
	Set(context.Context, []byte, interface{})
}
