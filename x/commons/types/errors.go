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

	// Recurring Spend Errors
	ErrRecurringSpendNotFound      = errors.Register(ModuleName, 1700, "recurring spend schedule not found")
	ErrRecurringSpendInactive      = errors.Register(ModuleName, 1701, "recurring spend schedule is not active")
	ErrRecurringSpendNotDue        = errors.Register(ModuleName, 1702, "recurring spend period has not elapsed")
	ErrRecurringSpendWindowClosed  = errors.Register(ModuleName, 1703, "recurring spend window has closed (past end_time)")
	ErrRecurringSpendInvalidPeriod = errors.Register(ModuleName, 1704, "recurring spend period is invalid")
	ErrRecurringSpendInvalidWindow = errors.Register(ModuleName, 1705, "recurring spend start/end window is invalid")
	ErrRecurringSpendCapReached    = errors.Register(ModuleName, 1706, "authority has reached max active recurring spends")
	ErrRecurringSpendUnauthorized  = errors.Register(ModuleName, 1707, "caller is not authorized for this recurring spend")
)
