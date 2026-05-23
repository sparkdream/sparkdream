package keeper

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"sparkdream/x/identity/types"
)

// InitGenesis persists the identity record, seals it for invariant
// enforcement, and seeds bank metadata for the two native tokens. Validation
// runs first; no state is written when validation fails. See §6.2.
//
// V1 DEVIATION FROM SPEC: when the supplied identity is empty (the chain is
// running in single-chain legacy mode without a configured identity),
// InitGenesis returns nil and writes no state. Queries against an
// uninitialized chain return NotFound. See
// docs/x-identity-implementation-decisions.md.
func (k Keeper) InitGenesis(ctx context.Context, gs types.GenesisState) error {
	if gs.Identity.BondDenom == "" && gs.Identity.DreamDenom == "" {
		return nil
	}
	if err := gs.Identity.Validate(); err != nil {
		return fmt.Errorf("invalid chain identity: %w", err)
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := gs.Identity.ValidateAgainstChainID(sdkCtx.ChainID(), gs.AllowChainIdMismatch); err != nil {
		return err
	}
	// A non-empty identity requires a bank keeper for the native-denom
	// metadata seed. The standalone-test path can pass a nil bank to
	// NewKeeper, but it must not also pass a non-empty identity to
	// InitGenesis (use SetBankKeeper with a mock before calling InitGenesis).
	// Production wiring sets the (guarded) bank keeper in app.go before any
	// module's InitGenesis runs; if this check fires in production, the
	// app.go wiring is broken.
	if k.bankKeeper == nil {
		return fmt.Errorf("identity: InitGenesis with a non-empty identity requires a bank keeper; check app.go wiring (SetBankKeeper) or pass a mock via SetBankKeeper in tests")
	}

	if err := k.setChainIdentity(ctx, gs.Identity); err != nil {
		return err
	}
	if err := k.sealGenesisIdentity(ctx, gs.Identity); err != nil {
		return err
	}
	if err := k.registerNativeDenomMetadata(ctx, gs.Identity); err != nil {
		return err
	}
	return nil
}

// ExportGenesis returns the current mutable identity. Returns an error if the
// mutable record is missing or has drifted from the sealed record (the
// latter is state corruption the §16 invariant should have caught earlier).
func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	id, err := k.GetChainIdentity(ctx)
	if err != nil {
		return nil, fmt.Errorf("ExportGenesis: mutable Identity missing on a live chain (corrupt state): %w", err)
	}
	sealed, err := k.GetSealedIdentity(ctx)
	if err != nil {
		return nil, fmt.Errorf("ExportGenesis: SealedGenesisIdentity missing on a live chain (corrupt state): %w", err)
	}
	if !sealed.EqualCanonical(id) {
		return nil, fmt.Errorf("ExportGenesis: sealed identity disagrees with live identity in canonical fields; diff: %s", sealed.DiffCanonical(id))
	}
	return &types.GenesisState{
		Identity:             id,
		AllowChainIdMismatch: false,
	}, nil
}

// sealGenesisIdentity writes the sealed record exactly once. Idempotent under
// state-sync replay against the same chain (existing == supplied); rejects
// any divergent re-import with a field-level diff. See §6.5.
func (k Keeper) sealGenesisIdentity(ctx context.Context, id types.ChainIdentity) error {
	existing, err := k.sealedIdentity.Get(ctx)
	switch {
	case errors.Is(err, collections.ErrNotFound):
		return k.sealedIdentity.Set(ctx, id)
	case err != nil:
		return err
	case existing.EqualCanonical(id):
		return nil
	default:
		return errorsmod.Wrapf(types.ErrIdentityAlreadySealed,
			"sealed identity conflicts with InitGenesis identity; diff: %s",
			existing.DiffCanonical(id))
	}
}

// registerNativeDenomMetadata writes bank metadata for both native tokens.
// Called once at InitGenesis. The bank keeper here may be the guarded
// wrapper; the guard treats pre-seal writes (this very call) as the
// legitimate seed window. The caller (InitGenesis) has already verified
// k.bankKeeper is non-nil.
func (k Keeper) registerNativeDenomMetadata(ctx context.Context, id types.ChainIdentity) error {
	sparkMeta := banktypes.Metadata{
		Description: fmt.Sprintf("%s, native gas and staking token of the %s federated Spark Dream chain",
			id.BondDisplayName, id.ChainHumanName),
		DenomUnits: []*banktypes.DenomUnit{
			{Denom: id.BondDenom, Exponent: 0, Aliases: []string{"micro" + strings.ToLower(id.BondDisplaySymbol)}},
			{Denom: strings.ToLower(id.BondDisplaySymbol), Exponent: id.BondDisplayDecimals},
		},
		Base:    id.BondDenom,
		Display: strings.ToLower(id.BondDisplaySymbol),
		Name:    id.BondDisplayName,
		Symbol:  id.BondDisplaySymbol,
	}
	k.bankKeeper.SetDenomMetaData(ctx, sparkMeta)

	dreamMeta := banktypes.Metadata{
		Description: fmt.Sprintf("%s, internal reputation token of the %s federated Spark Dream chain (non-transferable, non-IBC)",
			id.DreamDisplayName, id.ChainHumanName),
		DenomUnits: []*banktypes.DenomUnit{
			{Denom: id.DreamDenom, Exponent: 0, Aliases: []string{"micro" + strings.ToLower(id.DreamDisplaySymbol)}},
			{Denom: strings.ToLower(id.DreamDisplaySymbol), Exponent: id.DreamDisplayDecimals},
		},
		Base:    id.DreamDenom,
		Display: strings.ToLower(id.DreamDisplaySymbol),
		Name:    id.DreamDisplayName,
		Symbol:  id.DreamDisplaySymbol,
	}
	k.bankKeeper.SetDenomMetaData(ctx, dreamMeta)
	return nil
}
