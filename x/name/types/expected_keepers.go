package types

import (
	"context"

	commonstypes "sparkdream/x/commons/types"

	"cosmossdk.io/core/address"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// AuthKeeper defines the expected interface for the Auth module.
type AuthKeeper interface {
	AddressCodec() address.Codec
	GetAccount(context.Context, sdk.AccAddress) sdk.AccountI // only used for simulation
}

// IdentityKeeper defines the expected interface for the identity module.
// Resolves the chain's bond denom at runtime.
type IdentityKeeper interface {
	IsIdentityKeeper() // marker — disambiguates from rep/session.Keeper for depinject
	BondDenom(ctx context.Context) string
}

// BankKeeper defines the expected interface for the Bank module.
type BankKeeper interface {
	SpendableCoins(context.Context, sdk.AccAddress) sdk.Coins
	SendCoins(context.Context, sdk.AccAddress, sdk.AccAddress, sdk.Coins) error
	SendCoinsFromAccountToModule(context.Context, sdk.AccAddress, string, sdk.Coins) error
	// BurnCoins burns the registration fee once it reaches the module
	// account (the module account holds the Burner permission).
	BurnCoins(context.Context, string, sdk.Coins) error
}

// CommonsKeeper defines the expected interface for the x/commons module.
type CommonsKeeper interface {
	GetGroup(context.Context, string) (commonstypes.Group, error)
	SetGroup(context.Context, string, commonstypes.Group) error
	GetPolicyPermissions(context.Context, string) (commonstypes.PolicyPermissions, error)
	SetPolicyPermissions(context.Context, string, commonstypes.PolicyPermissions) error
	// IsCouncilAuthorized checks if addr is authorized via governance, council policy,
	// or committee membership.
	IsCouncilAuthorized(ctx context.Context, addr string, council string, committee string) bool
	// IsCouncilOrCommitteePolicy accepts the gov authority, the council's
	// policy address or the named committee's policy address, never an
	// individual member: the action needs an executed proposal.
	IsCouncilOrCommitteePolicy(ctx context.Context, addr string, council string, committee string) bool
	// HasMember checks if an address is a member of a council.
	HasMember(ctx context.Context, councilName string, address string) (bool, error)
	// AddMember adds a member to a council.
	AddMember(ctx context.Context, councilName string, member commonstypes.Member) error
}

// RepKeeper defines the expected interface for the x/rep module (DREAM token
// operations and active-membership check used to gate name registration).
type RepKeeper interface {
	LockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	UnlockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	BurnDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	BurnLockedDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	// IsActiveMember reports whether addr is a registered x/rep member with
	// status MEMBER_STATUS_ACTIVE. Used as the registration / transfer gate.
	IsActiveMember(ctx context.Context, addr sdk.AccAddress) bool

	// Member key migration alias contract (docs/x-rep-spec.md, "Member key
	// migration"). Records keep storing the address they were written with;
	// these resolve it against the member's current address.
	CanActAs(ctx context.Context, signer, stored string) bool
	SameMember(ctx context.Context, a, b string) bool
	AllAddressesOf(ctx context.Context, member string) []string
}

// ParamSubspace defines the expected Subspace interface for parameters.
type ParamSubspace interface {
	Get(context.Context, []byte, interface{})
	Set(context.Context, []byte, interface{})
}
