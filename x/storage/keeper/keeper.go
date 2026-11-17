package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	corestore "cosmossdk.io/core/store"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/storage/types"
)

// lateDeps are wired from app.go after depinject (never through depinject,
// to stay out of every dependency cycle).
type lateDeps struct {
	rep      types.RepKeeper
	service  types.ServiceKeeper
	commons  types.CommonsKeeper
	distr    types.DistrKeeper
	identity types.IdentityKeeper
}

type Keeper struct {
	storeService corestore.KVStoreService
	cdc          codec.Codec
	addressCodec address.Codec
	// Address capable of executing a MsgUpdateParams message.
	// Typically, this should be the x/gov module account.
	authority []byte

	Schema collections.Schema
	Params collections.Item[types.Params]

	bankKeeper types.BankKeeper
	authKeeper types.AuthKeeper
	late       *lateDeps

	Backends collections.Map[string, types.Backend]
	// ApprovedOperators: (backend, operator) -> approval.
	ApprovedOperators collections.Map[collections.Pair[string, string], types.Approval]
	ApprovedScanners  collections.Map[string, types.Approval]
	Requests          collections.Map[uint64, types.Request]
	RequestSeq        collections.Sequence
	// LiveByHash: (backend, sha256) -> id of the one PENDING, CLAIMED,
	// COMPLETED or SETTLED request for the file.
	LiveByHash    collections.Map[collections.Pair[string, string], uint64]
	RemovedHashes collections.Map[string, types.RemovedHash]
	// ByStatus: (backend/status, id); see statusKey.
	ByStatus collections.KeySet[collections.Pair[string, uint64]]
	// ByRequester: (requester, id) of every stored request.
	ByRequester collections.KeySet[collections.Pair[string, uint64]]
	// OpenByRequester: (requester, id) of PENDING, CLAIMED and COMPLETED
	// requests; backs the per-requester cap without walking settled ones.
	OpenByRequester collections.KeySet[collections.Pair[string, uint64]]
	// ByOperator: (operator, id) of CLAIMED and COMPLETED requests.
	ByOperator collections.KeySet[collections.Pair[string, uint64]]
	// ClaimsByOperator: (operator, id) of CLAIMED requests; backs the
	// open-claims cap.
	ClaimsByOperator collections.KeySet[collections.Pair[string, uint64]]
	// Deadlines: (height, id, kind). One timer queue for the EndBlocker.
	Deadlines collections.KeySet[collections.Triple[int64, uint64, uint32]]
	// Counters: (address, kind) -> per-window count.
	Counters collections.Map[collections.Pair[string, int32], types.Counter]
}

