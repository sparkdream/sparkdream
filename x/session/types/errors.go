package types

// DONTCOVER

import (
	"cosmossdk.io/errors"
)

// x/session module sentinel errors
var (
	ErrInvalidSigner           = errors.Register(ModuleName, 1100, "expected gov account as only signer for proposal message")
	ErrSessionExists           = errors.Register(ModuleName, 1101, "session already exists for (granter, grantee) pair")
	ErrSessionNotFound         = errors.Register(ModuleName, 1102, "no active session for (granter, grantee) pair")
	ErrSessionExpired          = errors.Register(ModuleName, 1103, "session has passed its expiration time")
	ErrMsgTypeNotAllowed       = errors.Register(ModuleName, 1104, "message type not in session's allowed list")
	ErrMsgTypeForbidden        = errors.Register(ModuleName, 1105, "message type is a session module message (NonDelegableSessionMsgs)")
	ErrMsgTypeNotInAllowlist   = errors.Register(ModuleName, 1106, "message type not in current Params.allowed_msg_types")
	ErrSpendLimitExceeded      = errors.Register(ModuleName, 1107, "session gas budget exhausted")
	ErrExecCountExceeded       = errors.Register(ModuleName, 1108, "session execution cap reached")
	ErrMaxSessionsExceeded     = errors.Register(ModuleName, 1109, "granter has too many active sessions")
	ErrMaxMsgTypesExceeded     = errors.Register(ModuleName, 1110, "too many message types in session grant")
	ErrExpirationTooLong       = errors.Register(ModuleName, 1111, "requested expiration exceeds max_expiration")
	ErrSpendLimitTooHigh       = errors.Register(ModuleName, 1112, "requested spend limit exceeds max_spend_limit")
	ErrSelfDelegation          = errors.Register(ModuleName, 1113, "cannot create session where granter == grantee")
	ErrNestedExec              = errors.Register(ModuleName, 1114, "MsgExecSession cannot contain MsgExecSession")
	ErrEmptyMsgs               = errors.Register(ModuleName, 1115, "MsgExecSession must contain at least one inner message")
	ErrTooManyMsgs             = errors.Register(ModuleName, 1116, "MsgExecSession contains too many inner messages (max 10)")
	ErrMixedTransaction        = errors.Register(ModuleName, 1117, "transaction contains MsgExecSession mixed with other message types")
	ErrInvalidExpiration       = errors.Register(ModuleName, 1118, "expiration is in the past")
	ErrMultipleGranters        = errors.Register(ModuleName, 1119, "transaction contains MsgExecSession messages with different granters")
	ErrMultipleSigners         = errors.Register(ModuleName, 1120, "inner message has multiple signers (only single-signer messages supported)")
	ErrInvalidDenom            = errors.Register(ModuleName, 1121, "spend_limit denom is not uspark")
	ErrCeilingExpansion        = errors.Register(ModuleName, 1122, "MsgUpdateParams attempted to add a type to max_allowed_msg_types not already in the current ceiling")
	ErrExceedsCeiling          = errors.Register(ModuleName, 1123, "allowed_msg_types contains a type not in max_allowed_msg_types")
)
