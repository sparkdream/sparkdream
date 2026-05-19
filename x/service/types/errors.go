package types

// DONTCOVER

import (
	"cosmossdk.io/errors"
)

// x/service module sentinel errors. Codes match x-service-spec.md §9
// (1-40). Codes ≥ 1100 are reserved for ignite-scaffolded errors that
// don't have a spec counterpart; do not reuse 1-40 for those.
var (
	ErrServiceTypeNotFound             = errors.Register(ModuleName, 1, "service type not found")
	ErrServiceTypeDisabled             = errors.Register(ModuleName, 2, "service type is disabled")
	ErrInvalidServiceType              = errors.Register(ModuleName, 3, "invalid service type")
	ErrOperatorNotFound                = errors.Register(ModuleName, 4, "operator not found")
	ErrOperatorAlreadyExists           = errors.Register(ModuleName, 5, "operator already exists")
	ErrOperatorSlashed                 = errors.Register(ModuleName, 6, "operator is slashed")
	ErrOperatorNotActive               = errors.Register(ModuleName, 7, "operator is not active")
	ErrOperatorUnbonding               = errors.Register(ModuleName, 8, "operator is unbonding")
	ErrInsufficientBond                = errors.Register(ModuleName, 9, "insufficient bond")
	ErrBondDenomMismatch               = errors.Register(ModuleName, 10, "bond denom mismatch")
	ErrUnbondingPeriodNotElapsed       = errors.Register(ModuleName, 11, "unbonding period not elapsed")
	ErrOpenReports                     = errors.Register(ModuleName, 12, "operator has open reports")
	ErrReportNotFound                  = errors.Register(ModuleName, 13, "report not found")
	ErrReportAlreadyResolved           = errors.Register(ModuleName, 14, "report already resolved")
	ErrReporterTrustLevelTooLow        = errors.Register(ModuleName, 15, "reporter trust level too low")
	ErrInsufficientReportDeposit       = errors.Register(ModuleName, 16, "insufficient report deposit balance")
	ErrSlashCapExceeded                = errors.Register(ModuleName, 17, "tier-1 slash bps exceeds per-slash cap")
	ErrTier1AggregateCapExceeded       = errors.Register(ModuleName, 18, "tier-1 aggregate cap exceeded for current window")
	ErrTier1CooldownActive             = errors.Register(ModuleName, 19, "tier-1 cooldown active")
	ErrContestWindowExpired            = errors.Register(ModuleName, 20, "contest window expired")
	ErrUnauthorizedController          = errors.Register(ModuleName, 21, "signer is not the operator's controller")
	ErrUnauthorizedCouncilResolver     = errors.Register(ModuleName, 22, "signer is not a Commons Council Operations Committee member")
	ErrUnauthorizedGovAuthority        = errors.Register(ModuleName, 23, "signer is not the x/gov module account")
	ErrInvalidMetadataSize             = errors.Register(ModuleName, 24, "metadata exceeds max size")
	ErrInvalidReasonSize               = errors.Register(ModuleName, 25, "reason exceeds max size")
	ErrTooManyOperatorsForAddress      = errors.Register(ModuleName, 26, "address has reached max active operators")
	ErrRefileCooldownActive            = errors.Register(ModuleName, 27, "refile cooldown active")
	ErrInvalidVerdict                  = errors.Register(ModuleName, 28, "invalid verdict for context")
	ErrInvalidParams                   = errors.Register(ModuleName, 29, "invalid params")
	ErrInvalidServiceTypeConfig        = errors.Register(ModuleName, 30, "invalid service type config")
	ErrSelfController                  = errors.Register(ModuleName, 31, "controller cannot equal operator address")
	ErrControllerNotGroup              = errors.Register(ModuleName, 32, "controller is not a registered group address")
	ErrOperatorPreviouslySlashed       = errors.Register(ModuleName, 33, "operator address has a prior SLASHED record for this service type")
	ErrReporterIsControllerMember      = errors.Register(ModuleName, 34, "reporter is a member of the operator's controller group")
	ErrReporterRateLimitExceeded       = errors.Register(ModuleName, 35, "reporter rate limit exceeded")
	ErrControllerTransferCaseAlreadyOpen = errors.Register(ModuleName, 36, "a controller-transfer case is already open for this operator")
	ErrEscrowStillActive               = errors.Register(ModuleName, 37, "tier-1 escrow entries are still within contest window")
	ErrPaginationLimitExceeded         = errors.Register(ModuleName, 38, "pagination limit exceeded")
	ErrControllerNoLongerEligible      = errors.Register(ModuleName, 39, "proposed controller is no longer eligible (group dissolved or empty)")
	ErrControllerTransferCaseNotFound  = errors.Register(ModuleName, 40, "controller transfer case not found")
	ErrJuryVerdictMismatch             = errors.Register(ModuleName, 41, "resolver's submitted verdict does not match the underlying JuryReview")
	ErrUnauthorizedSystemCaller        = errors.Register(ModuleName, 42, "caller is not in the system-report allowlist")
	ErrSystemReportRateLimited         = errors.Register(ModuleName, 43, "system report rate limit exceeded for caller")
	ErrInvalidDedupeKey                = errors.Register(ModuleName, 44, "dedupe key must be non-empty")

	// ErrInvalidSigner is the ignite-scaffolded generic invalid-signer
	// error retained from the module skeleton; retained as 1100 for
	// backward compatibility with auto-generated handlers that don't yet
	// use spec-specific errors.
	ErrInvalidSigner = errors.Register(ModuleName, 1100, "expected gov account as only signer for proposal message")
)
