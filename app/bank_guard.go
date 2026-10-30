package app

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	mintkeeper "github.com/cosmos/cosmos-sdk/x/mint/keeper"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"

	identitykeeper "sparkdream/x/identity/keeper"
	identitytypes "sparkdream/x/identity/types"
)

// MintKeeperAdapter adapts the upstream mint keeper to the identity
// MintKeeper interface. Upstream mint stores params via a collection Item
// (k.Params.Get) rather than exposing a GetParams method, so we wrap it
// at the app layer to avoid plumbing collections into x/identity.
type MintKeeperAdapter struct {
	k mintkeeper.Keeper
}

// NewMintKeeperAdapter wraps a mintkeeper.Keeper for identity's invariants.
func NewMintKeeperAdapter(k mintkeeper.Keeper) MintKeeperAdapter {
	return MintKeeperAdapter{k: k}
}

// GetParams implements identitytypes.MintKeeper.
func (a MintKeeperAdapter) GetParams(ctx context.Context) (minttypes.Params, error) {
	return a.k.Params.Get(ctx)
}

// BankKeeperWithIdentityGuard wraps a bank Keeper and rejects DenomMetadata
// writes that would alter what x/identity seals for its managed denoms: Base,
// Symbol, Display, Name and the denom units with their exponents (the
// decimals wallets render). Description, URI and unit-alias edits are still
// allowed.
//
// Composes the bank and identity keepers at the app layer, NOT in x/identity
// itself, because x/identity is a leaf module and must not import bank.
type BankKeeperWithIdentityGuard struct {
	bankkeeper.Keeper
	// identity is held by pointer so any post-construction state on the
	// underlying keeper (e.g. the sealed identity written at InitGenesis) is
	// visible to the guard on every call.
	identity *identitykeeper.Keeper
}

// WrapBankKeeperWithIdentityGuard constructs the wrapper.
func WrapBankKeeperWithIdentityGuard(bk bankkeeper.Keeper, idk *identitykeeper.Keeper) BankKeeperWithIdentityGuard {
	return BankKeeperWithIdentityGuard{Keeper: bk, identity: idk}
}

// SetDenomMetaData enforces the §14.6 invariant. Three branches:
//
//   - No sealed identity yet (truly pre-InitGenesis). identity's own
//     InitGenesis is the only legitimate writer in this window; pass through.
//   - Foreign (non-native) denom. Pass through.
//   - Native denom whose sealed fields differ from the canonical metadata
//     (types.CheckSealedMetadata). Panic with ErrIdentityImmutable so
//     gov-routed proposals fail execution and upgrade-handler writes halt
//     loudly.
func (w BankKeeperWithIdentityGuard) SetDenomMetaData(ctx context.Context, meta banktypes.Metadata) {
	sealed, err := w.identity.GetSealedIdentity(ctx)
	switch {
	case errors.Is(err, identitytypes.ErrIdentityNotInitialized), errors.Is(err, collections.ErrNotFound):
		w.Keeper.SetDenomMetaData(ctx, meta)
		return
	case err != nil:
		panic(fmt.Errorf("identity guard: sealed-identity lookup failed: %w", err))
	}
	want, native := sealed.CanonicalMetadataFor(meta.Base)
	if !native {
		w.Keeper.SetDenomMetaData(ctx, meta)
		return
	}
	if reason := identitytypes.CheckSealedMetadata(meta, want); reason != "" {
		panic(errorsmod.Wrapf(identitytypes.ErrIdentityImmutable,
			"denom %q: cannot alter the sealed metadata of an x/identity-managed denom: %s", meta.Base, reason))
	}
	w.Keeper.SetDenomMetaData(ctx, meta)
}
