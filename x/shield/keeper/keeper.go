package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	corestore "cosmossdk.io/core/store"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/shield/types"
)

// lateKeepers holds dependencies wired after depinject via Set* methods.
// Stored as a shared pointer so value-copies of Keeper (in AppModule, msgServer)
// see updates made after NewAppModule().
type lateKeepers struct {
	repKeeper      types.RepKeeper
	distrKeeper    types.DistrKeeper
	slashingKeeper types.SlashingKeeper
	stakingKeeper  types.StakingKeeper
	identityKeeper types.IdentityKeeper
	router         baseapp.MessageRouter

	// shieldAwareModules maps message type URL prefixes to their ShieldAware implementations.
	// Modules register via RegisterShieldAwareModule() after depinject.
	shieldAwareModules map[string]types.ShieldAware
}

type Keeper struct {
	storeService corestore.KVStoreService
	cdc          codec.Codec
	addressCodec address.Codec
	authority    []byte

	accountKeeper types.AccountKeeper
	bankKeeper    types.BankKeeper
	late          *lateKeepers

	Schema collections.Schema
	Params collections.Item[types.Params]

	// Registered shielded operations (key: message_type_url)
	ShieldedOps collections.Map[string, types.ShieldedOpRegistration]

	// Used nullifiers (key: domain + scope + nullifier_hex)
	UsedNullifiers collections.Map[collections.Triple[uint32, uint64, string], types.UsedNullifier]

	// Pending nullifiers for encrypted batch dedup (key: nullifier_hex)
	PendingNullifiers collections.Map[string, bool]

	// Day funding ledger (key: day number)
	DayFundings collections.Map[uint64, types.DayFunding]

	// Per-identity rate limits (key: epoch + rate_limit_nullifier_hex → count)
	IdentityRateLimits collections.Map[collections.Pair[uint64, string], uint64]

	// ZK verification keys (key: circuit_id)
	VerificationKeys collections.Map[string, types.VerificationKey]

	// TLE key set (singleton)
	TLEKeySet collections.Item[types.TLEKeySet]

	// TLE miss counters (key: validator_address → miss count)
	TLEMissCounters collections.Map[string, uint64]

	// Pending shielded operations (key: target_epoch + op_id). One epoch's
	// batch is a prefix range, and counting per epoch walks keys only.
	PendingOps collections.Map[collections.Pair[uint64, uint64], types.PendingShieldedOp]

	// Pending op ID sequence
	NextPendingOpId collections.Sequence

	// Shield epoch state (singleton)
	ShieldEpochState collections.Item[types.ShieldEpochState]

	// Shield epoch decryption keys (key: epoch)
	ShieldDecryptionKeys collections.Map[uint64, types.ShieldEpochDecryptionKey]

	// Shield decryption shares (key: epoch + validator)
	ShieldDecryptionShares collections.Map[collections.Pair[uint64, string], types.ShieldDecryptionShare]

	// DKG state (singleton)
	DKGState collections.Item[types.DKGState]

	// DKG contributions (key: validator_address)
	DKGContributions collections.Map[string, types.DKGContribution]

	// DKG registrations — pub keys from REGISTERING phase (key: validator_operator_address)
	// Reuses DKGContribution proto: FeldmanCommitments[0] = pub key, ProofOfPossession = PoP
	DKGRegistrations collections.Map[string, types.DKGContribution]

	// Pending op counter — maintained by SetPendingOp/DeletePendingOp to avoid full iteration
	PendingOpCount collections.Item[uint64]

	// TLE epoch participation ring buffer for sliding window liveness.
	// Key: (validator_address, slot) → participated (true/false), where slot
	// is TLEJudgedEpochs % tle_miss_window at judgement time, so the ring
	// holds the last tle_miss_window *judged* epochs however sparse demands
	// are. Reset whenever the key set changes.
	TLEEpochParticipation collections.Map[collections.Pair[string, uint64], bool]

	// TLEJudgedEpochs counts demanded epochs judged under the current key set.
	TLEJudgedEpochs collections.Item[uint64]

	// DKG complaints for the current round (key: dealer + complainer)
	DKGComplaints collections.Map[collections.Pair[string, string], types.DKGComplaint]

	// DecryptionDemands records, per closed epoch, the height at which the
	// EndBlocker first asked validators for decryption shares (the batch
	// became ready). Validators only release shares for demanded epochs, and
	// TLE liveness judges an epoch one interval after its demand.
	DecryptionDemands collections.Map[uint64, int64]

	// EpochFeeLedger is the fees the module paid in the current shield epoch,
	// checked against params.max_fees_per_epoch (singleton, overwritten when
	// the epoch changes).
	EpochFeeLedger collections.Item[types.EpochFeeLedger]

	// RetiredDomains keeps, per nullifier domain that no registered op uses
	// any more, the registration of the last op that used it. Deregistration
	// would otherwise erase the domain's scoping rule while its nullifiers
	// are still stored: pruning could no longer find an EPOCH domain, and a
	// new op could reuse the domain under a different rule. An EPOCH entry is
	// dropped once pruning has emptied the domain; any other scope type keeps
	// its nullifiers, and its entry, for good.
	RetiredDomains collections.Map[uint32, types.ShieldedOpRegistration]
}

