package types

// DONTCOVER

import (
	"cosmossdk.io/errors"
)

// x/name module sentinel errors
var (
	ErrInvalidSigner   = errors.Register(ModuleName, 1100, "expected gov account as only signer for proposal message")
	ErrNameTaken       = errors.Register(ModuleName, 1101, "name already taken")
	ErrNameNotFound    = errors.Register(ModuleName, 1102, "name not found")
	ErrInvalidName     = errors.Register(ModuleName, 1103, "name is invalid")
	ErrNameReserved    = errors.Register(ModuleName, 1104, "name is reserved")
	ErrTooManyNames    = errors.Register(ModuleName, 1105, "address has reached maximum number of names")
	ErrDisputeNotFound = errors.Register(ModuleName, 1106, "dispute not found")
)
