package keeper

import (
	"errors"
	"fmt"
	"strings"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/identity/types"
)

// RegisterInvariants wires the §16 invariants into the supplied registry.
// Invariants 1-4 panic on violation (state corruption or defeated guard);
// invariant 5 (SDK-params drift) is warning-grade (stop=false). All
// invariants read live keeper fields (notably the late-bound bank, staking,
// and mint keepers set from app.go) via the *Keeper pointer. Invariant 5
// returns a clean no-op if staking/mint keepers were never wired (test
// harnesses); production wiring always sets them.
func RegisterInvariants(ir sdk.InvariantRegistry, k *Keeper) {
	ir.RegisterRoute(types.ModuleName, "identity-initialized", IdentityInitializedInvariant(k))
	ir.RegisterRoute(types.ModuleName, "canonical-fields", CanonicalFieldsInvariant(k))
	ir.RegisterRoute(types.ModuleName, "bank-metadata-present", BankMetadataPresentInvariant(k))
	ir.RegisterRoute(types.ModuleName, "bank-metadata-canonical", BankMetadataCanonicalInvariant(k))
	ir.RegisterRoute(types.ModuleName, "sdk-params-aligned", SDKParamsAlignedInvariant(k))
}

// IdentityInitializedInvariant: both the mutable Identity and the
// SealedGenesisIdentity must be populated on a live chain. (Invariant 1.)
func IdentityInitializedInvariant(k *Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		if _, err := k.identity.Get(ctx); err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				return sdk.FormatInvariant(types.ModuleName, "identity-initialized",
					"mutable Identity collection is empty"), true
			}
			return sdk.FormatInvariant(types.ModuleName, "identity-initialized",
				fmt.Sprintf("Identity lookup failed: %v", err)), true
		}
		if _, err := k.sealedIdentity.Get(ctx); err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				return sdk.FormatInvariant(types.ModuleName, "identity-initialized",
					"SealedGenesisIdentity collection is empty"), true
			}
			return sdk.FormatInvariant(types.ModuleName, "identity-initialized",
				fmt.Sprintf("SealedGenesisIdentity lookup failed: %v", err)), true
		}
		return "", false
	}
}

// CanonicalFieldsInvariant: Identity.EqualCanonical(SealedGenesisIdentity).
// (Invariant 2.)
func CanonicalFieldsInvariant(k *Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		id, err := k.identity.Get(ctx)
		if err != nil {
			return sdk.FormatInvariant(types.ModuleName, "canonical-fields",
				fmt.Sprintf("Identity lookup failed: %v", err)), true
		}
		sealed, err := k.sealedIdentity.Get(ctx)
		if err != nil {
			return sdk.FormatInvariant(types.ModuleName, "canonical-fields",
				fmt.Sprintf("SealedGenesisIdentity lookup failed: %v", err)), true
		}
		if !sealed.EqualCanonical(id) {
			return sdk.FormatInvariant(types.ModuleName, "canonical-fields",
				"identity drifted from sealed: "+sealed.DiffCanonical(id)), true
		}
		return "", false
	}
}

// BankMetadataPresentInvariant: bank metadata must exist for both native
// denoms. (Invariant 3.) H1 guarantees bank keeper is set whenever identity
// is non-empty; if both this invariant and the bank keeper are unset, the
// chain is in legacy single-chain mode and IdentityInitializedInvariant
// will have tripped first.
func BankMetadataPresentInvariant(k *Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		if k.bankKeeper == nil {
			// Defense in depth: should be unreachable in production; legacy
			// mode trips invariant 1 (identity-initialized) first.
			return "", false
		}
		sealed, err := k.sealedIdentity.Get(ctx)
		if err != nil {
			return sdk.FormatInvariant(types.ModuleName, "bank-metadata-present",
				fmt.Sprintf("sealed lookup failed: %v", err)), true
		}
		if _, ok := k.bankKeeper.GetDenomMetaData(ctx, sealed.BondDenom); !ok {
			return sdk.FormatInvariant(types.ModuleName, "bank-metadata-present",
				fmt.Sprintf("no DenomMetadata for bond_denom %q", sealed.BondDenom)), true
		}
		if _, ok := k.bankKeeper.GetDenomMetaData(ctx, sealed.DreamDenom); !ok {
			return sdk.FormatInvariant(types.ModuleName, "bank-metadata-present",
				fmt.Sprintf("no DenomMetadata for dream_denom %q", sealed.DreamDenom)), true
		}
		return "", false
	}
}

// BankMetadataCanonicalInvariant: Symbol/Display of native-denom metadata
// matches sealed values. (Invariant 4.) Skipped when bank keeper not wired.
func BankMetadataCanonicalInvariant(k *Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		if k.bankKeeper == nil {
			return "", false
		}
		sealed, err := k.sealedIdentity.Get(ctx)
		if err != nil {
			return sdk.FormatInvariant(types.ModuleName, "bank-metadata-canonical",
				fmt.Sprintf("sealed lookup failed: %v", err)), true
		}
		check := func(base, wantSym, wantDisp string) string {
			m, ok := k.bankKeeper.GetDenomMetaData(ctx, base)
			if !ok {
				return ""
			}
			if m.Symbol != wantSym || m.Display != wantDisp {
				return fmt.Sprintf("denom %q: Symbol/Display drifted (%q/%q vs sealed %q/%q)",
					base, m.Symbol, m.Display, wantSym, wantDisp)
			}
			return ""
		}
		if msg := check(sealed.BondDenom, sealed.BondDisplaySymbol, strings.ToLower(sealed.BondDisplaySymbol)); msg != "" {
			return sdk.FormatInvariant(types.ModuleName, "bank-metadata-canonical", msg), true
		}
		if msg := check(sealed.DreamDenom, sealed.DreamDisplaySymbol, strings.ToLower(sealed.DreamDisplaySymbol)); msg != "" {
			return sdk.FormatInvariant(types.ModuleName, "bank-metadata-canonical", msg), true
		}
		return "", false
	}
}

// SDKParamsAlignedInvariant: staking.bond_denom and mint.mint_denom must
// match sealed bond_denom. Warning-grade (stop=false on params drift; only
// state corruption escalates to stop=true). (Invariant 5.) Reads
// staking/mint keepers from the *Keeper, set via SetStakingKeeper/
// SetMintKeeper from app.go. If either is nil (test harnesses), the
// invariant returns a clean no-op.
func SDKParamsAlignedInvariant(k *Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		if k.stakingKeeper == nil || k.mintKeeper == nil {
			return "", false
		}
		sealed, err := k.sealedIdentity.Get(ctx)
		if err != nil {
			return sdk.FormatInvariant(types.ModuleName, "sdk-params-aligned",
				fmt.Sprintf("sealed lookup failed: %v", err)), true
		}
		bondDenom, _ := k.stakingKeeper.BondDenom(ctx)
		mintParams, _ := k.mintKeeper.GetParams(ctx)
		mintDenom := mintParams.MintDenom
		if bondDenom == sealed.BondDenom && mintDenom == sealed.BondDenom {
			return "", false
		}
		return sdk.FormatInvariant(types.ModuleName, "sdk-params-aligned",
			fmt.Sprintf("staking.bond_denom=%q or mint.mint_denom=%q diverged from sealed bond_denom=%q",
				bondDenom, mintDenom, sealed.BondDenom)), false
	}
}
