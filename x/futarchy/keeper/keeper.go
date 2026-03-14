package keeper

import (
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	corestore "cosmossdk.io/core/store"
	"github.com/cosmos/cosmos-sdk/codec"

	"sparkdream/x/futarchy/types"
)

// lateKeepers holds keepers that are wired after depinject initialization
// (to break cyclic dependencies). All value copies of Keeper share the same
// pointer, so mutations via SetCommonsKeeper are visible everywhere.
type lateKeepers struct {
	commonsKeeper types.CommonsKeeper
}

type Keeper struct {
	storeService corestore.KVStoreService
	cdc          codec.Codec
	addressCodec address.Codec
	// Address capable of executing a MsgUpdateParams message.
	// Typically, this should be the x/gov module account.
	authority []byte
	late      *lateKeepers // shared across value copies

	Schema collections.Schema
	Params collections.Item[types.Params]

	authKeeper types.AuthKeeper
	bankKeeper types.BankKeeper

	Market        collections.Map[uint64, types.Market]
	MarketSeq     collections.Sequence
	ActiveMarkets collections.KeySet[collections.Pair[int64, uint64]]

	Hooks types.FutarchyHooks
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

	k := Keeper{
		storeService: storeService,
		cdc:          cdc,
		addressCodec: addressCodec,
		authority:    authority,
		late:         &lateKeepers{},

		authKeeper: authKeeper,
		bankKeeper: bankKeeper,
		Params:     collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),
		Market:     collections.NewMap(sb, types.MarketKey, "market", collections.Uint64Key, codec.CollValue[types.Market](cdc)),
		MarketSeq:  collections.NewSequence(sb, types.MarketSeqKey, "marketseq"),

		ActiveMarkets: collections.NewKeySet(
			sb,
			types.ActiveMarketsKey,
			"active_markets",
			collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key),
		),
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
