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
	govKeeper         *govkeeper.Keeper
	groupKeeper       groupkeeper.Keeper
	splitKeeper       types.SplitKeeper
	upgradeKeeper     types.UpgradeKeeper
	PolicyPermissions collections.Map[string, types.PolicyPermissions]
	ExtendedGroup     collections.Map[string, types.ExtendedGroup]

	// Indexes (For Performance)
	// Key: PolicyAddress (string) -> Value: GroupName (string)
	PolicyToName collections.Map[string, string]
}

func NewKeeper(
	storeService corestore.KVStoreService,
	cdc codec.Codec,
	addressCodec address.Codec,
	authority []byte,

	authKeeper types.AuthKeeper,
	bankKeeper types.BankKeeper,
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
		govKeeper:         govKeeper,
		groupKeeper:       groupKeeper,
		splitKeeper:       splitKeeper,
		upgradeKeeper:     upgradeKeeper,
		Params:            collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),
		PolicyPermissions: collections.NewMap(sb, types.PolicyPermissionsKey, "policyPermissions", collections.StringKey, codec.CollValue[types.PolicyPermissions](cdc)),
		ExtendedGroup:     collections.NewMap(sb, types.ExtendedGroupKey, "extendedGroup", collections.StringKey, codec.CollValue[types.ExtendedGroup](cdc)),
		PolicyToName:      collections.NewMap(sb, types.PolicyToNameKey, "policyToName", collections.StringKey, collections.StringValue),
	}

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema

	return k
}
