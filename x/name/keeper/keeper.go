package keeper

import (
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	storetypes "cosmossdk.io/core/store"
	"github.com/cosmos/cosmos-sdk/codec"

	"sparkdream/x/name/types"
)

type Keeper struct {
	cdc          codec.Codec
	addressCodec address.Codec
	// Address capable of executing a MsgUpdateParams message.
	// Typically, this should be the x/gov module account.
	authority []byte

	// External Keepers
	bankKeeper  types.BankKeeper
	groupKeeper types.GroupKeeper

	// State Collections
	Schema   collections.Schema
	Params   collections.Item[types.Params]
	Names    collections.Map[string, types.NameRecord] // Key: Name
	Owners   collections.Map[string, types.OwnerInfo]  // Key: Address (string)
	Disputes collections.Map[string, types.Dispute]    // Key: Name

	// Secondary Index: (OwnerAddress, Name) -> Empty
	// This allows efficient iteration of names owned by a specific address.
	OwnerNames collections.KeySet[collections.Pair[string, string]]
}

func NewKeeper(
	storeService storetypes.KVStoreService,
	cdc codec.Codec,
	addressCodec address.Codec,
	authority []byte,
	bankKeeper types.BankKeeper,
	groupKeeper types.GroupKeeper,
) Keeper {
	if _, err := addressCodec.BytesToString(authority); err != nil {
		panic(fmt.Sprintf("invalid authority address %s: %s", authority, err))
	}

	sb := collections.NewSchemaBuilder(storeService)

	k := Keeper{
		cdc:          cdc,
		addressCodec: addressCodec,
		authority:    authority,
		bankKeeper:   bankKeeper,
		groupKeeper:  groupKeeper,

		// Initialize Collections
		Params:   collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),
		Names:    collections.NewMap(sb, types.KeyNames, "names", collections.StringKey, codec.CollValue[types.NameRecord](cdc)),
		Owners:   collections.NewMap(sb, types.KeyOwners, "owners", collections.StringKey, codec.CollValue[types.OwnerInfo](cdc)),
		Disputes: collections.NewMap(sb, types.KeyDisputes, "disputes", collections.StringKey, codec.CollValue[types.Dispute](cdc)),

		// Initialize Secondary Index
		OwnerNames: collections.NewKeySet(
			sb,
			types.KeyOwnerNames,
			"owner_names",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
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
