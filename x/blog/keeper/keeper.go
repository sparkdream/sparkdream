package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	corestore "cosmossdk.io/core/store"
	"github.com/cosmos/cosmos-sdk/codec"

	"sparkdream/x/blog/types"
)

// DefaultEpochDuration is the fallback epoch duration in seconds (~5 months)
// used when no SeasonKeeper is configured.
const DefaultEpochDuration int64 = 13_140_000

type Keeper struct {
	storeService corestore.KVStoreService
	cdc          codec.Codec
	addressCodec address.Codec
	// Address capable of executing a MsgUpdateParams message.
	// Typically, this should be the x/gov module account.
	authority     []byte
	bankKeeper    types.BankKeeper
	commonsKeeper types.CommonsKeeper
	repKeeper     types.RepKeeper
	voteKeeper    types.VoteKeeper
	seasonKeeper  types.SeasonKeeper

	Schema collections.Schema
	Params collections.Item[types.Params]
}

func NewKeeper(
	storeService corestore.KVStoreService,
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

	// repKeeper may be nil during depinject (wired manually in app.go to break cycle).
	// It MUST be set via SetRepKeeper before the module processes any transactions.

	sb := collections.NewSchemaBuilder(storeService)

	k := Keeper{
		storeService:  storeService,
		cdc:           cdc,
		addressCodec:  addressCodec,
		authority:     authority,
		bankKeeper:    bankKeeper,
		commonsKeeper: commonsKeeper,
		repKeeper:     repKeeper,

		Params: collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),
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

// SetCommonsKeeper sets the optional CommonsKeeper for council authorization.
func (k *Keeper) SetCommonsKeeper(ck types.CommonsKeeper) {
	k.commonsKeeper = ck
}

// SetVoteKeeper sets the optional VoteKeeper for anonymous posting.
func (k *Keeper) SetVoteKeeper(vk types.VoteKeeper) {
	k.voteKeeper = vk
}

// SetRepKeeper sets the RepKeeper after depinject to break cyclic dependency
// (season → blog → rep → season).
func (k *Keeper) SetRepKeeper(rk types.RepKeeper) {
	k.repKeeper = rk
}

// SetSeasonKeeper sets the optional SeasonKeeper for epoch duration.
func (k *Keeper) SetSeasonKeeper(sk types.SeasonKeeper) {
	k.seasonKeeper = sk
}

// HasPost returns true if a blog post with the given ID exists.
func (k Keeper) HasPost(ctx context.Context, id uint64) bool {
	_, found := k.GetPost(ctx, id)
	return found
}

// HasReply returns true if a blog reply with the given ID exists.
func (k Keeper) HasReply(ctx context.Context, id uint64) bool {
	_, found := k.GetReply(ctx, id)
	return found
}
