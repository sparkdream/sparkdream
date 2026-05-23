package types

// DONTCOVER

import (
	"cosmossdk.io/errors"
)

// x/identity sentinel errors. See §12 of docs/x-identity-spec.md.
var (
	ErrInvalidChainName        = errors.Register(ModuleName, 1200, "invalid chain_human_name")
	ErrInvalidTickerPrefix     = errors.Register(ModuleName, 1201, "invalid chain_ticker_prefix")
	ErrInvalidBondDenom        = errors.Register(ModuleName, 1202, "invalid bond_denom")
	ErrInvalidBondSymbol       = errors.Register(ModuleName, 1203, "invalid bond_display_symbol")
	ErrInvalidBondDisplayName  = errors.Register(ModuleName, 1204, "invalid bond_display_name")
	ErrInvalidDreamDenom       = errors.Register(ModuleName, 1205, "invalid dream_denom")
	ErrInvalidDreamSymbol      = errors.Register(ModuleName, 1206, "invalid dream_display_symbol")
	ErrInvalidDreamDisplayName = errors.Register(ModuleName, 1207, "invalid dream_display_name")
	ErrInvalidDecimals         = errors.Register(ModuleName, 1208, "decimals out of range [0, 18]")
	ErrDenomCollision          = errors.Register(ModuleName, 1209, "bond_denom and dream_denom must differ")
	ErrSymbolCollision         = errors.Register(ModuleName, 1210, "bond_display_symbol and dream_display_symbol must differ")
	ErrInvalidFoundedAt        = errors.Register(ModuleName, 1211, "founded_at must be > 0")
	ErrIdentityNotInitialized  = errors.Register(ModuleName, 1212, "chain identity not initialized; genesis bug")
	ErrIdentityImmutable       = errors.Register(ModuleName, 1213, "chain identity is immutable post-genesis")
	ErrIdentityAlreadySealed   = errors.Register(ModuleName, 1214, "sealed genesis identity already present; cannot overwrite")
	ErrChainNameInconsistent   = errors.Register(ModuleName, 1215, "chain_human_name must be derivable from cosmos chain_id")
)
