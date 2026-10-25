package types

// DONTCOVER

import (
	"cosmossdk.io/errors"
)

// x/vault module sentinel errors
var (
	ErrInvalidSigner        = errors.Register(ModuleName, 1100, "expected gov account as only signer for proposal message")
	ErrCreationDisabled     = errors.Register(ModuleName, 1101, "vault creation is disabled")
	ErrInvalidSignerSet     = errors.Register(ModuleName, 1102, "invalid signer set")
	ErrInvalidThreshold     = errors.Register(ModuleName, 1103, "invalid threshold")
	ErrInvalidProposalTTL   = errors.Register(ModuleName, 1104, "invalid proposal ttl")
	ErrInvalidLabel         = errors.Register(ModuleName, 1105, "invalid label")
	ErrVaultNotFound        = errors.Register(ModuleName, 1106, "vault not found")
	ErrNotSigner            = errors.Register(ModuleName, 1107, "signer is not a current signer of the vault")
	ErrProposalNotFound     = errors.Register(ModuleName, 1108, "proposal not found")
	ErrProposalNotPending   = errors.Register(ModuleName, 1109, "proposal is not pending")
	ErrTooManyPending       = errors.Register(ModuleName, 1110, "too many pending proposals for this proposer")
	ErrNoMessages           = errors.Register(ModuleName, 1111, "proposal has no messages")
	ErrTooManyMessages      = errors.Register(ModuleName, 1112, "too many messages in proposal")
	ErrInnerMsgDenied       = errors.Register(ModuleName, 1113, "message type may not be executed by a vault")
	ErrInnerSignerMismatch  = errors.Register(ModuleName, 1114, "inner message signer is not the vault address")
	ErrMessagesHashMismatch = errors.Register(ModuleName, 1115, "messages_hash does not match the proposal")
	ErrAlreadyApproved      = errors.Register(ModuleName, 1116, "signer already approved this proposal")
	ErrNotApproved          = errors.Register(ModuleName, 1117, "signer has not approved this proposal")
	ErrThresholdNotMet      = errors.Register(ModuleName, 1118, "approving weight below threshold")
	ErrNotProposer          = errors.Register(ModuleName, 1119, "only the proposer or the vault may cancel")
	ErrNotPersonalAddress   = errors.Register(ModuleName, 1120, "signer must be a personal address")
	ErrInvalidMemo          = errors.Register(ModuleName, 1121, "invalid memo")
	ErrProposalExpired      = errors.Register(ModuleName, 1122, "proposal has expired")
	ErrConfigChanged        = errors.Register(ModuleName, 1123, "vault configuration changed since the proposal was created")
	ErrAddressOccupied      = errors.Register(ModuleName, 1124, "vault address is held by a non-base account")
)
