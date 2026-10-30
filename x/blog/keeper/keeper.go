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

type Keeper struct {
	storeService corestore.KVStoreService
	cdc          codec.Codec
	addressCodec address.Codec
	// Address capable of executing a MsgUpdateParams message.
	// Typically, this should be the x/gov module account.
	authority  []byte
	bankKeeper types.BankKeeper

	Schema collections.Schema
	Params collections.Item[types.Params]
	// late holds the keepers wired after depinject (app.go Set*Keeper).
	// It is a pointer so the AppModule's value copy of the keeper sees
	// them too (docs/development-conventions.md, AppModule value-copy bug).
	late *lateKeepers
}

// lateKeepers are the dependencies wired after construction; shared by
// every copy of the Keeper value.
type lateKeepers struct {
	commonsKeeper  types.CommonsKeeper
	repKeeper      types.RepKeeper
	identityKeeper types.IdentityKeeper
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
		late:         &lateKeepers{commonsKeeper: commonsKeeper, repKeeper: repKeeper},
		storeService: storeService,
		cdc:          cdc,
		addressCodec: addressCodec,
		authority:    authority,
		bankKeeper:   bankKeeper,

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
	k.late.commonsKeeper = ck
}

// SetRepKeeper sets the RepKeeper after depinject to break cyclic dependency
// (season → blog → rep → season).
func (k *Keeper) SetRepKeeper(rk types.RepKeeper) {
	k.late.repKeeper = rk
}

// hasRep reports whether the late-wired rep keeper is present. In network
// builds (types.RepKeeperRequired) a missing rep keeper is a wiring bug and
// hasRep panics instead of letting the caller skip the rep side of the
// operation: a tx fails (baseapp recovers the panic), a block hook halts the
// chain. Test builds keep the standalone fallbacks for unit tests.
func (k Keeper) hasRep() bool {
	if k.late.repKeeper != nil {
		return true
	}
	if types.RepKeeperRequired {
		panic("x/blog: rep keeper is not wired (SetRepKeeper)")
	}
	return false
}

// Alias contract helpers (member key migration). The rep keeper is late-wired
// and nil in standalone unit tests, where these degrade to address equality.

// canActAs reports whether signer may exercise rights recorded against stored.
func (k Keeper) canActAs(ctx context.Context, signer, stored string) bool {
	if !k.hasRep() {
		return signer == stored
	}
	return k.late.repKeeper.CanActAs(ctx, signer, stored)
}

// recentAddressesOf is the in-tx alias set for dedup checks.
func (k Keeper) recentAddressesOf(ctx context.Context, member string) []string {
	if !k.hasRep() {
		return []string{member}
	}
	return k.late.repKeeper.RecentAddressesOf(ctx, member)
}

// allAddressesOf is the full alias set for list queries (never in a tx).
func (k Keeper) allAddressesOf(ctx context.Context, member string) []string {
	if !k.hasRep() {
		return []string{member}
	}
	return k.late.repKeeper.AllAddressesOf(ctx, member)
}

// SetIdentityKeeper wires the x/identity keeper post-depinject so the keeper
// can resolve the chain's bond denom at runtime.
func (k *Keeper) SetIdentityKeeper(ik types.IdentityKeeper) {
	k.late.identityKeeper = ik
}

// BondDenom returns the chain's bond denom from the wired identity keeper.
// Panics if identity is not wired — every call site needs a real denom and
// silently returning a hardcoded literal would re-introduce the legacy
// mixed-state bug we just removed.
func (k Keeper) BondDenom(ctx context.Context) string {
	if k.late.identityKeeper == nil {
		panic("blog keeper: identityKeeper not wired (call SetIdentityKeeper after depinject)")
	}
	return k.late.identityKeeper.BondDenom(ctx)
}

// HasPost returns true if a blog post with the given ID exists.
func (k Keeper) HasPost(ctx context.Context, id uint64) bool {
	_, found := k.GetPost(ctx, id)
	return found
}

// LivePostCreatedAt returns the creation time (unix seconds) of a blog post
// that exists and is ACTIVE (not hidden or deleted), and false otherwise.
// Used by x/season's retro-PGF nomination checks.
func (k Keeper) LivePostCreatedAt(ctx context.Context, id uint64) (int64, bool) {
	post, found := k.GetPost(ctx, id)
	if !found || post.Status != types.PostStatus_POST_STATUS_ACTIVE {
		return 0, false
	}
	return post.CreatedAt, true
}

// HasReply returns true if a blog reply with the given ID exists.
func (k Keeper) HasReply(ctx context.Context, id uint64) bool {
	_, found := k.GetReply(ctx, id)
	return found
}
