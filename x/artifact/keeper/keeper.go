package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	corestore "cosmossdk.io/core/store"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"sparkdream/x/artifact/types"
)

type (
	// U64Pair keys a token: (class_id, token_id).
	U64Pair = collections.Pair[uint64, uint64]
	// AddrTokenKey keys a per-address token index: (address, class_id, token_id).
	AddrTokenKey = collections.Triple[string, uint64, uint64]
	// TimeTokenKey keys a token expiry index: (expires_at, class_id, token_id).
	TimeTokenKey = collections.Triple[int64, uint64, uint64]
)

// lateKeepers are the cross-module dependencies wired after depinject.
type lateKeepers struct {
	identity types.IdentityKeeper
	rep      types.RepKeeper
	commons  types.CommonsKeeper
	distr    types.DistrKeeper
}

type Keeper struct {
	storeService corestore.KVStoreService
	cdc          codec.Codec
	addressCodec address.Codec
	// Address capable of executing a MsgUpdateParams message.
	// Typically, this should be the x/gov module account.
	authority []byte

	authKeeper types.AuthKeeper
	bankKeeper types.BankKeeper

	// Late-wired from app.go (Set*Keeper) to avoid depinject cycles. Held
	// by pointer so the AppModule's value copy of the keeper sees the
	// wiring too (docs/development-conventions.md, AppModule Value-Copy Bug).
	late *lateKeepers

	Schema collections.Schema
	Params collections.Item[types.Params]

	ClassSeq            collections.Sequence
	Classes             collections.Map[uint64, types.Class]
	ClassesByOwner      collections.KeySet[collections.Pair[string, uint64]]
	ClassCountByCreator collections.Map[string, uint64]

	Tokens          collections.Map[U64Pair, types.Token]
	TokensByOwner   collections.KeySet[AddrTokenKey]
	PublicMintCount collections.Map[collections.Pair[uint64, string], uint64]
	ReceivePolicies collections.Map[string, uint64]

	PendingTransfers collections.Map[U64Pair, types.PendingTransfer]
	PendingMints     collections.Map[U64Pair, types.PendingMint]
	InboxByRecipient collections.Map[AddrTokenKey, uint64] // value: InboxKind
	OutboxBySender   collections.Map[AddrTokenKey, uint64] // value: InboxKind
	PendingPairCount collections.Map[collections.Pair[string, string], uint64]
	InboxCount       collections.Map[string, uint64]
	PendingExpiry    collections.KeySet[TimeTokenKey]

	Listings         collections.Map[U64Pair, types.Listing]
	ListingsBySeller collections.KeySet[AddrTokenKey]
	ListingExpiry    collections.KeySet[TimeTokenKey]

	ClassCancelQueue collections.Map[uint64, uint64] // class_id -> next token id to visit
	ClassScrubQueue  collections.Map[uint64, uint64]

	PendingClassOwners collections.Map[uint64, types.PendingClassOwner]
	PendingOwnerExpiry collections.KeySet[collections.Pair[int64, uint64]]

	HideSeq            collections.Sequence
	HideRecords        collections.Map[uint64, types.HideRecord]
	HideByTarget       collections.Map[U64Pair, uint64] // open (PENDING) record per target
	HidesByTargetAll   collections.KeySet[collections.Triple[uint64, uint64, uint64]]
	HideExpiry         collections.KeySet[collections.Pair[int64, uint64]]
	SentinelDailyHides collections.Map[collections.Pair[string, int64], uint64]

	// FailedExpiry records EndBlocker entries that errored, keyed by
	// (pass, key), so they never block the queue.
	FailedExpiry collections.KeySet[collections.Pair[string, string]]
}