func NewKeeper(
	storeService corestore.KVStoreService,
	cdc codec.Codec,
	addressCodec address.Codec,
	authority []byte,
	accountKeeper types.AccountKeeper,
	bankKeeper types.BankKeeper,
) Keeper {
	if _, err := addressCodec.BytesToString(authority); err != nil {
		panic(fmt.Sprintf("invalid authority address %s: %s", authority, err))
	}

	sb := collections.NewSchemaBuilder(storeService)

	k := Keeper{
		storeService:  storeService,
		cdc:           cdc,
		addressCodec:  addressCodec,
		authority:     authority,
		accountKeeper: accountKeeper,
		bankKeeper:    bankKeeper,
		late:          &lateKeepers{},

		Params: collections.NewItem(sb, types.ParamsKey, "params",
			codec.CollValue[types.Params](cdc)),

		ShieldedOps: collections.NewMap(sb, types.ShieldedOpsKey, "shieldedOps",
			collections.StringKey, codec.CollValue[types.ShieldedOpRegistration](cdc)),

		UsedNullifiers: collections.NewMap(sb, types.UsedNullifiersKey, "usedNullifiers",
			collections.TripleKeyCodec(collections.Uint32Key, collections.Uint64Key, collections.StringKey),
			codec.CollValue[types.UsedNullifier](cdc)),

		PendingNullifiers: collections.NewMap(sb, types.PendingNullifiersKey, "pendingNullifiers",
			collections.StringKey, collections.BoolValue),

		DayFundings: collections.NewMap(sb, types.DayFundingsKey, "dayFundings",
			collections.Uint64Key, codec.CollValue[types.DayFunding](cdc)),

		IdentityRateLimits: collections.NewMap(sb, types.IdentityRateLimitsKey, "identityRateLimits",
			collections.PairKeyCodec(collections.Uint64Key, collections.StringKey),
			collections.Uint64Value),

		VerificationKeys: collections.NewMap(sb, types.VerificationKeysKey, "verificationKeys",
			collections.StringKey, codec.CollValue[types.VerificationKey](cdc)),

		TLEKeySet: collections.NewItem(sb, types.TLEKeySetKey, "tleKeySet",
			codec.CollValue[types.TLEKeySet](cdc)),

		TLEMissCounters: collections.NewMap(sb, types.TLEMissCountersKey, "tleMissCounters",
			collections.StringKey, collections.Uint64Value),

		PendingOps: collections.NewMap(sb, types.PendingOpsKey, "pendingOps",
			collections.PairKeyCodec(collections.Uint64Key, collections.Uint64Key),
			codec.CollValue[types.PendingShieldedOp](cdc)),

		NextPendingOpId: collections.NewSequence(sb, types.NextPendingOpIdKey, "nextPendingOpId"),

		ShieldEpochState: collections.NewItem(sb, types.ShieldEpochStateKey, "shieldEpochState",
			codec.CollValue[types.ShieldEpochState](cdc)),

		ShieldDecryptionKeys: collections.NewMap(sb, types.ShieldDecryptionKeysKey, "shieldDecryptionKeys",
			collections.Uint64Key, codec.CollValue[types.ShieldEpochDecryptionKey](cdc)),

		ShieldDecryptionShares: collections.NewMap(sb, types.ShieldDecryptionSharesKey, "shieldDecryptionShares",
			collections.PairKeyCodec(collections.Uint64Key, collections.StringKey),
			codec.CollValue[types.ShieldDecryptionShare](cdc)),

		DKGState: collections.NewItem(sb, types.DKGStateKey, "dkgState",
			codec.CollValue[types.DKGState](cdc)),

		DKGContributions: collections.NewMap(sb, types.DKGContributionsKey, "dkgContributions",
			collections.StringKey, codec.CollValue[types.DKGContribution](cdc)),

		DKGRegistrations: collections.NewMap(sb, types.DKGRegistrationsKey, "dkgRegistrations",
			collections.StringKey, codec.CollValue[types.DKGContribution](cdc)),

		PendingOpCount: collections.NewItem(sb, types.PendingOpCountKey, "pendingOpCount",
			collections.Uint64Value),

		TLEEpochParticipation: collections.NewMap(sb, types.TLEEpochParticipationKey, "tleEpochParticipation",
			collections.PairKeyCodec(collections.StringKey, collections.Uint64Key),
			collections.BoolValue),
		DecryptionDemands: collections.NewMap(sb, types.DecryptionDemandsKey, "decryptionDemands",
			collections.Uint64Key, collections.Int64Value),
		TLEJudgedEpochs: collections.NewItem(sb, types.TLEJudgedEpochsKey, "tleJudgedEpochs",
			collections.Uint64Value),
		DKGComplaints: collections.NewMap(sb, types.DKGComplaintsKey, "dkgComplaints",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
			codec.CollValue[types.DKGComplaint](cdc)),
		EpochFeeLedger: collections.NewItem(sb, types.EpochFeeLedgerKey, "epochFeeLedger",
			codec.CollValue[types.EpochFeeLedger](cdc)),
		RetiredDomains: collections.NewMap(sb, types.RetiredDomainsKey, "retiredDomains",
			collections.Uint32Key, codec.CollValue[types.ShieldedOpRegistration](cdc)),
	}

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema

	return k
}

