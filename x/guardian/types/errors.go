package types

// DONTCOVER

import (
	"cosmossdk.io/errors"
)

// x/guardian sentinel errors.
var (
	ErrUnauthorized       = errors.Register(ModuleName, 1300, "guardian: authority does not match configured authority")
	ErrInnerMsgNotAllowed = errors.Register(ModuleName, 1301, "guardian: inner msg type not in allowlist")
	ErrInnerMsgInvalid    = errors.Register(ModuleName, 1302, "guardian: failed to unpack inner msg")
	ErrImmutableField     = errors.Register(ModuleName, 1303, "guardian: attempt to change immutable field")
	ErrRoutingFailed      = errors.Register(ModuleName, 1304, "guardian: inner msg routing failed")
	ErrIdentityNotSealed  = errors.Register(ModuleName, 1305, "guardian: identity not yet sealed; cannot validate native-denom constraints")
)
