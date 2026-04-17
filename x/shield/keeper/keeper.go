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

	// Pending shielded operations (key: op_id)
	PendingOps collections.Map[uint64, types.PendingShieldedOp]

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

	// Per-submitter address rate limits for ante handler anti-spam (key: epoch + submitter → count)
	SubmitterRateLimits collections.Map[collections.Pair[uint64, string], uint64]

	// Pending op counter — maintained by SetPendingOp/DeletePendingOp to avoid full iteration
	PendingOpCount collections.Item[uint64]

	// TLE epoch participation ring buffer for sliding window liveness.
	// Key: (validator_address, epoch_slot) → participated (true/false)
	// epoch_slot = epoch % TleMissWindow, forming a ring buffer.
	TLEEpochParticipation collections.Map[collections.Pair[string, uint64], bool]
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
			collections.Uint64Key, codec.CollValue[types.PendingShieldedOp](cdc)),

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

		SubmitterRateLimits: collections.NewMap(sb, types.SubmitterRateLimitsKey, "submitterRateLimits",
			collections.PairKeyCodec(collections.Uint64Key, collections.StringKey),
			collections.Uint64Value),

		PendingOpCount: collections.NewItem(sb, types.PendingOpCountKey, "pendingOpCount",
			collections.Uint64Value),

		TLEEpochParticipation: collections.NewMap(sb, types.TLEEpochParticipationKey, "tleEpochParticipation",
			collections.PairKeyCodec(collections.StringKey, collections.Uint64Key),
			collections.BoolValue),
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
	// Match by longest prefix
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

// GetSubmitterExecCount returns the number of MsgShieldedExec submissions by
// a given submitter address in a given epoch. Used by the ante handler for
// per-submitter anti-spam rate limiting (SHIELD-8).
func (k Keeper) GetSubmitterExecCount(ctx context.Context, epoch uint64, submitter string) uint64 {
	count, err := k.SubmitterRateLimits.Get(ctx, collections.Join(epoch, submitter))
	if err != nil {
		return 0
	}
	return count
}

// IncrementSubmitterExecCount increments the per-submitter exec count for the
// given epoch. Used by the ante handler (SHIELD-8).
func (k Keeper) IncrementSubmitterExecCount(ctx context.Context, epoch uint64, submitter string) {
	key := collections.Join(epoch, submitter)
	current := k.GetSubmitterExecCount(ctx, epoch, submitter)
	_ = k.SubmitterRateLimits.Set(ctx, key, current+1)
}
