package keeper

import (
	"fmt"

	"sparkdream/x/commons/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	corestore "cosmossdk.io/core/store"
	"github.com/cosmos/cosmos-sdk/codec"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	groupkeeper "github.com/cosmos/cosmos-sdk/x/group/keeper"
)

type Keeper struct {
	storeService corestore.KVStoreService
	cdc          codec.Codec
	addressCodec address.Codec
	// Address capable of executing a MsgUpdateParams message.
	// Typically, this should be the x/gov module account.
	authority []byte

	Schema collections.Schema
	Params collections.Item[types.Params]

	authKeeper        types.AuthKeeper
	bankKeeper        types.BankKeeper
	futarchyKeeper    types.FutarchyKeeper
	govKeeper         *govkeeper.Keeper
	groupKeeper       groupkeeper.Keeper
	splitKeeper       types.SplitKeeper
	upgradeKeeper     types.UpgradeKeeper
	PolicyPermissions collections.Map[string, types.PolicyPermissions]
	ExtendedGroup     collections.Map[string, types.ExtendedGroup]

	// Indexes (For Performance)
	// Key: PolicyAddress (string) -> Value: GroupName (string)
	PolicyToName collections.Map[string, string]

	// Maps a Futarchy Market ID -> Extended Group Name
	// Key: MarketID (uint64) | Value: GroupName (string)
	MarketToGroup collections.Map[uint64, string]

	// Market Trigger Queue
	// Key: (TriggerTimeUnix, GroupName) -> No Value
	MarketTriggerQueue collections.KeySet[collections.Pair[int64, string]]
}

func NewKeeper(
	storeService corestore.KVStoreService,
	cdc codec.Codec,
	addressCodec address.Codec,
	authority []byte,

	authKeeper types.AuthKeeper,
	bankKeeper types.BankKeeper,
	futarchyKeeper types.FutarchyKeeper,
	govKeeper *govkeeper.Keeper,
	groupKeeper groupkeeper.Keeper,
	splitKeeper types.SplitKeeper,
	upgradeKeeper types.UpgradeKeeper,
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

		authKeeper:        authKeeper,
		bankKeeper:        bankKeeper,
		futarchyKeeper:    futarchyKeeper,
		govKeeper:         govKeeper,
		groupKeeper:       groupKeeper,
		splitKeeper:       splitKeeper,
		upgradeKeeper:     upgradeKeeper,
		Params:            collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),
		PolicyPermissions: collections.NewMap(sb, types.PolicyPermissionsKey, "policyPermissions", collections.StringKey, codec.CollValue[types.PolicyPermissions](cdc)),
		ExtendedGroup:     collections.NewMap(sb, types.ExtendedGroupKey, "extendedGroup", collections.StringKey, codec.CollValue[types.ExtendedGroup](cdc)),
		PolicyToName:      collections.NewMap(sb, types.PolicyToNameKey, "policyToName", collections.StringKey, collections.StringValue),
		MarketToGroup:     collections.NewMap(sb, types.MarketToGroupKey, "market_to_group", collections.Uint64Key, collections.StringValue),

		MarketTriggerQueue: collections.NewKeySet(
			sb,
			types.MarketTriggerQueueKey,
			"market_trigger_queue",
			collections.PairKeyCodec(collections.Int64Key, collections.StringKey),
		),
	}

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema

	return k
}