func NewKeeper(
	storeService corestore.KVStoreService,
	cdc codec.Codec,
	addressCodec address.Codec,
	authority []byte,

	bankKeeper types.BankKeeper,
	authKeeper types.AuthKeeper,
) Keeper {
	if _, err := addressCodec.BytesToString(authority); err != nil {
		panic(fmt.Sprintf("invalid authority address %s: %s", authority, err))
	}

	sb := collections.NewSchemaBuilder(storeService)
	pairSS := collections.PairKeyCodec(collections.StringKey, collections.StringKey)
	pairSU := collections.PairKeyCodec(collections.StringKey, collections.Uint64Key)

	k := Keeper{
		storeService: storeService,
		cdc:          cdc,
		addressCodec: addressCodec,
		authority:    authority,

		bankKeeper: bankKeeper,
		authKeeper: authKeeper,
		late:       &lateDeps{},
		Params:     collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),

		Backends:          collections.NewMap(sb, types.BackendsKey, "backends", collections.StringKey, codec.CollValue[types.Backend](cdc)),
		ApprovedOperators: collections.NewMap(sb, types.ApprovedOperatorsKey, "approvedOperators", pairSS, codec.CollValue[types.Approval](cdc)),
		ApprovedScanners:  collections.NewMap(sb, types.ApprovedScannersKey, "approvedScanners", collections.StringKey, codec.CollValue[types.Approval](cdc)),
		Requests:          collections.NewMap(sb, types.RequestsKey, "requests", collections.Uint64Key, codec.CollValue[types.Request](cdc)),
		RequestSeq:        collections.NewSequence(sb, types.RequestSeqKey, "requestSeq"),
		LiveByHash:        collections.NewMap(sb, types.LiveByHashKey, "liveByHash", pairSS, collections.Uint64Value),
		RemovedHashes:     collections.NewMap(sb, types.RemovedHashesKey, "removedHashes", collections.StringKey, codec.CollValue[types.RemovedHash](cdc)),
		ByStatus:          collections.NewKeySet(sb, types.ByStatusKey, "byStatus", pairSU),
		ByRequester:       collections.NewKeySet(sb, types.ByRequesterKey, "byRequester", pairSU),
		OpenByRequester:   collections.NewKeySet(sb, types.OpenByRequesterKey, "openByRequester", pairSU),
		ByOperator:        collections.NewKeySet(sb, types.ByOperatorKey, "byOperator", pairSU),
		ClaimsByOperator:  collections.NewKeySet(sb, types.ClaimsByOperatorKey, "claimsByOperator", pairSU),
		Deadlines:         collections.NewKeySet(sb, types.DeadlinesKey, "deadlines", collections.TripleKeyCodec(collections.Int64Key, collections.Uint64Key, collections.Uint32Key)),
		Counters:          collections.NewMap(sb, types.CountersKey, "counters", collections.PairKeyCodec(collections.StringKey, collections.Int32Key), codec.CollValue[types.Counter](cdc)),
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

// SetRepKeeper wires membership, trust level and the alias contract.
func (k Keeper) SetRepKeeper(rk types.RepKeeper) { k.late.rep = rk }

// SetServiceKeeper wires operator status and controller lookups.
func (k Keeper) SetServiceKeeper(sk types.ServiceKeeper) { k.late.service = sk }

// SetCommonsKeeper wires the council checks.
func (k Keeper) SetCommonsKeeper(ck types.CommonsKeeper) { k.late.commons = ck }

// SetDistrKeeper wires the community pool.
func (k Keeper) SetDistrKeeper(dk types.DistrKeeper) { k.late.distr = dk }

// SetIdentityKeeper wires the chain identity (bond denom).
func (k Keeper) SetIdentityKeeper(ik types.IdentityKeeper) { k.late.identity = ik }

// wired fails a message when the keepers it needs were not set in app.go.
func (k Keeper) wired() error {
	if k.late.rep == nil || k.late.service == nil || k.late.commons == nil || k.late.identity == nil {
		return types.ErrNotWired
	}
	return nil
}

// BondDenom returns the chain's SPARK denom.
func (k Keeper) BondDenom(ctx context.Context) string {
	if k.late.identity == nil {
		panic("storage keeper: identityKeeper not wired (call SetIdentityKeeper after depinject)")
	}
	return k.late.identity.BondDenom(ctx)
}

// isGov reports whether signer is the module authority (x/gov).
func (k Keeper) isGov(signer string) bool {
	auth, err := k.addressCodec.BytesToString(k.authority)
	return err == nil && signer != "" && signer == auth
}

// moduleAddress is the storage module account.
func (k Keeper) moduleAddress() sdk.AccAddress {
	return k.authKeeper.GetModuleAddress(types.ModuleName)
}

// accAddr decodes a bech32 address.
func (k Keeper) accAddr(addr string) (sdk.AccAddress, error) {
	b, err := k.addressCodec.StringToBytes(addr)
	if err != nil {
		return nil, err
	}
	return sdk.AccAddress(b), nil
}

func (k Keeper) canActAs(ctx context.Context, signer, stored string) bool {
	return k.late.rep.CanActAs(ctx, signer, stored)
}

func (k Keeper) canonical(ctx context.Context, addr string) string {
	return k.late.rep.Canonical(ctx, addr)
}
