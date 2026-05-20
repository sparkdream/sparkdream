package types

// DONTCOVER

import (
	"cosmossdk.io/errors"
)

// x/session module sentinel errors
var (
	ErrInvalidSigner         = errors.Register(ModuleName, 1100, "expected gov account as only signer for proposal message")
	ErrSessionExists         = errors.Register(ModuleName, 1101, "session already exists for (granter, grantee) pair")
	ErrSessionNotFound       = errors.Register(ModuleName, 1102, "no active session for (granter, grantee) pair")
	ErrSessionExpired        = errors.Register(ModuleName, 1103, "session has passed its expiration time")
	ErrMsgTypeNotAllowed     = errors.Register(ModuleName, 1104, "message type not in session's allowed list")
	ErrMsgTypeForbidden      = errors.Register(ModuleName, 1105, "message type is a session module message (NonDelegableSessionMsgs)")
	ErrMsgTypeNotInAllowlist = errors.Register(ModuleName, 1106, "message type not in current Params.allowed_msg_types")
	ErrSpendLimitExceeded    = errors.Register(ModuleName, 1107, "session gas budget exhausted")
	ErrExecCountExceeded     = errors.Register(ModuleName, 1108, "session execution cap reached")
	ErrMaxSessionsExceeded   = errors.Register(ModuleName, 1109, "granter has too many active sessions")
	ErrMaxMsgTypesExceeded   = errors.Register(ModuleName, 1110, "too many message types in session grant")
	ErrExpirationTooLong     = errors.Register(ModuleName, 1111, "requested expiration exceeds max_expiration")
	ErrSpendLimitTooHigh     = errors.Register(ModuleName, 1112, "requested spend limit exceeds max_spend_limit")
	ErrSelfDelegation        = errors.Register(ModuleName, 1113, "cannot create session where granter == grantee")
	ErrNestedExec            = errors.Register(ModuleName, 1114, "MsgExecSession cannot contain MsgExecSession")
	ErrEmptyMsgs             = errors.Register(ModuleName, 1115, "MsgExecSession must contain at least one inner message")
	ErrTooManyMsgs           = errors.Register(ModuleName, 1116, "MsgExecSession contains too many inner messages (max 10)")
	ErrMixedTransaction      = errors.Register(ModuleName, 1117, "transaction contains MsgExecSession mixed with other message types")
	ErrInvalidExpiration     = errors.Register(ModuleName, 1118, "expiration is in the past")
	ErrMultipleGranters      = errors.Register(ModuleName, 1119, "transaction contains MsgExecSession messages with different granters")
	ErrMultipleSigners       = errors.Register(ModuleName, 1120, "inner message has multiple signers (only single-signer messages supported)")
	ErrInvalidDenom          = errors.Register(ModuleName, 1121, "spend_limit denom is not uspark")
	ErrCeilingExpansion      = errors.Register(ModuleName, 1122, "MsgUpdateParams attempted to add a type to max_allowed_msg_types not already in the current ceiling")
	ErrExceedsCeiling        = errors.Register(ModuleName, 1123, "allowed_msg_types contains a type not in max_allowed_msg_types")
	ErrSpendLimitRequired    = errors.Register(ModuleName, 1124, "spend_limit must be positive for fee delegation to function")
	ErrMaxExecCountRequired  = errors.Register(ModuleName, 1125, "max_exec_count must be positive; 0 is no longer permitted")
	ErrMaxExecCountTooHigh   = errors.Register(ModuleName, 1126, "requested max_exec_count exceeds params.max_exec_count")

	// --- Universal grant errors (P3) ---
	ErrGrantNotFound         = errors.Register(ModuleName, 1200, "grant not found")
	ErrGrantInactive         = errors.Register(ModuleName, 1201, "grant is not in an active status")
	ErrGrantTypeMismatch     = errors.Register(ModuleName, 1202, "grant type mismatch")
	ErrInvalidPayload        = errors.Register(ModuleName, 1203, "MsgCreateGrant payload missing or invalid")
	ErrNoteTooLong           = errors.Register(ModuleName, 1204, "grant note exceeds 256 characters")
	ErrGrantLifetimeTooLong  = errors.Register(ModuleName, 1205, "grant expires_at - created_at exceeds max_grant_lifetime_seconds")
	ErrDenomNotAllowed       = errors.Register(ModuleName, 1206, "denom is not in params.allowed_denoms")
	ErrDreamDenomForbidden   = errors.Register(ModuleName, 1207, "dream denom is forbidden for grant payloads")

	// --- RecurringPull errors (P3) ---
	ErrPeriodTooShort           = errors.Register(ModuleName, 1300, "period_seconds is below params.min_recurring_period_seconds")
	ErrDurationTooLong          = errors.Register(ModuleName, 1301, "RecurringPull duration exceeds params.max_recurring_duration_seconds")
	ErrMaxRecurringPullsExceeded = errors.Register(ModuleName, 1302, "granter has too many active RecurringPull grants")
	ErrAmountNotPositive        = errors.Register(ModuleName, 1303, "amount_per_period must be a positive coin")
	ErrInvalidMaxPerEpoch       = errors.Register(ModuleName, 1304, "max_per_epoch_uspark must parse as a non-negative sdk.Int")
	ErrMaxPerEpochBelowAmount   = errors.Register(ModuleName, 1305, "max_per_epoch_uspark below amount_per_period; one claim would breach the per-epoch ceiling")
	ErrRecurringPullNotDue      = errors.Register(ModuleName, 1306, "next RecurringPull claim window not yet open")
	ErrRecurringPullWindowClosed = errors.Register(ModuleName, 1307, "RecurringPull window has closed without a claim being submitted in time")
	ErrRecurringPullUnauthorized = errors.Register(ModuleName, 1308, "caller is not the RecurringPull grantee on file")
	ErrInsufficientGranterBalance = errors.Register(ModuleName, 1309, "granter has insufficient balance for the requested claim/pull")
	ErrEpochCeilingExceeded      = errors.Register(ModuleName, 1310, "claim would exceed max_per_epoch_uspark for the current UTC day")

	// --- SpendingAllowance errors (P4) ---
	ErrAllowancePeriodTooShort    = errors.Register(ModuleName, 1400, "period_seconds is below params.min_allowance_period_seconds")
	ErrMaxAllowancesExceeded      = errors.Register(ModuleName, 1401, "granter has too many active SpendingAllowance grants")
	ErrRecipientListTooLong       = errors.Register(ModuleName, 1402, "allowed_recipients length exceeds params.max_allowance_recipient_list")
	ErrRecipientNotWhitelisted    = errors.Register(ModuleName, 1403, "recipient is not in the grant's allowed_recipients whitelist")
	ErrAllowanceDenomMismatch     = errors.Register(ModuleName, 1404, "amount denom does not match grant.denom")
	ErrAllowanceAmountBelowMin    = errors.Register(ModuleName, 1405, "amount is below params.min_pull_amount")
	ErrAllowanceBudgetExceeded    = errors.Register(ModuleName, 1406, "pull would exceed max_per_period for the current rolling window")
	ErrAllowanceUnauthorized      = errors.Register(ModuleName, 1407, "caller is not the SpendingAllowance grantee on file")
	ErrAllowanceRecipientIsGranter = errors.Register(ModuleName, 1408, "recipient must not be the granter (anti self-roundtrip)")
	ErrInvalidMinPullAmount       = errors.Register(ModuleName, 1409, "min_pull_amount must parse as a non-negative sdk.Int")

	// --- ScheduledOneshot errors (P5) ---
	ErrOneshotActionMissing      = errors.Register(ModuleName, 1500, "ScheduledOneshot payload must set exactly one action (transfer or exec)")
	ErrFireAtTooSoon             = errors.Register(ModuleName, 1501, "fire_at is below block_time + params.min_schedule_delay_seconds")
	ErrFireAtTooFar              = errors.Register(ModuleName, 1502, "fire_at exceeds block_time + params.max_schedule_horizon_seconds")
	ErrFireToExpiryBufferTooSmall = errors.Register(ModuleName, 1503, "fire_at + params.fire_to_expiry_buffer_seconds exceeds expires_at")
	ErrMaxPendingOneshotsExceeded = errors.Register(ModuleName, 1504, "granter has too many active ScheduledOneshot grants")
	ErrMaxPausedOneshotsExceeded  = errors.Register(ModuleName, 1505, "granter has too many paused ScheduledOneshot grants")
	ErrOneshotGasLimitOutOfRange = errors.Register(ModuleName, 1506, "OneshotExec gas_limit outside [min_oneshot_exec_gas, max_oneshot_exec_gas]")
	ErrOneshotInnerMsgMissing    = errors.Register(ModuleName, 1507, "OneshotExec.msg is required")
	ErrOneshotMsgNotAllowed      = errors.Register(ModuleName, 1508, "OneshotExec inner msg type is not in params.allowed_msg_types")
	ErrOneshotMsgForbidden       = errors.Register(ModuleName, 1509, "OneshotExec inner msg type is in NonDelegableSessionMsgs (anti-recursion)")
	ErrOneshotTargetRevokeGrant  = errors.Register(ModuleName, 1510, "OneshotExec cannot target MsgRevokeGrant/MsgRevokeSession (defense-in-depth)")
	ErrDepositTooSmall           = errors.Register(ModuleName, 1511, "computed deposit is below params.min_oneshot_deposit_uspark")
	ErrRouterUnwired             = errors.Register(ModuleName, 1512, "msg router is not wired; OneshotExec grants cannot be created")
	ErrGrantNotPaused            = errors.Register(ModuleName, 1513, "grant is not in PAUSED_INSUFFICIENT_FUNDS status")
	ErrGrantTerminal             = errors.Register(ModuleName, 1514, "grant is in a terminal status")
	ErrUnauthorizedRetry         = errors.Register(ModuleName, 1515, "retry caller must be either the granter or the grantee on file")
	ErrInvalidOneshotGasPrice    = errors.Register(ModuleName, 1516, "oneshot_gas_price_uspark must parse as a non-negative sdk.Dec")

	// --- Universal revoke / decline errors (P6) ---
	ErrRevokeUnauthorized        = errors.Register(ModuleName, 1600, "caller is not authorized to revoke this grant")
	ErrSelfRevokeNotPermitted    = errors.Register(ModuleName, 1601, "session key cannot include MsgRevokeGrant without allow_self_revoke = true")
	ErrCrossGranterRevoke        = errors.Register(ModuleName, 1602, "session-key revoke must target a grant of the same granter")
	ErrDeclineUnauthorized       = errors.Register(ModuleName, 1603, "caller is not the grantee on file")
	ErrAlreadyDeclined           = errors.Register(ModuleName, 1604, "grant is already DECLINED (one-way; granter must Revoke + CreateGrant to retry)")

	// --- Module-bypass errors (P8) ---
	ErrModuleNotAuthorized = errors.Register(ModuleName, 1700, "caller module address is not in params.authorized_grant_creators")
	ErrBypassDisabled      = errors.Register(ModuleName, 1701, "module-bypass keeper entrypoint requires a non-empty authorized_grant_creators allowlist")
)