func NewKeeper(
	storeService corestore.KVStoreService,
	cdc codec.Codec,
	addressCodec address.Codec,
	authority []byte,
	authKeeper types.AuthKeeper,
	bankKeeper types.BankKeeper,
) Keeper {
	if _, err := addressCodec.BytesToString(authority); err != nil {
		panic(fmt.Sprintf("invalid authority address %s: %s", authority, err))
	}

	sb := collections.NewSchemaBuilder(storeService)
	u64pair := collections.PairKeyCodec(collections.Uint64Key, collections.Uint64Key)
	addrToken := collections.TripleKeyCodec(collections.StringKey, collections.Uint64Key, collections.Uint64Key)
	timeToken := collections.TripleKeyCodec(collections.Int64Key, collections.Uint64Key, collections.Uint64Key)

	k := Keeper{
		storeService: storeService,
		cdc:          cdc,
		addressCodec: addressCodec,
		authority:    authority,
		authKeeper:   authKeeper,
		bankKeeper:   bankKeeper,
		late:         &lateKeepers{},

		Params: collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),

		ClassSeq:            collections.NewSequence(sb, types.ClassSeqKey, "class_seq"),
		Classes:             collections.NewMap(sb, types.ClassesKey, "classes", collections.Uint64Key, codec.CollValue[types.Class](cdc)),
		ClassesByOwner:      collections.NewKeySet(sb, types.ClassesByOwnerKey, "classes_by_owner", collections.PairKeyCodec(collections.StringKey, collections.Uint64Key)),
		ClassCountByCreator: collections.NewMap(sb, types.ClassCountByCreatorKey, "class_count_by_creator", collections.StringKey, collections.Uint64Value),

		Tokens:          collections.NewMap(sb, types.TokensKey, "tokens", u64pair, codec.CollValue[types.Token](cdc)),
		TokensByOwner:   collections.NewKeySet(sb, types.TokensByOwnerKey, "tokens_by_owner", addrToken),
		PublicMintCount: collections.NewMap(sb, types.PublicMintCountKey, "public_mint_count", collections.PairKeyCodec(collections.Uint64Key, collections.StringKey), collections.Uint64Value),
		ReceivePolicies: collections.NewMap(sb, types.ReceivePoliciesKey, "receive_policies", collections.StringKey, collections.Uint64Value),

		PendingTransfers: collections.NewMap(sb, types.PendingTransfersKey, "pending_transfers", u64pair, codec.CollValue[types.PendingTransfer](cdc)),
		PendingMints:     collections.NewMap(sb, types.PendingMintsKey, "pending_mints", u64pair, codec.CollValue[types.PendingMint](cdc)),
		InboxByRecipient: collections.NewMap(sb, types.InboxByRecipientKey, "inbox_by_recipient", addrToken, collections.Uint64Value),
		OutboxBySender:   collections.NewMap(sb, types.OutboxBySenderKey, "outbox_by_sender", addrToken, collections.Uint64Value),
		PendingPairCount: collections.NewMap(sb, types.PendingPairCountKey, "pending_pair_count", collections.PairKeyCodec(collections.StringKey, collections.StringKey), collections.Uint64Value),
		InboxCount:       collections.NewMap(sb, types.InboxCountKey, "inbox_count", collections.StringKey, collections.Uint64Value),
		PendingExpiry:    collections.NewKeySet(sb, types.PendingExpiryKey, "pending_expiry", timeToken),

		Listings:         collections.NewMap(sb, types.ListingsKey, "listings", u64pair, codec.CollValue[types.Listing](cdc)),
		ListingsBySeller: collections.NewKeySet(sb, types.ListingsBySellerKey, "listings_by_seller", addrToken),
		ListingExpiry:    collections.NewKeySet(sb, types.ListingExpiryKey, "listing_expiry", timeToken),

		ClassCancelQueue: collections.NewMap(sb, types.ClassCancelQueueKey, "class_cancel_queue", collections.Uint64Key, collections.Uint64Value),
		ClassScrubQueue:  collections.NewMap(sb, types.ClassScrubQueueKey, "class_scrub_queue", collections.Uint64Key, collections.Uint64Value),

		PendingClassOwners: collections.NewMap(sb, types.PendingClassOwnersKey, "pending_class_owners", collections.Uint64Key, codec.CollValue[types.PendingClassOwner](cdc)),
		PendingOwnerExpiry: collections.NewKeySet(sb, types.PendingOwnerExpiryKey, "pending_owner_expiry", collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key)),

		HideSeq:            collections.NewSequence(sb, types.HideSeqKey, "hide_seq"),
		HideRecords:        collections.NewMap(sb, types.HideRecordsKey, "hide_records", collections.Uint64Key, codec.CollValue[types.HideRecord](cdc)),
		HideByTarget:       collections.NewMap(sb, types.HideByTargetKey, "hide_by_target", u64pair, collections.Uint64Value),
		HidesByTargetAll:   collections.NewKeySet(sb, types.HidesByTargetAllKey, "hides_by_target_all", collections.TripleKeyCodec(collections.Uint64Key, collections.Uint64Key, collections.Uint64Key)),
		HideExpiry:         collections.NewKeySet(sb, types.HideExpiryKey, "hide_expiry", collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key)),
		SentinelDailyHides: collections.NewMap(sb, types.SentinelDailyHidesKey, "sentinel_daily_hides", collections.PairKeyCodec(collections.StringKey, collections.Int64Key), collections.Uint64Value),

		FailedExpiry: collections.NewKeySet(sb, types.FailedExpiryKey, "failed_expiry", collections.PairKeyCodec(collections.StringKey, collections.StringKey)),
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

// SetIdentityKeeper wires the identity keeper post-depinject.
func (k Keeper) SetIdentityKeeper(ik types.IdentityKeeper) { k.late.identity = ik }

// SetRepKeeper wires the rep keeper post-depinject.
func (k Keeper) SetRepKeeper(rk types.RepKeeper) { k.late.rep = rk }

// SetCommonsKeeper wires the commons keeper post-depinject.
func (k Keeper) SetCommonsKeeper(ck types.CommonsKeeper) { k.late.commons = ck }

// SetDistrKeeper wires the distribution adapter post-depinject.
func (k Keeper) SetDistrKeeper(dk types.DistrKeeper) { k.late.distr = dk }

// GetRepKeeper returns the rep keeper (simulation use).
func (k Keeper) GetRepKeeper() types.RepKeeper { return k.late.rep }

// ModuleAddress returns the module account address (deposit escrow).
func (k Keeper) ModuleAddress() sdk.AccAddress { return authtypes.NewModuleAddress(types.ModuleName) }

// BondDenom returns the chain's bond denom. Panics if identity isn't wired;
// every call site needs a real denom.
func (k Keeper) BondDenom(ctx context.Context) string {
	if k.late.identity == nil {
		panic("artifact keeper: identityKeeper not wired (call SetIdentityKeeper after depinject)")
	}
	return k.late.identity.BondDenom(ctx)
}

// DreamDenom returns the chain's DREAM denom ("" when identity isn't wired).
func (k Keeper) DreamDenom(ctx context.Context) string {
	if k.late.identity == nil {
		return ""
	}
	return k.late.identity.DreamDenom(ctx)
}
