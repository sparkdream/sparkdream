package types

// DONTCOVER

import (
	"cosmossdk.io/errors"
)

// x/commons module sentinel errors
var (
	ErrInvalidSigner = errors.Register(ModuleName, 1100, "expected gov account as only signer for proposal message")

	// Governance & Group Errors
	ErrGroupNotFound     = errors.Register(ModuleName, 1600, "group not found in registry")
	ErrInvalidGroupSize  = errors.Register(ModuleName, 1601, "group member count is outside defined bounds")
	ErrRateLimitExceeded = errors.Register(ModuleName, 1602, "spending limit or update rate limit exceeded")
	ErrGroupNotActive    = errors.Register(ModuleName, 1603, "group is not yet active (shell group)")
	ErrGroupExpired      = errors.Register(ModuleName, 1604, "group term has expired")
)
