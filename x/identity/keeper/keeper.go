package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	corestore "cosmossdk.io/core/store"
	"github.com/cosmos/cosmos-sdk/codec"

	"sparkdream/x/identity/types"
)

// Keeper is the x/identity keeper. It holds two collection items:
//
//   - identity (Item[ChainIdentity], prefix 0x00): the mutable record served
//     by queries and convenience accessors.
//   - sealedIdentity (Item[ChainIdentity], prefix 0x01): the immutable record
//     written exactly once at InitGenesis, used by the invariants in §16 to
//     detect post-genesis drift.
//
// x/identity is a leaf module: it has no Msg types, no governance authority,
// and no dependency on other modules at runtime. Bank, staking, and mint
// keepers are late-bound via setters (the seed bank, plus invariant 5's
// staking/mint readers) so the depinject graph stays acyclic. See §1, §7.5
// of x-identity-spec.md.
type Keeper struct {
	storeService corestore.KVStoreService
	cdc          codec.Codec
	bankKeeper   types.BankKeeper
	// stakingKeeper and mintKeeper are optional. Only invariant 5
	// (SDKParamsAlignedInvariant) reads them; if either is nil the invariant
	// returns clean. Production wiring sets both via Set*Keeper from app.go.
	stakingKeeper types.StakingKeeper
	mintKeeper    types.MintKeeper

	Schema         collections.Schema
	identity       collections.Item[types.ChainIdentity]
	sealedIdentity collections.Item[types.ChainIdentity]
}

func NewKeeper(
	storeService corestore.KVStoreService,
	cdc codec.Codec,
	bankKeeper types.BankKeeper,
) Keeper {
	sb := collections.NewSchemaBuilder(storeService)

	k := Keeper{
		storeService: storeService,
		cdc:          cdc,
		bankKeeper:   bankKeeper,

		identity:       collections.NewItem(sb, types.IdentityKey, "identity", codec.CollValue[types.ChainIdentity](cdc)),
		sealedIdentity: collections.NewItem(sb, types.SealedIdentityKey, "sealed_identity", codec.CollValue[types.ChainIdentity](cdc)),
	}

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema
	return k
}

// SetBankKeeper allows late-binding the bank keeper after construction. The
// app wires the guarded BankKeeperWithIdentityGuard back through this setter
// because the bank guard needs the identity keeper, and circular construction
// would otherwise be required.
//
// Write-once: subsequent calls panic. This prevents accidental swaps of a
// guarded bank keeper for the raw one mid-flight (e.g., a future PR that
// constructs identity twice via depinject). The app.go wiring sets it
// exactly once after depinject; tests that construct the keeper with a nil
// bankKeeper via NewKeeper can then call SetBankKeeper once.
func (k *Keeper) SetBankKeeper(bk types.BankKeeper) {
	if k.bankKeeper != nil {
		panic("identity: SetBankKeeper called twice; bank keeper is set-once at app construction")
	}
	k.bankKeeper = bk
}

// SetStakingKeeper late-binds the staking keeper used by invariant 5
// (SDKParamsAlignedInvariant) to compare staking.bond_denom against the
// sealed identity. Write-once for the same reasons as SetBankKeeper.
func (k *Keeper) SetStakingKeeper(sk types.StakingKeeper) {
	if k.stakingKeeper != nil {
		panic("identity: SetStakingKeeper called twice; staking keeper is set-once at app construction")
	}
	k.stakingKeeper = sk
}

// SetMintKeeper late-binds the mint keeper used by invariant 5
// (SDKParamsAlignedInvariant) to compare mint.mint_denom against the sealed
// identity. Write-once for the same reasons as SetBankKeeper.
func (k *Keeper) SetMintKeeper(mk types.MintKeeper) {
	if k.mintKeeper != nil {
		panic("identity: SetMintKeeper called twice; mint keeper is set-once at app construction")
	}
	k.mintKeeper = mk
}

// IsIdentityKeeper is a marker method used by consumer modules'
// expected_keepers.go interfaces to disambiguate the identity keeper from
// other keepers that happen to expose BondDenom/DreamDenom (rep, session,
// etc.). Without this marker, depinject sees three satisfying types and
// panics with "Multiple implementations found for interface IdentityKeeper".
func (k Keeper) IsIdentityKeeper() {}

// GetChainIdentity returns the full mutable identity record. Returns
// (zero, ErrIdentityNotInitialized) before InitGenesis runs.
func (k Keeper) GetChainIdentity(ctx context.Context) (types.ChainIdentity, error) {
	id, err := k.identity.Get(ctx)
	if errors.Is(err, collections.ErrNotFound) {
		return types.ChainIdentity{}, types.ErrIdentityNotInitialized
	}
	return id, err
}

// GetSealedIdentity returns the immutable sealed-genesis record. Returns
// (zero, ErrIdentityNotInitialized) before InitGenesis runs.
func (k Keeper) GetSealedIdentity(ctx context.Context) (types.ChainIdentity, error) {
	id, err := k.sealedIdentity.Get(ctx)
	if errors.Is(err, collections.ErrNotFound) {
		return types.ChainIdentity{}, types.ErrIdentityNotInitialized
	}
	return id, err
}

// BondDenom returns the chain's native gas/staking denom. Panics if
// identity is not yet initialized: every consumer module relies on this
// returning a valid denom, and silently falling back to a hardcoded
// literal would re-introduce the mixed-state class of bug that motivated
// the identity module in the first place. Pre-genesis callers (CLI
// helpers) should use GetChainIdentity directly.
func (k Keeper) BondDenom(ctx context.Context) string {
	id, err := k.GetChainIdentity(ctx)
	if err != nil {
		panic(fmt.Errorf("identity: BondDenom called before genesis or with corrupt state: %w", err))
	}
	return id.BondDenom
}

// DreamDenom returns the chain's native DREAM denom. Panic rules identical
// to BondDenom.
func (k Keeper) DreamDenom(ctx context.Context) string {
	id, err := k.GetChainIdentity(ctx)
	if err != nil {
		panic(fmt.Errorf("identity: DreamDenom called before genesis or with corrupt state: %w", err))
	}
	return id.DreamDenom
}

// setChainIdentity is unexported by design: only InitGenesis is allowed to
// mutate the identity record. No on-chain message can reach this method.
func (k Keeper) setChainIdentity(ctx context.Context, id types.ChainIdentity) error {
	return k.identity.Set(ctx, id)
}