// GetAuthority returns the module's authority.
func (k Keeper) GetAuthority() []byte {
	return k.authority
}

// SetRepKeeper wires the RepKeeper after depinject.
func (k Keeper) SetRepKeeper(rk types.RepKeeper) {
	k.late.repKeeper = rk
}

// SetDistrKeeper wires the DistrKeeper after depinject.
func (k Keeper) SetDistrKeeper(dk types.DistrKeeper) {
	k.late.distrKeeper = dk
}

// SetSlashingKeeper wires the SlashingKeeper after depinject.
func (k Keeper) SetSlashingKeeper(sk types.SlashingKeeper) {
	k.late.slashingKeeper = sk
}

// SetStakingKeeper wires the StakingKeeper after depinject.
func (k Keeper) SetStakingKeeper(sk types.StakingKeeper) {
	k.late.stakingKeeper = sk
}

// SetIdentityKeeper late-binds the identity keeper for federated-denom
// resolution. Called from app.go post-depinject.
func (k Keeper) SetIdentityKeeper(idk types.IdentityKeeper) {
	k.late.identityKeeper = idk
}

// BondDenom returns the chain's bond denom from the wired identity keeper.
// Panics if identity isn't wired: no silent fallback to a hardcoded literal.
func (k Keeper) BondDenom(ctx context.Context) string {
	if k.late.identityKeeper == nil {
		panic("shield keeper: identityKeeper not wired (call SetIdentityKeeper after depinject)")
	}
	return k.late.identityKeeper.BondDenom(ctx)
}

// SetRouter wires the MsgServiceRouter after app build for inner message dispatch.
func (k Keeper) SetRouter(router baseapp.MessageRouter) {
	k.late.router = router
}

// RegisterShieldAwareModule registers a module as ShieldAware for the given
// message type URL prefix (e.g., "/sparkdream.blog.v1."). The shield executor
// checks this before dispatching inner messages as a second gate beyond the
// governance whitelist.
func (k Keeper) RegisterShieldAwareModule(typeURLPrefix string, sa types.ShieldAware) {
	if k.late.shieldAwareModules == nil {
		k.late.shieldAwareModules = make(map[string]types.ShieldAware)
	}
	k.late.shieldAwareModules[typeURLPrefix] = sa
}

// getShieldAware looks up the ShieldAware implementation for a given message type URL.
func (k Keeper) getShieldAware(typeURL string) (types.ShieldAware, bool) {
	if k.late.shieldAwareModules == nil {
		return nil, false
	}
	// First matching prefix. Map order is random, so this is deterministic
	// only because the registered prefixes are disjoint module namespaces
	// ("/sparkdream.<module>.v1.").
	for prefix, sa := range k.late.shieldAwareModules {
		if len(typeURL) >= len(prefix) && typeURL[:len(prefix)] == prefix {
			return sa, true
		}
	}
	return nil, false
}

// GetShieldParams returns module params. Used by the ante handler.
func (k Keeper) GetShieldParams(ctx sdk.Context) (types.Params, error) {
	return k.Params.Get(ctx)
}

// GetStakingKeeper returns the late-wired staking keeper. Used by ABCI handlers.
func (k Keeper) GetStakingKeeper() types.StakingKeeper {
	return k.late.stakingKeeper
}

// GetAddressCodec returns the address codec. Used by ABCI handlers.
func (k Keeper) GetAddressCodec() address.Codec {
	return k.addressCodec
}
