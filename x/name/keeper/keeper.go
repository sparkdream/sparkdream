package keeper

import (
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	storetypes "cosmossdk.io/core/store"
	"github.com/cosmos/cosmos-sdk/codec"

	"sparkdream/lib/dreamutil"
	"sparkdream/x/name/types"
)

type Keeper struct {
	cdc          codec.Codec
	addressCodec address.Codec
	// Address capable of executing a MsgUpdateParams message.
	// Typically, this should be the x/gov module account.
	authority []byte

	// External Keepers
	bankKeeper    types.BankKeeper
	commonsKeeper types.CommonsKeeper
	repKeeper     types.RepKeeper

	// Shared DREAM token operations (delegates to repKeeper)
	dreamOps dreamutil.Ops

	// State Collections
	Schema   collections.Schema
	Params   collections.Item[types.Params]
	Names    collections.Map[string, types.NameRecord] // Key: Name
	Owners   collections.Map[string, types.OwnerInfo]  // Key: Address (string)
	Disputes collections.Map[string, types.Dispute]    // Key: Name

	// Secondary Index: (OwnerAddress, Name) -> Empty
	// This allows efficient iteration of names owned by a specific address.
	OwnerNames collections.KeySet[collections.Pair[string, string]]

	// Dispute stake tracking
	DisputeStakes collections.Map[string, types.DisputeStake] // Key: challenge_id
	ContestStakes collections.Map[string, types.ContestStake] // Key: challenge_id
}

// GetCommonsKeeper returns the commons keeper for simulation use.
func (k Keeper) GetCommonsKeeper() types.CommonsKeeper {
	return k.commonsKeeper
}

func NewKeeper(
	storeService storetypes.KVStoreService,
	cdc codec.Codec,
	addressCodec address.Codec,
	authority []byte,
	bankKeeper types.BankKeeper,
	commonsKeeper types.CommonsKeeper,
	repKeeper types.RepKeeper,
) Keeper {
	if _, err := addressCodec.BytesToString(authority); err != nil {
		panic(fmt.Sprintf("invalid authority address %s: %s", authority, err))
	}

	sb := collections.NewSchemaBuilder(storeService)

	k := Keeper{
		cdc:           cdc,
		addressCodec:  addressCodec,
		authority:     authority,
		bankKeeper:    bankKeeper,
		commonsKeeper: commonsKeeper,
		repKeeper:     repKeeper,

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

		// Dispute stake tracking
		DisputeStakes: collections.NewMap(sb, types.KeyDisputeStakes, "dispute_stakes", collections.StringKey, codec.CollValue[types.DisputeStake](cdc)),
		ContestStakes: collections.NewMap(sb, types.KeyContestStakes, "contest_stakes", collections.StringKey, codec.CollValue[types.ContestStake](cdc)),
	}

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema

	// Initialize shared DREAM operations (after schema build)
	k.dreamOps = dreamutil.NewOps(repKeeper, addressCodec)

	return k
}

// GetAuthority returns the module's authority.
func (k Keeper) GetAuthority() []byte {
	return k.authority
}
