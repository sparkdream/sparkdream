package types

import (
	"context"

	"cosmossdk.io/core/address"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

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
	// GetBalance reads the bridge-operator reward pool's sub-address balance.
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
	SendCoins(ctx context.Context, fromAddr, toAddr sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
	BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error
	// SetDenomMetaData / GetDenomMetaData are used by the peer-identity IBC
	// voucher pre-registration (spec §9.2). Federation persists the metadata
	// at peer activation so wallets render <SYMBOL>.ibc instead of ibc/<hash>.
	SetDenomMetaData(ctx context.Context, denomMetaData banktypes.Metadata)
	GetDenomMetaData(ctx context.Context, denom string) (banktypes.Metadata, bool)
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
	// upgrades. The bond amount is in the chain's bond denom (resolved
	// at runtime via x/identity); callers pass a bare math.Int.
	RegisterOperator(ctx context.Context, creator, serviceType, controller string, bondAmount math.Int, metadata []byte, source int) (servicetypes.Operator, error)

	// TopUpBond adds SPARK to an existing operator's bond. Fires
	// AfterOperatorReFunded synchronously on UNDERFUNDED→ACTIVE, so
	// callers MUST have written any state the hook may read (e.g.
	// federation's BridgeBinding) BEFORE invoking this — see Phase 3
	// of the migration plan for the re-entrance ordering rule. The
	// amount is in the chain's bond denom; callers pass a bare math.Int.
	TopUpBond(ctx context.Context, opBytes []byte, serviceType string, additionalBondAmount math.Int) error

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
	// MintDREAM mints DREAM to a member's balance. Used by Phase 10 verifier
	// epoch rewards; subject to x/rep's per-epoch global mint cap.
	MintDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
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
	// IncreaseBond locks `amount` DREAM from the role-holder and credits it
	// to current_bond. Used by Phase 10 verifier rewards to auto-bond into
	// RECOVERY status until min_bond is restored.
	IncreaseBond(ctx context.Context, roleType reptypes.RoleType, addr string, amount math.Int) error
	// RecordRewardPayout updates LastRewardEpoch + CumulativeRewards on the
	// BondedRole record after a reward distribution.
	RecordRewardPayout(ctx context.Context, roleType reptypes.RoleType, addr string, epoch int64, amount math.Int) error
	RecordActivity(ctx context.Context, roleType reptypes.RoleType, addr string) error

	// Shared accountability record (RoleActivity), owned by x/rep. Federation
	// REPORTS verifications and verdicts here and reads the results back for
	// action gating and event payloads; rep applies every consequence --
	// streaks, the escalating overturn cooldown, the accuracy ring, streak
	// demotion -- and runs the role's reward distribution. Federation keeps no
	// copy of any of it.
	RecordRoleAction(ctx context.Context, roleType reptypes.RoleType, addr, kind string) error
	RecordRoleOutcome(ctx context.Context, roleType reptypes.RoleType, addr, kind string, upheld bool) error
	GetRoleActivity(ctx context.Context, roleType reptypes.RoleType, addr string) (reptypes.RoleActivity, error)
	RoleOverturnCooldownUntil(ctx context.Context, roleType reptypes.RoleType, addr string) int64
	SetBondStatus(ctx context.Context, roleType reptypes.RoleType, addr string, status reptypes.BondedRoleStatus, cooldownUntil int64) error
	SetBondedRoleConfig(ctx context.Context, cfg reptypes.BondedRoleConfig) error
}

// DistrKeeper is the slice of x/distribution federation needs to auto-fund
// the bridge-operator reward pool from the community pool.
//
// Late-wired in app.go rather than declared as a depinject dependency: even an
// optional dependency participates in cycle detection.
type DistrKeeper interface {
	DistributeFromFeePool(ctx context.Context, amount sdk.Coins, receiveAddr sdk.AccAddress) error
	GetCommunityPool(ctx context.Context) (sdk.DecCoins, error)
	// GetCommunityTax is the fraction of block rewards routed to the community
	// pool. With annual provisions it gives the pool's income RATE, which is
	// what operator_reward_inflation_share is a share of.
	GetCommunityTax(ctx context.Context) (math.LegacyDec, error)
}

// MintKeeper is the slice of x/mint federation needs to size the operator
// funding draw.
//
// Only annual provisions: supply x current inflation. Reading the rate rather
// than the community pool balance is deliberate — the balance includes the
// genesis allocation earmarked for the councils and any direct deposit,
// neither of which is income.
type MintKeeper interface {
	AnnualProvisions(ctx context.Context) (math.LegacyDec, error)
}

// NameKeeper defines the expected interface for the Name module.
// Minimal interface — name resolution for outbound content creator attribution.
type NameKeeper interface {
	// GetNameOwner returns the owner of a name, if it exists.
	GetNameOwner(ctx context.Context, name string) (sdk.AccAddress, bool)
}

// IdentityKeeper exposes the resolved chain denoms from x/identity. The
// federation keeper consults this at every fee-charging / coin-constructing
// site so a federated chain's denom (e.g. uspark.phoenix) flows through
// correctly instead of a hardcoded "uspark" literal.
type IdentityKeeper interface {
	IsIdentityKeeper() // marker — disambiguates from rep/session.Keeper for depinject
	BondDenom(ctx context.Context) string
	DreamDenom(ctx context.Context) string
}

// Note: x/federation does not call x/shield directly. Instead, x/shield
// dispatches MsgSubmitArbiterHash to x/federation after ZK proof verification.
// Federation implements ShieldAware (see keeper/shield_aware.go) to opt in.
