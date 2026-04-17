package keeper

import (
	"bytes"
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	corestore "cosmossdk.io/core/store"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/session/types"
)

// lateKeepers holds dependencies wired after depinject via Set* methods.
// Stored as a shared pointer so value-copies of Keeper (in AppModule, msgServer)
// see updates made after NewAppModule().
type lateKeepers struct {
	router        baseapp.MessageRouter
	commonsKeeper types.CommonsKeeper
}

type Keeper struct {
	storeService corestore.KVStoreService
	cdc          codec.Codec
	addressCodec address.Codec
	authority    []byte

	bankKeeper types.BankKeeper
	authKeeper types.AuthKeeper
	late       *lateKeepers

	// State Collections
	Schema collections.Schema
	Params collections.Item[types.Params]

	// Primary: (granter, grantee) -> Session
	Sessions collections.Map[collections.Pair[string, string], types.Session]

	// Index: (granter, grantee) -> empty (for list-by-granter queries)
	SessionsByGranter collections.KeySet[collections.Pair[string, string]]

	// Index: (grantee, granter) -> empty (for list-by-grantee queries + ante decorator lookup)
	SessionsByGrantee collections.KeySet[collections.Pair[string, string]]

	// Index: (expiration_unix, granter, grantee) -> empty (for efficient pruning)
	SessionsByExpiration collections.KeySet[collections.Triple[int64, string, string]]
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

	k := Keeper{
		storeService: storeService,
		cdc:          cdc,
		addressCodec: addressCodec,
		authority:    authority,
		bankKeeper:   bankKeeper,
		authKeeper:   authKeeper,
		late:         &lateKeepers{},

		Params: collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),

		Sessions: collections.NewMap(
			sb, types.SessionsKey, "sessions",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
			codec.CollValue[types.Session](cdc),
		),

		SessionsByGranter: collections.NewKeySet(
			sb, types.SessionsByGranterKey, "sessions_by_granter",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
		),

		SessionsByGrantee: collections.NewKeySet(
			sb, types.SessionsByGranteeKey, "sessions_by_grantee",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
		),

		SessionsByExpiration: collections.NewKeySet(
			sb, types.SessionsByExpirationKey, "sessions_by_expiration",
			collections.TripleKeyCodec(collections.Int64Key, collections.StringKey, collections.StringKey),
		),
	}

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema

	return k
}

// SetRouter wires the MsgServiceRouter after app build for inner message dispatch.
func (k Keeper) SetRouter(router baseapp.MessageRouter) {
	k.late.router = router
}

// SetCommonsKeeper wires the optional CommonsKeeper used for council-gated
// operational parameter updates. Wired in app.go post-depinject.
func (k Keeper) SetCommonsKeeper(ck types.CommonsKeeper) {
	k.late.commonsKeeper = ck
}

// isCouncilAuthorized returns true when addr is either the governance authority
// or (when CommonsKeeper is wired) a member/policy of the given council/committee.
func (k Keeper) isCouncilAuthorized(ctx context.Context, addr string, council string, committee string) bool {
	addrBytes, err := k.addressCodec.StringToBytes(addr)
	if err != nil {
		return false
	}
	if bytes.Equal(k.authority, addrBytes) {
		return true
	}
	if k.late.commonsKeeper == nil {
		return false
	}
	return k.late.commonsKeeper.IsCouncilAuthorized(ctx, addr, council, committee)
}

// GetAuthority returns the module's authority.
func (k Keeper) GetAuthority() []byte {
	return k.authority
}

// GetSession returns a session by (granter, grantee). Used by the ante handler.
func (k Keeper) GetSession(ctx context.Context, granter, grantee string) (types.Session, error) {
	return k.Sessions.Get(ctx, collections.Join(granter, grantee))
}

// UpdateSessionSpent increments a session's spent counter. Used by the post handler.
func (k Keeper) UpdateSessionSpent(ctx context.Context, granter, grantee string, feeAmount sdk.Coin) error {
	key := collections.Join(granter, grantee)
	session, err := k.Sessions.Get(ctx, key)
	if err != nil {
		return err // session may have been deleted/expired; ignore
	}
	session.Spent = session.Spent.Add(feeAmount)
	return k.Sessions.Set(ctx, key, session)
}
