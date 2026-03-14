package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/split/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	corestore "cosmossdk.io/core/store"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// lateKeepers holds dependencies wired after depinject via Set* methods.
// Stored as a shared pointer so value-copies of Keeper (in AppModule, msgServer)
// see updates made after NewAppModule().
type lateKeepers struct {
	distrKeeper types.DistrKeeper
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

	authKeeper types.AuthKeeper
	bankKeeper types.BankKeeper
	late       *lateKeepers
	Share      collections.Map[string, types.Share]
}

func NewKeeper(
	storeService corestore.KVStoreService,
	cdc codec.Codec,
	addressCodec address.Codec,
	authority []byte,

	authKeeper types.AuthKeeper,
	bankKeeper types.BankKeeper,
	distrKeeper types.DistrKeeper,
) Keeper {
	if _, err := addressCodec.BytesToString(authority); err != nil {
		panic(fmt.Sprintf("invalid authority address %s: %s", authority, err))
	}

	sb := collections.NewSchemaBuilder(storeService)

	late := &lateKeepers{distrKeeper: distrKeeper}

	k := Keeper{
		storeService: storeService,
		cdc:          cdc,
		addressCodec: addressCodec,
		authority:    authority,

		authKeeper: authKeeper,
		bankKeeper: bankKeeper,
		late:       late,
		Params:     collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),
		Share:      collections.NewMap(sb, types.ShareKey, "share", collections.StringKey, codec.CollValue[types.Share](cdc))}

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

// SetDistrKeeper wires the DistrKeeper after depinject.
func (k Keeper) SetDistrKeeper(dk types.DistrKeeper) {
	k.late.distrKeeper = dk
}

// SetShareByAddress is a helper to satisfy the x/commons expected interface.
// It acts as a wrapper around the Collections API.
func (k Keeper) SetShareByAddress(ctx context.Context, address string, weight uint64) {
	// Construct the Share struct
	share := types.Share{
		Address: address,
		Weight:  weight,
	}

	if err := k.Share.Set(ctx, address, share); err != nil {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		sdkCtx.Logger().With("module", "x/split").Error("failed to set share in x/split", "address", address, "error", err)
	}
}
