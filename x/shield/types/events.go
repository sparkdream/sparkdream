package types

// Event types for the x/shield module.
const (
	// Immediate mode
	EventTypeShieldedExec = "shielded_exec"

	// Encrypted batch mode
	EventTypeShieldedQueued      = "shielded_queued"
	EventTypeShieldBatchExecuted = "shield_batch_executed"
	EventTypeShieldBatchExpired  = "shield_batch_expired"
	EventTypeShieldBatchSkipped  = "shield_batch_skipped"

	// Decryption key
	EventTypeShieldDecryptionKeyAvailable = "shield_decryption_key_available"
	EventTypeShieldDecryptionKeyFailed    = "shield_decryption_key_failed"

	// Funding
	EventTypeShieldFunded        = "shield_funded"
	EventShieldFundingCapReached = "shield_funding_cap_reached"

	// Registration
	EventTypeShieldedOpRegistered   = "shielded_op_registered"
	EventTypeShieldedOpDeregistered = "shielded_op_deregistered"

	// DKG lifecycle
	EventTypeShieldDKGTriggered     = "shield_dkg_triggered"
	EventTypeShieldTLERegistered    = "shield_tle_registered"
	EventTypeShieldDKGOpened        = "shield_dkg_opened"
	EventTypeShieldDKGRegistration  = "shield_dkg_registration"
	EventTypeShieldDKGContribution  = "shield_dkg_contribution"
	EventTypeShieldDKGComplete      = "shield_dkg_complete"
	EventTypeShieldDKGActivated     = "shield_dkg_activated"
	EventTypeShieldDKGFailed        = "shield_dkg_failed"
	EventTypeShieldValidatorDrift   = "shield_validator_set_drift"

	// TLE liveness
	EventTypeTLEMiss = "shield_tle_miss"
	EventTypeTLEJail = "shield_tle_jail"

	// Attribute keys
	AttributeKeyMessageType       = "message_type"
	AttributeKeyNullifierDomain   = "nullifier_domain"
	AttributeKeyNullifierHex      = "nullifier_hex"
	AttributeKeyExecMode          = "exec_mode"
	AttributeKeyPendingOpId       = "pending_op_id"
	AttributeKeyTargetEpoch       = "target_epoch"
	AttributeKeyEpoch             = "epoch"
	AttributeKeyBatchSize         = "batch_size"
	AttributeKeyExecuted          = "executed"
	AttributeKeyDropped           = "dropped"
	AttributeKeyCount             = "count"
	AttributeKeyCutoffEpoch       = "cutoff_epoch"
	AttributeKeySharesSubmitted   = "shares_submitted"
	AttributeKeyThresholdRequired = "threshold_required"
	AttributeKeyAmount            = "amount"
	AttributeKeyDay               = "day"
	AttributeKeyNewBalance        = "new_balance"
	AttributeKeyTotalFunded       = "total_funded"
	AttributeKeyCap               = "cap"
	AttributeKeyError             = "error"
	AttributeKeyValidator         = "validator"
	AttributeKeyMissCount         = "miss_count"
	AttributeKeyJailDuration      = "jail_duration"
	AttributeKeyDecryptFailed     = "decrypt_failed"
	AttributeKeyProofFailed       = "proof_failed"
	AttributeKeyDKGRound          = "dkg_round"
	AttributeKeyDKGPhase          = "dkg_phase"
	AttributeKeyContributionsCount = "contributions_count"
	AttributeKeyDriftPercent      = "drift_percent"
)
