package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/split/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	corestore "cosmossdk.io/core/store"
	errorsmod "cosmossdk.io/errors"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// lateKeepers holds dependencies wired after depinject via Set* methods.
// Stored as a shared pointer so value-copies of Keeper (in AppModule, msgServer)
// see updates made after NewAppModule().
type lateKeepers struct {
	distrKeeper    types.DistrKeeper
	identityKeeper types.IdentityKeeper
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

// SetIdentityKeeper wires the IdentityKeeper (bond denom source) after depinject.
func (k Keeper) SetIdentityKeeper(ik types.IdentityKeeper) {
	k.late.identityKeeper = ik
}

// SetShareByAddress registers (or replaces) the x/split share of address.
// x/commons is the only caller, and only x/gov (or the genesis bootstrap) can
// give a group a non-zero funding weight. Refuses a zero weight, a weight
// above types.MaxShareWeight, and a weight that would push the sum of all
// shares above types.MaxTotalShareWeight, so the caller's state never
// disagrees with what x/split distributes.
func (k Keeper) SetShareByAddress(ctx context.Context, address string, weight uint64) error {
	if weight == 0 {
		return errorsmod.Wrapf(types.ErrInvalidShare, "zero weight for %s", address)
	}
	if weight > types.MaxShareWeight {
		return errorsmod.Wrapf(types.ErrInvalidShare, "weight %d for %s exceeds the cap %d", weight, address, types.MaxShareWeight)
	}

	total := weight
	if err := k.Share.Walk(ctx, nil, func(addr string, share types.Share) (bool, error) {
		if addr != address {
			total += share.Weight
		}
		return false, nil
	}); err != nil {
		return err
	}
	if total > types.MaxTotalShareWeight {
		return errorsmod.Wrapf(types.ErrInvalidShare,
			"total share weight %d would exceed the cap %d", total, types.MaxTotalShareWeight)
	}

	if err := k.Share.Set(ctx, address, types.Share{Address: address, Weight: weight}); err != nil {
		return err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		"split_share_set",
		sdk.NewAttribute("address", address),
		sdk.NewAttribute("weight", fmt.Sprintf("%d", weight)),
	))
	return nil
}

// RemoveShareByAddress deletes the share registered for address, so the
// address stops receiving distributions. Removing an address with no share
// is a no-op. x/commons calls it when a group is deleted.
func (k Keeper) RemoveShareByAddress(ctx context.Context, address string) error {
	has, err := k.Share.Has(ctx, address)
	if err != nil || !has {
		return err
	}
	if err := k.Share.Remove(ctx, address); err != nil {
		return err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		"split_share_removed",
		sdk.NewAttribute("address", address),
	))
	return nil
}
