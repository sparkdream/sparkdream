package types

// DONTCOVER

import (
	"cosmossdk.io/errors"
)

// x/futarchy module sentinel errors
var (
	ErrInvalidSigner   = errors.Register(ModuleName, 1100, "expected gov account as only signer for proposal message")
	ErrInvalidDuration = errors.Register(ModuleName, 1101, "expected duration to be positive")
)
