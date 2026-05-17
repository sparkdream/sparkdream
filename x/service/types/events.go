package types

import (
	"fmt"
	"strconv"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Event types — see x-service-spec.md §12. One-to-one mapping with the
// spec list; the constants here are the `sdk.Event.Type` values
// emitted from every write path.
const (
	EventTypeOperatorRegistered             = "service.operator_registered"
	EventTypeOperatorToppedUp               = "service.operator_topped_up"
	EventTypeOperatorUnderfunded            = "service.operator_underfunded"
	EventTypeOperatorUnbondStarted          = "service.operator_unbond_started"
	EventTypeOperatorUnbondCompleted        = "service.operator_unbond_completed"
	EventTypeOperatorArchived               = "service.operator_archived"
	EventTypeOperatorSlashed                = "service.operator_slashed"
	EventTypeOperatorDissolved              = "service.operator_dissolved"
	EventTypeTier1EscrowLocked              = "service.tier1_escrow_locked"
	EventTypeTier1EscrowReleased            = "service.tier1_escrow_released"
	EventTypeReportFiled                    = "service.report_filed"
	EventTypeReportResolvedT1               = "service.report_resolved_t1"
	EventTypeReportEscalated                = "service.report_escalated"
	EventTypeReportResolvedT2               = "service.report_resolved_t2"
	EventTypeReportAutoDismissed            = "service.report_auto_dismissed"
	EventTypeReportAutoTimeout              = "service.report_auto_timeout"
	EventTypeReportClosedDissolved          = "service.report_closed_dissolved"
	EventTypeReportContested                = "service.report_contested"
	EventTypeControllerTransferCaseOpened   = "service.controller_transfer_case_opened"
	EventTypeControllerTransferCaseFinalized = "service.controller_transfer_case_finalized"
	EventTypeMetadataUpdated                = "service.metadata_updated"
	EventTypeControllerTransferred          = "service.controller_transferred"
	EventTypeServiceTypeUpdated             = "service.service_type_updated"
)

// Attribute keys. Stable identifiers — downstream indexers depend on
// these so consider deprecation rather than renames once shipped.
const (
	AttrAddress               = "address"
	AttrServiceType           = "service_type"
	AttrController            = "controller"
	AttrBond                  = "bond"
	AttrAdditionalBond        = "additional_bond"
	AttrNewBond               = "new_bond"
	AttrCurrentBond           = "current_bond"
	AttrMinBond               = "min_bond"
	AttrGraceUntil            = "grace_until"
	AttrCompleteAt            = "complete_at"
	AttrReturnedBond          = "returned_bond"
	AttrFinalStatus           = "final_status"
	AttrRetiredAt             = "retired_at"
	AttrSlashAmount           = "slash_amount"
	AttrTier                  = "tier"
	AttrReportID              = "report_id"
	AttrConfiscatedBond       = "confiscated_bond"
	AttrEscrowID              = "escrow_id"
	AttrReleaseAt             = "release_at"
	AttrAmount                = "amount"
	AttrDestination           = "destination"
	AttrReporter              = "reporter"
	AttrOperator              = "operator"
	AttrDeposit               = "deposit"
	AttrVerdict               = "verdict"
	AttrJuryCaseID            = "jury_case_id"
	AttrProposedSlashBps      = "proposed_slash_bps"
	AttrDissolve              = "dissolve"
	AttrDepositRefunded       = "deposit_refunded"
	AttrEscrowReturnedToBond  = "escrow_returned_to_bond"
	AttrRestoredBond          = "restored_bond"
	AttrOpener                = "opener"
	AttrProposedNewController = "proposed_new_controller"
	AttrNewController         = "new_controller"
	AttrDepositDestination    = "deposit_destination"
	AttrEnabled               = "enabled"
	AttrChangedFields         = "changed_fields"
	AttrSource                = "source"
)

// Event destination constants for tier-1 escrow release.
const (
	EscrowDestCommunityPool = "COMMUNITY_POOL"
	EscrowDestBondRestored  = "BOND_RESTORED"
)

// Unbond source constants for `service.operator_unbond_started`.
const (
	UnbondSourceVoluntary         = "VOLUNTARY"
	UnbondSourceForcedUnderfunded = "FORCED_UNDERFUNDED"
)

// Controller-transfer finalize deposit destination constants.
const (
	CtfDepositOpenerRefunded = "OPENER_REFUNDED"
	CtfDepositCommunityPool  = "COMMUNITY_POOL"
)

// Slash tier constants for service.operator_slashed.
const (
	TierTier1 = "T1"
	TierTier2 = "T2"
)

// Helpers to keep handlers terse. Each `New*Event` returns an
// sdk.Event ready to feed to `ctx.EventManager().EmitEvent(...)`.

func NewOperatorRegisteredEvent(addr, serviceType, controller string, bond sdk.Coin) sdk.Event {
	return sdk.NewEvent(EventTypeOperatorRegistered,
		sdk.NewAttribute(AttrAddress, addr),
		sdk.NewAttribute(AttrServiceType, serviceType),
		sdk.NewAttribute(AttrController, controller),
		sdk.NewAttribute(AttrBond, bond.String()),
	)
}

func NewOperatorToppedUpEvent(addr, serviceType string, additional, newBond sdk.Coin) sdk.Event {
	return sdk.NewEvent(EventTypeOperatorToppedUp,
		sdk.NewAttribute(AttrAddress, addr),
		sdk.NewAttribute(AttrServiceType, serviceType),
		sdk.NewAttribute(AttrAdditionalBond, additional.String()),
		sdk.NewAttribute(AttrNewBond, newBond.String()),
	)
}

func NewOperatorUnderfundedEvent(addr, serviceType string, currentBond, minBond sdk.Coin, graceUntil int64) sdk.Event {
	return sdk.NewEvent(EventTypeOperatorUnderfunded,
		sdk.NewAttribute(AttrAddress, addr),
		sdk.NewAttribute(AttrServiceType, serviceType),
		sdk.NewAttribute(AttrCurrentBond, currentBond.String()),
		sdk.NewAttribute(AttrMinBond, minBond.String()),
		sdk.NewAttribute(AttrGraceUntil, fmt.Sprintf("%d", graceUntil)),
	)
}

func NewOperatorUnbondStartedEvent(addr, serviceType string, completeAt int64, source string) sdk.Event {
	return sdk.NewEvent(EventTypeOperatorUnbondStarted,
		sdk.NewAttribute(AttrAddress, addr),
		sdk.NewAttribute(AttrServiceType, serviceType),
		sdk.NewAttribute(AttrCompleteAt, fmt.Sprintf("%d", completeAt)),
		sdk.NewAttribute(AttrSource, source),
	)
}

func NewOperatorUnbondCompletedEvent(addr, serviceType string, returned sdk.Coin) sdk.Event {
	return sdk.NewEvent(EventTypeOperatorUnbondCompleted,
		sdk.NewAttribute(AttrAddress, addr),
		sdk.NewAttribute(AttrServiceType, serviceType),
		sdk.NewAttribute(AttrReturnedBond, returned.String()),
	)
}

func NewOperatorArchivedEvent(addr, serviceType, finalStatus string, retiredAt int64) sdk.Event {
	return sdk.NewEvent(EventTypeOperatorArchived,
		sdk.NewAttribute(AttrAddress, addr),
		sdk.NewAttribute(AttrServiceType, serviceType),
		sdk.NewAttribute(AttrFinalStatus, finalStatus),
		sdk.NewAttribute(AttrRetiredAt, fmt.Sprintf("%d", retiredAt)),
	)
}

func NewOperatorSlashedEvent(addr, serviceType string, slashAmount, newBond sdk.Coin, tier string, reportID uint64) sdk.Event {
	return sdk.NewEvent(EventTypeOperatorSlashed,
		sdk.NewAttribute(AttrAddress, addr),
		sdk.NewAttribute(AttrServiceType, serviceType),
		sdk.NewAttribute(AttrSlashAmount, slashAmount.String()),
		sdk.NewAttribute(AttrNewBond, newBond.String()),
		sdk.NewAttribute(AttrTier, tier),
		sdk.NewAttribute(AttrReportID, fmt.Sprintf("%d", reportID)),
	)
}

func NewOperatorDissolvedEvent(addr, serviceType string, confiscated sdk.Coin, reportID uint64) sdk.Event {
	return sdk.NewEvent(EventTypeOperatorDissolved,
		sdk.NewAttribute(AttrAddress, addr),
		sdk.NewAttribute(AttrServiceType, serviceType),
		sdk.NewAttribute(AttrConfiscatedBond, confiscated.String()),
		sdk.NewAttribute(AttrReportID, fmt.Sprintf("%d", reportID)),
	)
}

func NewTier1EscrowLockedEvent(escrowID, reportID uint64, addr, serviceType string, amount sdk.Coin, releaseAt int64) sdk.Event {
	return sdk.NewEvent(EventTypeTier1EscrowLocked,
		sdk.NewAttribute(AttrEscrowID, fmt.Sprintf("%d", escrowID)),
		sdk.NewAttribute(AttrReportID, fmt.Sprintf("%d", reportID)),
		sdk.NewAttribute(AttrAddress, addr),
		sdk.NewAttribute(AttrServiceType, serviceType),
		sdk.NewAttribute(AttrAmount, amount.String()),
		sdk.NewAttribute(AttrReleaseAt, fmt.Sprintf("%d", releaseAt)),
	)
}

func NewTier1EscrowReleasedEvent(escrowID, reportID uint64, addr, serviceType string, amount sdk.Coin, destination string) sdk.Event {
	return sdk.NewEvent(EventTypeTier1EscrowReleased,
		sdk.NewAttribute(AttrEscrowID, fmt.Sprintf("%d", escrowID)),
		sdk.NewAttribute(AttrReportID, fmt.Sprintf("%d", reportID)),
		sdk.NewAttribute(AttrAddress, addr),
		sdk.NewAttribute(AttrServiceType, serviceType),
		sdk.NewAttribute(AttrAmount, amount.String()),
		sdk.NewAttribute(AttrDestination, destination),
	)
}

func NewReportFiledEvent(reportID uint64, reporter, op, serviceType string, deposit sdk.Coin) sdk.Event {
	return sdk.NewEvent(EventTypeReportFiled,
		sdk.NewAttribute(AttrReportID, fmt.Sprintf("%d", reportID)),
		sdk.NewAttribute(AttrReporter, reporter),
		sdk.NewAttribute(AttrOperator, op),
		sdk.NewAttribute(AttrServiceType, serviceType),
		sdk.NewAttribute(AttrDeposit, deposit.String()),
	)
}

func NewReportResolvedT1Event(reportID uint64, verdict string, slashAmount sdk.Coin) sdk.Event {
	return sdk.NewEvent(EventTypeReportResolvedT1,
		sdk.NewAttribute(AttrReportID, fmt.Sprintf("%d", reportID)),
		sdk.NewAttribute(AttrVerdict, verdict),
		sdk.NewAttribute(AttrSlashAmount, slashAmount.String()),
	)
}

func NewReportEscalatedEvent(reportID, juryCaseID uint64, proposedSlashBps uint32) sdk.Event {
	return sdk.NewEvent(EventTypeReportEscalated,
		sdk.NewAttribute(AttrReportID, fmt.Sprintf("%d", reportID)),
		sdk.NewAttribute(AttrJuryCaseID, fmt.Sprintf("%d", juryCaseID)),
		sdk.NewAttribute(AttrProposedSlashBps, fmt.Sprintf("%d", proposedSlashBps)),
	)
}

func NewReportResolvedT2Event(reportID uint64, verdict string, slashAmount sdk.Coin, dissolve bool) sdk.Event {
	return sdk.NewEvent(EventTypeReportResolvedT2,
		sdk.NewAttribute(AttrReportID, fmt.Sprintf("%d", reportID)),
		sdk.NewAttribute(AttrVerdict, verdict),
		sdk.NewAttribute(AttrSlashAmount, slashAmount.String()),
		sdk.NewAttribute(AttrDissolve, strconv.FormatBool(dissolve)),
	)
}

func NewReportAutoDismissedEvent(reportID uint64, depositRefunded sdk.Coin) sdk.Event {
	return sdk.NewEvent(EventTypeReportAutoDismissed,
		sdk.NewAttribute(AttrReportID, fmt.Sprintf("%d", reportID)),
		sdk.NewAttribute(AttrDepositRefunded, depositRefunded.String()),
	)
}

func NewReportAutoTimeoutEvent(reportID uint64, depositRefunded, escrowReturned sdk.Coin) sdk.Event {
	return sdk.NewEvent(EventTypeReportAutoTimeout,
		sdk.NewAttribute(AttrReportID, fmt.Sprintf("%d", reportID)),
		sdk.NewAttribute(AttrDepositRefunded, depositRefunded.String()),
		sdk.NewAttribute(AttrEscrowReturnedToBond, escrowReturned.String()),
	)
}

func NewReportClosedDissolvedEvent(reportID uint64, depositRefunded sdk.Coin) sdk.Event {
	return sdk.NewEvent(EventTypeReportClosedDissolved,
		sdk.NewAttribute(AttrReportID, fmt.Sprintf("%d", reportID)),
		sdk.NewAttribute(AttrDepositRefunded, depositRefunded.String()),
	)
}

func NewReportContestedEvent(reportID uint64, restoredBond sdk.Coin) sdk.Event {
	return sdk.NewEvent(EventTypeReportContested,
		sdk.NewAttribute(AttrReportID, fmt.Sprintf("%d", reportID)),
		sdk.NewAttribute(AttrRestoredBond, restoredBond.String()),
	)
}

func NewControllerTransferCaseOpenedEvent(operator, serviceType, opener, proposedNewController string, juryCaseID uint64) sdk.Event {
	return sdk.NewEvent(EventTypeControllerTransferCaseOpened,
		sdk.NewAttribute(AttrOperator, operator),
		sdk.NewAttribute(AttrServiceType, serviceType),
		sdk.NewAttribute(AttrOpener, opener),
		sdk.NewAttribute(AttrProposedNewController, proposedNewController),
		sdk.NewAttribute(AttrJuryCaseID, fmt.Sprintf("%d", juryCaseID)),
	)
}

func NewControllerTransferCaseFinalizedEvent(juryCaseID uint64, verdict, depositDest string) sdk.Event {
	return sdk.NewEvent(EventTypeControllerTransferCaseFinalized,
		sdk.NewAttribute(AttrJuryCaseID, fmt.Sprintf("%d", juryCaseID)),
		sdk.NewAttribute(AttrVerdict, verdict),
		sdk.NewAttribute(AttrDepositDestination, depositDest),
	)
}

func NewMetadataUpdatedEvent(addr, serviceType string) sdk.Event {
	return sdk.NewEvent(EventTypeMetadataUpdated,
		sdk.NewAttribute(AttrAddress, addr),
		sdk.NewAttribute(AttrServiceType, serviceType),
	)
}

func NewControllerTransferredEvent(addr, serviceType, newController string, juryCaseID uint64) sdk.Event {
	return sdk.NewEvent(EventTypeControllerTransferred,
		sdk.NewAttribute(AttrAddress, addr),
		sdk.NewAttribute(AttrServiceType, serviceType),
		sdk.NewAttribute(AttrNewController, newController),
		sdk.NewAttribute(AttrJuryCaseID, fmt.Sprintf("%d", juryCaseID)),
	)
}

func NewServiceTypeUpdatedEvent(serviceType string, enabled bool, changedFields string) sdk.Event {
	return sdk.NewEvent(EventTypeServiceTypeUpdated,
		sdk.NewAttribute(AttrServiceType, serviceType),
		sdk.NewAttribute(AttrEnabled, strconv.FormatBool(enabled)),
		sdk.NewAttribute(AttrChangedFields, changedFields),
	)
}
