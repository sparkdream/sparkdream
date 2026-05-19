package types

import (
	"context"

	"cosmossdk.io/core/address"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	reptypes "sparkdream/x/rep/types"
	servicetypes "sparkdream/x/service/types"
)

// AuthKeeper defines the expected interface for the Auth module.
type AuthKeeper interface {
	AddressCodec() address.Codec
	GetAccount(context.Context, sdk.AccAddress) sdk.AccountI
	GetModuleAddress(name string) sdk.AccAddress
}

// BankKeeper defines the expected interface for the Bank module.
type BankKeeper interface {
	SpendableCoins(context.Context, sdk.AccAddress) sdk.Coins
	SendCoins(ctx context.Context, fromAddr, toAddr sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
	BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error
}

// CommonsKeeper defines the expected interface for the Commons module.
type CommonsKeeper interface {
	IsCouncilAuthorized(ctx context.Context, addr string, council string, committee string) bool

	// IsGroupPolicyAddress reports whether `addr` is a registered
	// x/commons group-policy address. Used by Phase 1 federation→
	// service migration to validate `Peer.controller_group` /
	// MsgUpdatePeerController inputs before storing them.
	IsGroupPolicyAddress(ctx context.Context, addr string) bool

	// GetCouncilPolicyAddress resolves a (council, committee) tuple to
	// the corresponding x/commons Group's policy address. Used by Phase
	// 3 of the federation→service migration: peers registered without
	// an explicit controller_group fall back to Operations Committee
	// (council="commons", committee="operations"). Returns ok=false if
	// the council/committee is unknown.
	GetCouncilPolicyAddress(ctx context.Context, council string, committee string) (string, bool)
}

// ServiceKeeper is the slice of x/service that x/federation consumes
// (Phase 2 of the federation→service migration). Bridge-operator
// economic state (bond, status, slash history, unbonding lifecycle)
// lives on the service.Operator record keyed by (address, service_type);
// federation owns only the BridgeBinding side and reacts to hooks.
//
// The concrete service.Keeper satisfies this interface implicitly.
// Service hooks fire back into federation via the ServiceHooks
// interface registered in app/service_adapters.go.
type ServiceKeeper interface {
	// RegisterOperator escrows the bond and creates the service.Operator
	// atomically. Does NOT fire any hooks so it's safe to call before
	// federation has written its BridgeBinding. Source must be 0 for
	// normal registrations; non-zero values are reserved for chain
	// upgrades.
	RegisterOperator(ctx context.Context, creator, serviceType, controller string, bond sdk.Coin, metadata []byte, source int) (servicetypes.Operator, error)

	// TopUpBond adds SPARK to an existing operator's bond. Fires
	// AfterOperatorReFunded synchronously on UNDERFUNDED→ACTIVE, so
	// callers MUST have written any state the hook may read (e.g.
	// federation's BridgeBinding) BEFORE invoking this — see Phase 3
	// of the migration plan for the re-entrance ordering rule.
	TopUpBond(ctx context.Context, opBytes []byte, serviceType string, additionalBond sdk.Coin) error

	// OpenSystemReport files a PENDING report from an allowlisted
	// caller module without trust-level/deposit checks. dedupeKey
	// makes the call idempotent within report TTL.
	OpenSystemReport(ctx context.Context, callerModuleAddr sdk.AccAddress, operatorAddr sdk.AccAddress, serviceType string, slashBps uint32, evidenceURI string, dedupeKey []byte) (reportID uint64, idempotent bool, err error)

	// GetOperator returns the live service.Operator record (ACTIVE /
	// UNDERFUNDED / UNBONDING). Archived (SLASHED / RETIRED) operators
	// are not returned — callers iterating bindings should treat
	// !exists as "operator is terminal; binding is orphan" and prune.
	GetOperator(ctx context.Context, addrBytes []byte, serviceType string) (servicetypes.Operator, bool)

	// HasSlashedRecord reports whether (address, service_type) has a
	// prior SLASHED record in the archive. Federation surfaces this
	// at MsgRegisterBridge time to reject re-registration with the
	// same address.
	HasSlashedRecord(ctx context.Context, addrBytes []byte, serviceType string) (bool, error)

	// GetServiceTypeConfig fetches the registry entry for the given
	// service_type or returns false if missing/disabled.
	GetServiceTypeConfig(ctx context.Context, serviceType string) (servicetypes.ServiceTypeConfig, bool)
}

// Source values for ServiceKeeper.RegisterOperator. Mirrors x/service's
// SlashSource constants — kept as untyped consts so the interface stays
// `int`-typed and doesn't require importing servicekeeper.
const (
	// ServiceSourceNormal is the default registration source: all
	// service-side gates enforced (controller validation, min_bond,
	// max_active_operators_per_address, etc.).
	ServiceSourceNormal int = 0
)

// RepKeeper defines the expected interface for the Reputation module.
// Methods are aligned with the actual x/rep keeper implementation.
type RepKeeper interface {
	// GetTrustLevel returns the trust level for a member.
	GetTrustLevel(ctx context.Context, addr sdk.AccAddress) (reptypes.TrustLevel, error)
	// BurnDREAM burns DREAM from an address (used for challenge-fee burns).
	BurnDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	// LockDREAM locks DREAM tokens (moves from available balance to staked).
	LockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	// UnlockDREAM unlocks DREAM tokens (moves from staked to available balance).
	UnlockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error

	// Bonded-role accountability (Phase 4). Federation verifier is the
	// module role keyed as ROLE_TYPE_FEDERATION_VERIFIER.
	GetBondedRole(ctx context.Context, roleType reptypes.RoleType, addr string) (reptypes.BondedRole, error)
	ReserveBond(ctx context.Context, roleType reptypes.RoleType, addr string, amount math.Int) error
	ReleaseBond(ctx context.Context, roleType reptypes.RoleType, addr string, amount math.Int) error
	SlashBond(ctx context.Context, roleType reptypes.RoleType, addr string, amount math.Int, reason string) error
	RecordActivity(ctx context.Context, roleType reptypes.RoleType, addr string) error
	SetBondStatus(ctx context.Context, roleType reptypes.RoleType, addr string, status reptypes.BondedRoleStatus, cooldownUntil int64) error
	SetBondedRoleConfig(ctx context.Context, cfg reptypes.BondedRoleConfig) error
}

// NameKeeper defines the expected interface for the Name module.
// Minimal interface — name resolution for outbound content creator attribution.
type NameKeeper interface {
	// GetNameOwner returns the owner of a name, if it exists.
	GetNameOwner(ctx context.Context, name string) (sdk.AccAddress, bool)
}

// Note: x/federation does not call x/shield directly. Instead, x/shield
// dispatches MsgSubmitArbiterHash to x/federation after ZK proof verification.
// Federation implements ShieldAware (see keeper/shield_aware.go) to opt in.
